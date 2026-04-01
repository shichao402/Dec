#!/usr/bin/env python3
"""
Dec 自举验收脚本。

目标：
1. 使用当前仓库自身作为项目目录
2. 在临时 HOME / DEC_HOME 中运行，避免污染用户环境
3. 备份并恢复项目中的 .dec / .cursor / .windsurf 等目录
4. 验证以下关键 story：
   - dec init 创建项目配置并安装 Dec Skill
   - dec vault init 连接个人 vault
   - dec sync 根据 vault.yaml 拉取 vault 资产到全部 IDE
   - 托管资产使用 dec-* 命名且不覆盖用户原始内容
   - dec vault pull 同步到全部 IDE，并写回 vault.yaml
   - pull 后再次 dec sync 不会丢失资产
   - MCP 资产通过 live mcp.json 合并，不再写 sidecar
   - dec vault import 将项目内 skill 导入到 vault
   - dec vault status 能检测本地修改
"""

from __future__ import annotations

import argparse
import json
import os
import shutil
import subprocess
import sys
import tempfile
from dataclasses import dataclass
from pathlib import Path


@dataclass
class CommandResult:
    command: list[str]
    returncode: int
    stdout: str
    stderr: str


class SelfHostTestError(RuntimeError):
    pass


def print_step(message: str) -> None:
    print(f"\n[STEP] {message}")


def print_ok(message: str) -> None:
    print(f"[ OK ] {message}")


def print_info(message: str) -> None:
    print(f"[INFO] {message}")


def run_command(
    command: list[str],
    *,
    cwd: Path,
    env: dict[str, str],
    check: bool = True,
) -> CommandResult:
    completed = subprocess.run(
        command,
        cwd=str(cwd),
        env=env,
        capture_output=True,
        text=True,
    )
    result = CommandResult(
        command=command,
        returncode=completed.returncode,
        stdout=completed.stdout,
        stderr=completed.stderr,
    )
    if check and completed.returncode != 0:
        raise SelfHostTestError(
            "命令执行失败:\n"
            f"  cwd: {cwd}\n"
            f"  cmd: {' '.join(command)}\n"
            f"  exit_code: {completed.returncode}\n"
            f"  stdout:\n{completed.stdout}\n"
            f"  stderr:\n{completed.stderr}"
        )
    return result


def ensure_file(path: Path, content: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content, encoding="utf-8")


def assert_exists(path: Path, description: str) -> None:
    if not path.exists():
        raise SelfHostTestError(f"{description} 不存在: {path}")
    print_ok(f"{description}: {path}")


def assert_not_exists(path: Path, description: str) -> None:
    if path.exists():
        raise SelfHostTestError(f"{description} 不应存在: {path}")
    print_ok(f"{description} 未生成")


def assert_contains(text: str, needle: str, description: str) -> None:
    if needle not in text:
        raise SelfHostTestError(f"{description} 缺少内容: {needle}\n实际输出:\n{text}")
    print_ok(description)


def assert_file_contains(path: Path, needle: str, description: str) -> None:
    assert_contains(read_text(path), needle, description)


def copy_path(src: Path, dst: Path) -> None:
    if src.is_dir():
        shutil.copytree(src, dst, symlinks=True, ignore_dangling_symlinks=True)
    else:
        dst.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, dst)


def remove_path(path: Path) -> None:
    if not path.exists():
        return
    if path.is_dir() and not path.is_symlink():
        shutil.rmtree(path)
    else:
        path.unlink()


class ProjectBackup:
    def __init__(self, project_root: Path, backup_root: Path, rel_paths: list[str]) -> None:
        self.project_root = project_root
        self.backup_root = backup_root
        self.rel_paths = rel_paths
        self.existing: dict[str, bool] = {}

    def backup(self) -> None:
        for rel in self.rel_paths:
            src = self.project_root / rel
            self.existing[rel] = src.exists()
            if src.exists():
                copy_path(src, self.backup_root / rel)
                remove_path(src)

    def restore(self) -> None:
        for rel in self.rel_paths:
            src = self.project_root / rel
            backup = self.backup_root / rel
            remove_path(src)
            if self.existing.get(rel) and backup.exists():
                copy_path(backup, src)


