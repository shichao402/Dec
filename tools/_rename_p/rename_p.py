#!/usr/bin/env python3
"""One-off: replace the "P" shorthand with 项目 in human-readable text."""
import re
import subprocess
import sys
from pathlib import Path

REPO = Path(__file__).resolve().parents[2]

EXTS = {".go", ".md", ".mdc", ".proto", ".yml", ".yaml", ".txt", ".py", ".sh", ".ps1", ".json"}

# lines where a bare P is a keystroke, not the project shorthand
SKIP_LINE = re.compile(r"P\s+推送|P\s+Push|按 P 后|P 后应|P 预览|不再称 P")

CJK = r"\u4e00-\u9fff"
TOKEN = re.compile(r"(?<![0-9A-Za-z_/\-.])P(?![0-9A-Za-z_/\-])")
PLACEHOLDER = "\u0001"


def has_cjk(s: str) -> bool:
    return re.search(f"[{CJK}]", s) is not None


def convert(line: str) -> str:
    if not has_cjk(line) or SKIP_LINE.search(line):
        return line
    new = TOKEN.sub(PLACEHOLDER, line)
    if PLACEHOLDER not in new:
        return line
    # 中文之间不留空格
    new = re.sub(f"([{CJK}]) +{PLACEHOLDER}", r"\1" + PLACEHOLDER, new)
    new = re.sub(f"{PLACEHOLDER} +([{CJK}])", PLACEHOLDER + r"\1", new)
    # 「本项目 P」这类重复主语去重
    new = new.replace("项目" + PLACEHOLDER, "项目")
    return new.replace(PLACEHOLDER, "项目")


def tracked_files():
    out = subprocess.run(
        ["git", "ls-files"], cwd=REPO, capture_output=True, text=True, check=True
    ).stdout.splitlines()
    for rel in out:
        p = REPO / rel
        if p.suffix in EXTS and p.is_file():
            yield p


def main() -> int:
    apply = "--apply" in sys.argv
    report = (REPO / "tools/_rename_p/report.md").open("w", encoding="utf-8")
    changed = 0
    for path in tracked_files():
        try:
            text = path.read_text(encoding="utf-8")
        except UnicodeDecodeError:
            continue
        lines = text.split("\n")
        new_lines = [convert(l) for l in lines]
        if new_lines == lines:
            continue
        changed += 1
        if apply:
            path.write_text("\n".join(new_lines), encoding="utf-8")
        else:
            rel = path.relative_to(REPO)
            for i, (old, new) in enumerate(zip(lines, new_lines), start=1):
                if old != new:
                    report.write(f"{rel}:{i}\n  - {old.strip()}\n  + {new.strip()}\n")
    report.write(f"\n{'applied' if apply else 'dry-run'}: {changed} files\n")
    report.close()
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
