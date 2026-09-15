"""Single source of truth for every relkit release component.

The deploy CLI, immutable consumer, and product gates all import this module.
Adding a facade therefore means adding one validated row, not another branch.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import PurePosixPath
from typing import Mapping, Optional, Sequence

TARGETS = (
    "linux-amd64",
    "linux-arm64",
    "windows-amd64",
    "darwin-amd64",
    "darwin-arm64",
)
ROLES = ("host-binary", "product-binary", "product-tree")
PROBES = ("files", "tree-sha256")


@dataclass(frozen=True)
class Component:
    name: str
    role: str
    build_flag: str
    archive: str
    source: str
    destination: str
    probe: str
    required_paths: tuple[str, ...]
    go_package: Optional[str] = None
    binary_prefix: Optional[str] = None
    portable: bool = False
    default_consume: bool = False
    detect: tuple[str, ...] = ()
    updater_process: Optional[str] = None
    import_signals: tuple[str, ...] = ()
    webview_projection: bool = False
    install_names: tuple[tuple[str, str], ...] = ()

    def install_name(self, target: str) -> str:
        overrides = dict(self.install_names)
        if target in overrides:
            return overrides[target]
        if "*" in overrides:
            value = overrides["*"]
            return value.replace("{exe}", ".exe" if target.startswith("windows-") else "")
        raise ValueError(f"{self.name} has no install name for {target}")


COMPONENTS: tuple[Component, ...] = (
    Component("serve", "host-binary", "serve", "", "", "", "files", (),
              "./cmd/relkit-serve", "relkit-serve"),
    Component("agent", "host-binary", "agent", "", "", "", "files", (),
              "./cmd/relkit-agent", "relkit-agent"),
    Component(
        "cli", "product-binary", "cli", "", "", "tools/bin", "files", (),
        "./cmd/relkit", "relkit", default_consume=True, install_names=(
            ("windows-amd64", "relkit.exe"),
            ("linux-amd64", "relkit-linux-amd64"),
            ("*", "relkit"),
        ),
    ),
    Component(
        "updater", "product-binary", "updater", "", "", "tools/bin", "files", (),
        "./cmd/relkit-updater", "relkit-updater",
        default_consume=True, install_names=(("*", "relkit-updater{exe}"),),
    ),
    Component(
        "host-scripts", "product-tree", "host-scripts",
        "relkit-host-scripts.zip", "scripts/host", "scripts/host",
        "tree-sha256", (
            "relkit_host.py", "relkit_consume.py", "hostlib/__init__.py",
            "hostlib/const.py", "hostlib/state.py", "hostlib/ssh.py",
            "hostlib/inspect.py", "hostlib/onboard.py", "hostlib/reconcile.py",
            "hostlib/remote.py", "hostlib/release.py", "hostlib/retrospect.py",
            "hostlib/facets.py", "hostlib/digest.py", "hostlib/gates.py",
            "hostlib/runtime.py",
        ),
        portable=True,
    ),
    Component(
        "sdk-dart", "product-tree", "dart-sdk", "relkit-sdk-dart.zip",
        "sdk/dart", "third_party/relkit/sdk/dart", "files",
        ("pubspec.yaml", "lib"), portable=True, default_consume=True,
        detect=("pubspec.yaml",), updater_process="dart",
        import_signals=("package:rup_client/",),
    ),
    Component(
        "sdk-go", "product-tree", "go-sdk", "relkit-sdk-go.zip",
        ".", "third_party/relkit", "files",
        ("go.mod", "sdk", "api/updater/v1"), portable=True,
        detect=("go.mod",), updater_process="go",
        import_signals=("go.firoyang.com/relkit/sdk",),
    ),
    Component(
        "sdk-rust", "product-tree", "rust-sdk", "relkit-sdk-rust.zip",
        "sdk/rust", "third_party/relkit/sdk/rust", "files",
        ("Cargo.toml", "src/lib.rs", "proto/updater/v1/updater.proto"),
        portable=True, detect=("Cargo.toml", "**/src-tauri/Cargo.toml"),
        updater_process="rust", import_signals=("relkit_updater", "relkit-updater"),
    ),
    Component(
        "bindings-ts", "product-tree", "bindings-ts",
        "relkit-bindings-ts.zip", "bindings/ts",
        "third_party/relkit/bindings/ts", "files",
        ("package.json", "dist/updater_pb.js", "dist/updater_pb.d.ts"), portable=True,
        detect=("package.json", "**/package.json"), updater_process="node",
        import_signals=("third_party/relkit/bindings/ts", "@relkit/updater-bindings"),
        webview_projection=True,
    ),
)


def validate_components(rows: Sequence[Component]) -> None:
    names: set[str] = set()
    flags: set[str] = set()
    for row in rows:
        missing = [
            field for field in ("name", "role", "build_flag", "probe")
            if not getattr(row, field)
        ]
        if row.role in ("host-binary", "product-binary"):
            missing.extend(
                field for field in ("go_package", "binary_prefix")
                if not getattr(row, field)
            )
        if row.role == "product-tree":
            missing.extend(
                field for field in ("archive", "source", "destination", "required_paths")
                if not getattr(row, field)
            )
        if missing:
            raise ValueError(f"component {row.name or '<unnamed>'} missing fields: {', '.join(missing)}")
        if row.role not in ROLES:
            raise ValueError(f"component {row.name} has invalid role {row.role}")
        if row.probe not in PROBES:
            raise ValueError(f"component {row.name} has invalid probe {row.probe}")
        if row.name in names or row.build_flag in flags:
            raise ValueError(f"duplicate component name/build flag: {row.name}/{row.build_flag}")
        if row.portable != (row.role == "product-tree"):
            raise ValueError(f"component {row.name} portable disagrees with role")
        names.add(row.name)
        flags.add(row.build_flag)


validate_components(COMPONENTS)
BY_NAME: Mapping[str, Component] = {row.name: row for row in COMPONENTS}


def components(role: Optional[str] = None) -> tuple[Component, ...]:
    return tuple(row for row in COMPONENTS if role is None or row.role == role)


def product_components() -> tuple[Component, ...]:
    return components("product-binary") + components("product-tree")


def portable_components() -> tuple[Component, ...]:
    return tuple(row for row in product_components() if row.portable)


def default_components() -> tuple[Component, ...]:
    return tuple(row for row in product_components() if row.default_consume)


def detected_components(signals: Sequence[str]) -> tuple[str, ...]:
    return tuple(
        row.name
        for row in components("product-tree")
        if row.detect and any(
            PurePosixPath(signal).match(pattern)
            for signal in signals for pattern in row.detect
        )
    )


def updater_process_values() -> tuple[str, ...]:
    return tuple(dict.fromkeys(
        row.updater_process for row in components("product-tree") if row.updater_process
    )) + ("other",)


def with_component(row: Component) -> tuple[Component, ...]:
    """Test helper proving a new facade is entirely represented by one row."""
    rows = COMPONENTS + (row,)
    validate_components(rows)
    return rows
