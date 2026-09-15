"""Single source of truth for every relkit release component.

The deploy CLI, immutable consumer, and product gates all import this module.
Adding a facade therefore means adding one validated row, not another branch.
Product-tree rows also declare how their zip is packed so deploy never branches
on component names.
"""

from __future__ import annotations

from dataclasses import dataclass
from pathlib import Path, PurePosixPath
from typing import Mapping, Optional, Sequence
import re

TARGETS = (
    "linux-amd64",
    "linux-arm64",
    "windows-amd64",
    "darwin-amd64",
    "darwin-arm64",
)
ROLES = ("host-binary", "product-binary", "product-tree")
PROBES = ("files", "tree-sha256")
PACK_KINDS = ("tracked-tree", "working-tree", "listed-files", "go-deps")
HOST_API = ("protocol", "facade")
INPROCESS_IN_HOST_ZIP = re.compile(r"\bRupUpdater\b|\bpackage inprocess\b")
HOST_SOURCE_SUFFIXES = {".go", ".dart", ".ts", ".tsx", ".js", ".rs"}


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
    pack: str = ""
    pack_files: tuple[str, ...] = ()
    pack_extras: tuple[tuple[str, str], ...] = ()
    pack_exclude_parts: tuple[str, ...] = ()
    pack_exclude_suffixes: tuple[str, ...] = ()
    pack_exclude_paths: tuple[str, ...] = ()
    go_entrypoints: tuple[str, ...] = ()
    go_module: str = ""
    host_api: tuple[str, ...] = ()
    facade_paths: tuple[str, ...] = ()

    def pack_kind(self) -> str:
        if self.role != "product-tree":
            return ""
        return self.pack or "tracked-tree"

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
        pack="working-tree",
        pack_exclude_parts=("__pycache__",),
    ),
    Component(
        "sdk-dart", "product-tree", "dart-sdk", "relkit-sdk-dart.zip",
        "sdk/dart", "third_party/relkit/sdk/dart", "files",
        ("pubspec.yaml", "lib"), portable=True, default_consume=True,
        detect=("pubspec.yaml",), updater_process="dart",
        import_signals=("package:rup_client/",),
        host_api=("protocol", "facade"),
        facade_paths=("lib/src/updater_facade.dart",),
        pack_exclude_paths=(
            "lib/src/updater.dart",
            "lib/src/scheduler.dart",
            "example/",
            "test/",
        ),
    ),
    Component(
        "sdk-go", "product-tree", "go-sdk", "relkit-sdk-go.zip",
        ".", "third_party/relkit", "files",
        ("go.mod", "sdk", "api/updater/v1"), portable=True,
        detect=("go.mod",), updater_process="go",
        import_signals=("go.firoyang.com/relkit/sdk",),
        pack="go-deps",
        pack_files=("go.mod", "go.sum"),
        pack_exclude_suffixes=("_test.go",),
        go_entrypoints=("./sdk", "./sdk/updaterfacade"),
        go_module="go.firoyang.com/relkit",
        host_api=("protocol", "facade"),
        facade_paths=("sdk/updaterfacade/facade.go",),
    ),
    Component(
        "sdk-rust", "product-tree", "rust-sdk", "relkit-sdk-rust.zip",
        "sdk/rust", "third_party/relkit/sdk/rust", "files",
        ("Cargo.toml", "src/lib.rs", "proto/updater/v1/updater.proto"),
        portable=True, detect=("Cargo.toml", "**/src-tauri/Cargo.toml"),
        updater_process="rust", import_signals=("relkit_updater", "relkit-updater"),
        pack_extras=(
            ("proto/updater/v1/updater.proto", "proto/updater/v1/updater.proto"),
        ),
        host_api=("facade",),
        facade_paths=("src/lib.rs",),
    ),
    Component(
        "bindings-ts", "product-tree", "bindings-ts",
        "relkit-bindings-ts.zip", "bindings/ts",
        "third_party/relkit/bindings/ts", "files",
        ("package.json", "dist/updater_pb.js", "dist/updater_pb.d.ts"), portable=True,
        detect=("package.json", "**/package.json"), updater_process="node",
        import_signals=("third_party/relkit/bindings/ts", "@relkit/updater-bindings"),
        webview_projection=True,
        pack="listed-files",
        pack_files=(
            "package.json",
            "README.md",
            "dist/updater_pb.js",
            "dist/updater_pb.d.ts",
        ),
        host_api=("facade",),
        facade_paths=("dist/updater_pb.js",),
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
        if row.role != "product-tree":
            extra_pack = (
                row.pack,
                row.pack_files,
                row.pack_extras,
                row.pack_exclude_parts,
                row.pack_exclude_suffixes,
                row.pack_exclude_paths,
                row.go_entrypoints,
                row.go_module,
                row.host_api,
                row.facade_paths,
                row.updater_process,
            )
            if any(extra_pack):
                raise ValueError(f"component {row.name} pack fields only apply to product-tree")
        else:
            kind = row.pack_kind()
            if kind not in PACK_KINDS:
                raise ValueError(f"component {row.name} has invalid pack {row.pack}")
            if kind == "listed-files" and not row.pack_files:
                raise ValueError(f"component {row.name} listed-files pack requires pack_files")
            if kind == "go-deps" and (not row.go_entrypoints or not row.go_module):
                raise ValueError(f"component {row.name} go-deps pack requires go_entrypoints and go_module")
            if kind != "go-deps" and (row.go_entrypoints or row.go_module):
                raise ValueError(f"component {row.name} go-deps fields require pack=go-deps")
            if kind not in ("listed-files", "go-deps") and row.pack_files:
                raise ValueError(f"component {row.name} pack_files require listed-files or go-deps")
            if kind != "working-tree" and row.pack_exclude_parts:
                raise ValueError(f"component {row.name} pack_exclude_parts require working-tree")
            if kind != "go-deps" and row.pack_exclude_suffixes:
                raise ValueError(f"component {row.name} pack_exclude_suffixes require go-deps")
            if kind not in ("tracked-tree", "working-tree") and row.pack_exclude_paths:
                raise ValueError(f"component {row.name} pack_exclude_paths require tracked-tree or working-tree")
            if any("inprocess" in item for item in row.go_entrypoints):
                raise ValueError(f"component {row.name} go_entrypoints must not pack internal/inprocess")
            if row.updater_process:
                unknown = [item for item in row.host_api if item not in HOST_API]
                if unknown:
                    raise ValueError(f"component {row.name} has invalid host_api {unknown}")
                if "facade" not in row.host_api:
                    raise ValueError(f"component {row.name} updater_process requires host_api to include facade")
                if not row.facade_paths:
                    raise ValueError(f"component {row.name} updater_process requires facade_paths")
            elif row.host_api or row.facade_paths:
                raise ValueError(f"component {row.name} host_api/facade_paths require updater_process")
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


def packed_host_surface_errors(
    row: Component,
    archive_names: Sequence[str],
    texts: Mapping[str, str],
) -> tuple[str, ...]:
    """Host zips may ship protocol helpers plus a facade, never an in-process engine."""
    if not row.updater_process:
        return ()
    names = set(archive_names)
    errors: list[str] = []
    for path in row.facade_paths:
        if path not in names:
            errors.append(f"{row.name} facade {path} is not packed")
    for name, text in texts.items():
        suffix = Path(name).suffix.lower()
        if suffix in HOST_SOURCE_SUFFIXES and INPROCESS_IN_HOST_ZIP.search(text):
            errors.append(f"{row.name} packs in-process updater in {name}")
    return tuple(errors)
