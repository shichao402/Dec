"""Create the Dec repo orphan branch `registry` locally (does not push)."""
from __future__ import annotations

import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


def git(*args: str) -> None:
    subprocess.check_call(["git", *args], cwd=ROOT)


def main() -> int:
    git("checkout", "--orphan", "registry")
    git("reset")
    registry_dir = ROOT / ".registry-seed"
    registry_dir.mkdir(exist_ok=True)
    readme = ROOT / "REGISTRY.md"
    readme.write_text(
        "Official Dec asset registry.\nPublished snapshots live on this orphan branch.\n",
        encoding="utf-8",
    )
    yanked = ROOT / "yanked.yaml"
    if not yanked.exists():
        yanked.write_text("{}\n", encoding="utf-8")
    git("add", "REGISTRY.md", "yanked.yaml")
    git("commit", "-m", "registry: seed orphan branch")
    print("Created local branch registry. Push with: git push -u origin registry")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