def build_binary(project_root: Path, work_root: Path, env: dict[str, str]) -> Path:
    binary_name = "dec.exe" if os.name == "nt" else "dec"
    binary_path = work_root / "bin" / binary_name
    binary_path.parent.mkdir(parents=True, exist_ok=True)
    print_step("构建 Dec 二进制")
    run_command(["go", "build", "-o", str(binary_path), "."], cwd=project_root, env=env)
    assert_exists(binary_path, "Dec 二进制")
    return binary_path


def make_vault_index(items: list[dict[str, str]]) -> str:
    payload = {"version": "v1", "items": items}
    return json.dumps(payload, indent=2, ensure_ascii=False)


def setup_local_vault_remote(work_root: Path, env: dict[str, str]) -> Path:
    print_step("准备本地 vault remote")
    src_repo = work_root / "vault-src"
    bare_repo = work_root / "vault-remote.git"
    src_repo.mkdir(parents=True, exist_ok=True)

    run_command(["git", "init", "-b", "main"], cwd=src_repo, env=env)
    ensure_file(src_repo / "README.md", "# Dec Vault\n")

    ensure_file(
        src_repo / "skills" / "reusable-skill" / "SKILL.md",
        "---\nname: reusable-skill\ndescription: reusable vault skill\n---\n# reusable skill\n",
    )
    ensure_file(
        src_repo / "skills" / "pulled-skill" / "SKILL.md",
        "---\nname: pulled-skill\ndescription: pull target skill\n---\n# pulled skill\n",
    )
    ensure_file(
        src_repo / "mcp" / "reusable-mcp.json",
        json.dumps(
            {
                "command": "npx",
                "args": ["-y", "reusable-mcp"],
                "env": {"REUSABLE_TOKEN": "${REUSABLE_TOKEN}"},
            },
            indent=2,
            ensure_ascii=False,
        )
        + "\n",
    )
    ensure_file(
        src_repo / "mcp" / "pulled-mcp.json",
        json.dumps(
            {
                "command": "npx",
                "args": ["-y", "pulled-mcp"],
                "env": {"PULLED_TOKEN": "${PULLED_TOKEN}"},
            },
            indent=2,
            ensure_ascii=False,
        )
        + "\n",
    )

    vault_items = [
        {
            "name": "reusable-skill",
            "type": "skill",
            "description": "reusable vault skill",
            "path": "skills/reusable-skill",
            "created_at": "2026-01-01T00:00:00Z",
            "updated_at": "2026-01-01T00:00:00Z",
        },
        {
            "name": "pulled-skill",
            "type": "skill",
            "description": "pull target skill",
            "path": "skills/pulled-skill",
            "created_at": "2026-01-01T00:00:00Z",
            "updated_at": "2026-01-01T00:00:00Z",
        },
        {
            "name": "reusable-mcp",
            "type": "mcp",
            "description": "reusable vault mcp",
            "path": "mcp/reusable-mcp.json",
            "created_at": "2026-01-01T00:00:00Z",
            "updated_at": "2026-01-01T00:00:00Z",
        },
        {
            "name": "pulled-mcp",
            "type": "mcp",
            "description": "pull target mcp",
            "path": "mcp/pulled-mcp.json",
            "created_at": "2026-01-01T00:00:00Z",
            "updated_at": "2026-01-01T00:00:00Z",
        },
    ]
    ensure_file(src_repo / "vault.json", make_vault_index(vault_items))

    run_command(["git", "add", "."], cwd=src_repo, env=env)
    run_command(["git", "commit", "-m", "init vault"], cwd=src_repo, env=env)

    run_command(["git", "init", "--bare", str(bare_repo)], cwd=work_root, env=env)
    run_command(["git", "remote", "add", "origin", str(bare_repo)], cwd=src_repo, env=env)
    run_command(["git", "push", "-u", "origin", "main"], cwd=src_repo, env=env)

    assert_exists(bare_repo, "本地 vault bare remote")
    return bare_repo


def read_text(path: Path) -> str:
    return path.read_text(encoding="utf-8")


def overwrite_vault_config(project_root: Path) -> None:
    print_step("写入项目级 vault.yaml 声明")
    ensure_file(
        project_root / ".dec" / "config" / "vault.yaml",
        "vault_skills:\n  - reusable-skill\n\nvault_mcps:\n  - reusable-mcp\n",
    )
    print_ok("vault.yaml 已声明 reusable-skill 和 reusable-mcp")


def create_user_skill(project_root: Path, name: str) -> Path:
    skill_dir = project_root / ".cursor" / "skills" / name
    ensure_file(
        skill_dir / "SKILL.md",
        f"---\nname: {name}\ndescription: local skill {name}\n---\n# {name}\n",
    )
    return skill_dir


def verify_sync_outputs(project_root: Path) -> None:
    print_step("验证 sync 输出")
    assert_exists(
        project_root / ".cursor" / "skills" / "dec-reusable-skill" / "SKILL.md",
        "Cursor 托管 reusable skill",
    )
    assert_exists(
        project_root / ".windsurf" / "skills" / "dec-reusable-skill" / "SKILL.md",
        "Windsurf 托管 reusable skill",
    )
    assert_exists(
        project_root / ".cursor" / "skills" / "local-story-skill" / "SKILL.md",
        "用户原始本地 skill",
    )
    assert_not_exists(
        project_root / ".windsurf" / "skills" / "reusable-skill",
        "未托管名称的 reusable skill 副本",
    )
    assert_file_contains(
        project_root / ".cursor" / "mcp.json",
        '"dec-reusable-mcp"',
        "Cursor live mcp.json 已包含托管 reusable MCP",
    )
    assert_file_contains(
        project_root / ".windsurf" / "mcp.json",
        '"dec-reusable-mcp"',
        "Windsurf live mcp.json 已包含托管 reusable MCP",
    )


def verify_pull_outputs(project_root: Path) -> None:
    print_step("验证 pull 输出")
    assert_exists(
        project_root / ".cursor" / "skills" / "dec-pulled-skill" / "SKILL.md",
        "Cursor pull 托管 skill",
    )
    assert_exists(
        project_root / ".windsurf" / "skills" / "dec-pulled-skill" / "SKILL.md",
        "Windsurf pull 托管 skill",
    )
    assert_file_contains(
        project_root / ".cursor" / "mcp.json",
        '"dec-pulled-mcp"',
        "Cursor pull 托管 MCP",
    )
    assert_file_contains(
        project_root / ".windsurf" / "mcp.json",
        '"dec-pulled-mcp"',
        "Windsurf pull 托管 MCP",
    )
    assert_not_exists(
        project_root / ".cursor" / "mcp-configs" / "dec-pulled-mcp.json",
        "旧 sidecar MCP 文件",
    )


def verify_pull_updates_vault_config(project_root: Path) -> None:
    print_step("验证 pull 自动更新 vault.yaml")
    vault_config = read_text(project_root / ".dec" / "config" / "vault.yaml")
    assert_contains(vault_config, "pulled-skill", "vault.yaml 已声明 pulled-skill")
    assert_contains(vault_config, "pulled-mcp", "vault.yaml 已声明 pulled-mcp")


def verify_saved_to_remote(work_root: Path, remote_repo: Path, env: dict[str, str]) -> None:
    print_step("验证保存后的 vault remote 内容")
    inspect_dir = work_root / "vault-inspect"
    run_command(["git", "clone", str(remote_repo), str(inspect_dir)], cwd=work_root, env=env)
    vault_json = read_text(inspect_dir / "vault.json")
    assert_contains(vault_json, "\"story-created-skill\"", "remote vault 已包含 story-created-skill")
    assert_exists(
        inspect_dir / "skills" / "story-created-skill" / "SKILL.md",
        "remote vault skill 目录",
    )


def verify_status_output(status_output: str) -> None:
    print_step("验证 vault status 输出")
    assert_contains(status_output, "story-created-skill", "status 输出包含已修改 skill")
    assert_contains(status_output, "[M]", "status 输出包含 modified 标记")


def run_story(project_root: Path, keep_artifacts: bool) -> None:
    with tempfile.TemporaryDirectory(prefix="dec-self-host-") as temp_dir:
        work_root = Path(temp_dir)
        home_dir = work_root / "home"
        dec_home = work_root / "dec-home"
        backup_root = work_root / "backup"
        home_dir.mkdir(parents=True, exist_ok=True)
        dec_home.mkdir(parents=True, exist_ok=True)
        backup_root.mkdir(parents=True, exist_ok=True)

        env = os.environ.copy()
        env.update(
            {
                "HOME": str(home_dir),
                "USERPROFILE": str(home_dir),
                "DEC_HOME": str(dec_home),
                "GIT_AUTHOR_NAME": "Dec Self Host Test",
                "GIT_AUTHOR_EMAIL": "dec-self-host@example.com",
                "GIT_COMMITTER_NAME": "Dec Self Host Test",
                "GIT_COMMITTER_EMAIL": "dec-self-host@example.com",
            }
        )

        backup = ProjectBackup(
            project_root,
            backup_root,
            [
                ".dec",
                ".cursor",
                ".windsurf",
                ".codebuddy",
                ".trae",
            ],
        )

        if keep_artifacts:
            print_info(f"临时目录保留在: {work_root}")

        backup.backup()
        try:
            binary_path = build_binary(project_root, work_root, env)
            remote_repo = setup_local_vault_remote(work_root, env)

            print_step("运行 dec init")
            run_command(
                [str(binary_path), "init", "--ide", "cursor", "--ide", "windsurf"],
                cwd=project_root,
                env=env,
            )
            assert_exists(project_root / ".dec" / "config" / "vault.yaml", "vault.yaml")
            assert_exists(home_dir / ".cursor" / "skills" / "dec" / "SKILL.md", "用户级 Dec Skill")

            print_step("运行 dec vault init")
            run_command(
                [str(binary_path), "vault", "init", "--repo", str(remote_repo)],
                cwd=project_root,
                env=env,
            )
            assert_exists(dec_home / "vault" / ".git", "本地 vault clone")

            overwrite_vault_config(project_root)
            create_user_skill(project_root, "local-story-skill")

            print_step("运行 dec sync")
            run_command([str(binary_path), "sync"], cwd=project_root, env=env)
            verify_sync_outputs(project_root)

            print_step("运行 dec vault pull skill pulled-skill")
            run_command(
                [str(binary_path), "vault", "pull", "skill", "pulled-skill"],
                cwd=project_root,
                env=env,
            )
            print_step("运行 dec vault pull mcp pulled-mcp")
            run_command(
                [str(binary_path), "vault", "pull", "mcp", "pulled-mcp"],
                cwd=project_root,
                env=env,
            )
            verify_pull_outputs(project_root)
            verify_pull_updates_vault_config(project_root)

            print_step("pull 后再次运行 dec sync")
            run_command([str(binary_path), "sync"], cwd=project_root, env=env)
            verify_pull_outputs(project_root)

            print_step("运行 dec vault import")
            created_skill_dir = create_user_skill(project_root, "story-created-skill")
            run_command(
                [
                    str(binary_path),
                    "vault",
                    "import",
                    "skill",
                    str(created_skill_dir),
                    "--tag",
                    "story",
                ],
                cwd=project_root,
                env=env,
            )
            verify_saved_to_remote(work_root, remote_repo, env)

            print_step("修改已保存 skill 后运行 dec vault status")
            ensure_file(
                created_skill_dir / "notes.md",
                "# changed\n",
            )
            status_result = run_command(
                [str(binary_path), "vault", "status"],
                cwd=project_root,
                env=env,
            )
            verify_status_output(status_result.stdout)

            print("\n[SUCCESS] 自举验收通过")
        finally:
            backup.restore()


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Dec 自举验收脚本")
    parser.add_argument(
        "--project-root",
        default=str(Path(__file__).resolve().parent.parent),
        help="项目根目录，默认使用当前仓库",
    )
    parser.add_argument(
        "--keep-artifacts",
        action="store_true",
        help="打印临时目录位置，便于排查问题",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    project_root = Path(args.project_root).resolve()

    if not (project_root / "go.mod").exists():
        raise SelfHostTestError(f"项目根目录无效，未找到 go.mod: {project_root}")

    run_story(project_root, args.keep_artifacts)
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except SelfHostTestError as exc:
        print(f"\n[FAIL] {exc}", file=sys.stderr)
        raise SystemExit(1)
