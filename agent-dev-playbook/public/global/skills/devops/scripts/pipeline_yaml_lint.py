#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
蓝盾普通流水线 YAML 本地校验器

在调用 save-draft 之前先在本地把 YAML 校到合法，避免后端返回
`status: 2100130 yaml不合法 [...]` 之后再来回猜。

两层校验：
1. 规则校验（无第三方依赖）：覆盖后端最常报错的几类写法（on.manual、props.type、
   variables 结构、cron 字段数等），并给出可直接照抄的修法。
2. JSON Schema 校验（需要 `pip install jsonschema`）：用官方 pipeline-yaml-schema.json
   全量校验，未安装时自动跳过，只提示。

用法：
    python pipeline_yaml_lint.py --yaml-file ./pipeline.yaml
    python pipeline_yaml_lint.py --yaml-file ./pipeline.yaml --fix --in-place

退出码：0 = 通过（可能有 warning），2 = 有 error，1 = 参数/读取错误。
"""

import argparse
import json
import os
import re
import sys
from typing import Any, Dict, List, Optional, Tuple

import yaml

SCHEMA_FILENAME = "pipeline-yaml-schema.json"

ROOT_KEYS = {
    "version", "name", "desc", "label", "on", "variables", "stages", "jobs", "steps",
    "extends", "resources", "finally", "notices", "concurrency", "disable-pipeline",
    "fail-if-variable-invalid", "recommended-version", "custom-build-num",
    "syntax-dialect", "cancel-policy", "runs-on",
}

TRIGGER_KEYS = {
    "push", "tag", "mr", "note", "review", "issue", "delete", "manual", "remote",
    "schedules", "openapi", "repo-hook", "repo-name", "type", "change-commit",
    "workspace-id", "story", "bug", "enable", "name", "id", "path-filter-type",
}

MANUAL_KEYS = {"id", "name", "enable", "can-skip-step", "use-latest-inputs"}

VARIABLE_KEYS = {
    "value", "readonly", "required", "const", "allow-modify-at-startup",
    "value-not-empty", "props",
}

PROPS_KEYS = {
    "label", "type", "options", "description", "group", "required", "repo-id",
    "relative-path", "scm-type", "container-type", "filter-rule", "version-control",
    "metadata", "payload", "fields", "multiple", "search-url", "key-url", "value-url",
}

PROPS_TYPE_ENUM = [
    "vuex-input", "vuex-textarea", "selector", "checkbox", "boolean", "git-ref",
    "svn-tag", "code-lib", "container-type", "artifactory", "sub-pipeline",
    "custom-file", "tips", "repo-ref", "form-list",
]

# 只做「唯一且无歧义」的映射，其余一律报错让人工决定
PROPS_TYPE_ALIASES = {
    "input": "vuex-input",
    "text": "vuex-input",
    "string": "vuex-input",
    "vuex_input": "vuex-input",
    "textarea": "vuex-textarea",
    "vuex_textarea": "vuex-textarea",
    "password": "vuex-input",
    "select": "selector",
    "enum": "selector",
    "multi-selector": "selector",
    "multiselector": "selector",
    "bool": "boolean",
    "atom-checkbox": "checkbox",
    "enum-input": "selector",
}

TAPD_STORY_ACTIONS = {"create", "update", "delete", "status_change"}
TAPD_BUG_ACTIONS = {"create", "update", "delete", "status_change"}

RESERVED_VAR_PREFIXES = ("BK_CI_", "CI_", "BKCI_")


class BkciYamlLoader(yaml.SafeLoader):
    """YAML 1.1 会把 on/off/yes/no 解析成布尔，导致 `on:` 触发器变成 True 键。

    蓝盾后端按 YAML 1.2 语义处理，这里对齐它，只把 true/false 当布尔。
    """


BkciYamlLoader.yaml_implicit_resolvers = {
    ch: [(tag, regexp) for tag, regexp in resolvers if tag != "tag:yaml.org,2002:bool"]
    if ch in "oOyYnN" else resolvers
    for ch, resolvers in yaml.SafeLoader.yaml_implicit_resolvers.items()
}


def load_yaml(text: str) -> Any:
    return yaml.load(text, Loader=BkciYamlLoader)


class Issue:
    def __init__(self, level: str, path: str, message: str, fix: str = "") -> None:
        self.level = level
        self.path = path
        self.message = message
        self.fix = fix

    def render(self) -> str:
        icon = "❌" if self.level == "error" else "⚠️"
        out = f"{icon} {self.path}: {self.message}"
        if self.fix:
            out += f"\n     修法：{self.fix}"
        return out


# ==================== 规则校验 ====================

def _as_trigger_list(on_value: Any) -> List[Any]:
    return on_value if isinstance(on_value, list) else [on_value]


def _check_manual(node: Any, path: str, issues: List[Issue]) -> None:
    if isinstance(node, str):
        if node.strip() not in ("enabled", "disabled"):
            issues.append(Issue(
                "error", path,
                f"字符串写法只允许 enabled / disabled，当前为 {node!r}",
                "写成 `manual: enabled`",
            ))
        return
    if not isinstance(node, dict):
        issues.append(Issue(
            "error", path, f"类型非法（{type(node).__name__}）",
            "写成 `manual: enabled`，或对象写法 `manual: {name: 手动触发, enable: true}`",
        ))
        return
    if "enabled" in node:
        issues.append(Issue(
            "error", f"{path}.enabled",
            "对象写法里没有 enabled 字段（schema 不允许多余字段）",
            "开关字段叫 `enable`（布尔）；只想开启就直接写 `manual: enabled`",
        ))
    for key in node:
        if key not in MANUAL_KEYS and key != "enabled":
            issues.append(Issue(
                "error", f"{path}.{key}", "不是 manual 允许的字段",
                f"允许的字段：{', '.join(sorted(MANUAL_KEYS))}",
            ))
    if "enable" in node and not isinstance(node["enable"], bool):
        issues.append(Issue(
            "error", f"{path}.enable", "manual.enable 必须是布尔值",
            "`enable: true`（注意 remote.enable 反过来是字符串 enabled/disabled）",
        ))


def _check_remote(node: Any, path: str, issues: List[Issue]) -> None:
    if isinstance(node, str):
        if node.strip() not in ("enabled", "disabled"):
            issues.append(Issue("error", path, f"只允许 enabled / disabled，当前为 {node!r}", "写成 `remote: enabled`"))
        return
    if isinstance(node, dict) and "enable" in node and not isinstance(node["enable"], str):
        issues.append(Issue(
            "error", f"{path}.enable", "remote.enable 必须是字符串 enabled / disabled",
            "`enable: enabled`（和 manual.enable 的布尔写法不一样，别混）",
        ))


def _check_cron(expr: Any, path: str, issues: List[Issue]) -> None:
    if not isinstance(expr, str):
        issues.append(Issue("error", path, "cron 必须是字符串", '用引号包起来，如 cron: "0 9 * * *"'))
        return
    fields = expr.split()
    if len(fields) != 5:
        issues.append(Issue(
            "error", path, f"cron 需要 5 段（分 时 日 月 周），当前 {len(fields)} 段：{expr!r}",
            '每天 9 点写 "0 9 * * *"；不支持秒级，别写 6 段',
        ))


def _check_schedules(node: Any, path: str, issues: List[Issue]) -> None:
    for idx, item in enumerate(node if isinstance(node, list) else [node]):
        item_path = f"{path}[{idx}]" if isinstance(node, list) else path
        if not isinstance(item, dict):
            issues.append(Issue("error", item_path, "schedules 项必须是对象", '如 {cron: "0 9 * * *", always: true}'))
            continue
        if "cron" not in item:
            issues.append(Issue("error", item_path, "缺少 cron", '补 cron: "0 9 * * *"'))
        else:
            _check_cron(item["cron"], f"{item_path}.cron", issues)
        if item.get("repo-type") is None and item.get("always") is None:
            issues.append(Issue(
                "warning", item_path, "既没写 always 也没写 repo-type",
                "无代码库场景建议 `always: true` + `repo-type: NONE`，否则可能不触发",
            ))


def _check_tapd(node: Dict[str, Any], path: str, issues: List[Issue]) -> None:
    if not node.get("workspace-id"):
        issues.append(Issue("error", path, "type: tapd 缺少 workspace-id", "补 `workspace-id: \"<TAPD 项目 ID>\"`"))
    for event, allowed in (("story", TAPD_STORY_ACTIONS), ("bug", TAPD_BUG_ACTIONS)):
        block = node.get(event)
        if block is None:
            continue
        if not isinstance(block, dict):
            issues.append(Issue("error", f"{path}.{event}", "必须是对象", "如 `story: {action: [create, update]}`"))
            continue
        actions = block.get("action") or []
        if isinstance(actions, str):
            actions = [actions]
        for action in actions:
            if action not in allowed:
                issues.append(Issue(
                    "error", f"{path}.{event}.action", f"不支持的 action {action!r}",
                    f"可选：{', '.join(sorted(allowed))}",
                ))
    if not node.get("story") and not node.get("bug"):
        issues.append(Issue("error", path, "TAPD 触发器至少要配 story 或 bug", "补 `story: {action: [create]}`"))


def _check_on(on_value: Any, issues: List[Issue]) -> None:
    triggers = _as_trigger_list(on_value)
    for idx, trigger in enumerate(triggers):
        path = f"$.on[{idx}]" if isinstance(on_value, list) else "$.on"
        if not isinstance(trigger, dict):
            issues.append(Issue("error", path, "触发器必须是对象", "如 `on: {manual: enabled}`"))
            continue
        for key, value in trigger.items():
            if key not in TRIGGER_KEYS:
                issues.append(Issue(
                    "error", f"{path}.{key}", "不是 on 允许的触发器字段",
                    f"允许：{', '.join(sorted(TRIGGER_KEYS))}",
                ))
            if key == "manual":
                _check_manual(value, f"{path}.manual", issues)
            elif key == "remote":
                _check_remote(value, f"{path}.remote", issues)
            elif key == "schedules":
                _check_schedules(value, f"{path}.schedules", issues)
        if trigger.get("type") == "tapd":
            _check_tapd(trigger, path, issues)


def _check_props(props: Any, path: str, issues: List[Issue]) -> None:
    if not isinstance(props, dict):
        issues.append(Issue("error", path, "props 必须是对象", "如 `props: {label: 分支, type: vuex-input}`"))
        return
    if "type" not in props:
        issues.append(Issue("error", path, "props 缺少必填字段 type", "补 `type: vuex-input`"))
    else:
        ptype = props["type"]
        if ptype not in PROPS_TYPE_ENUM:
            alias = PROPS_TYPE_ALIASES.get(str(ptype).strip().lower())
            fix = (f"改成 `type: {alias}`" if alias
                   else f"只能取：{', '.join(PROPS_TYPE_ENUM)}")
            issues.append(Issue(
                "error", f"{path}.type", f"{ptype!r} 不在 props.type 枚举里（单行文本是 vuex-input，不是 input）", fix,
            ))
    for key in props:
        if key not in PROPS_KEYS:
            issues.append(Issue(
                "error", f"{path}.{key}", "不是 props 允许的字段（schema 禁止多余字段）",
                f"允许：{', '.join(sorted(PROPS_KEYS))}",
            ))
    if props.get("type") == "selector" and not props.get("options"):
        issues.append(Issue("warning", path, "selector 没有 options，启动表单会是空下拉", "补 `options: [{id: dev}, {id: prod}]`"))
    if props.get("type") == "git-ref" and not props.get("repo-id"):
        issues.append(Issue("warning", path, "git-ref 缺少 repo-id", "补关联代码库的 hashId"))


def _check_variables(variables: Any, issues: List[Issue]) -> None:
    if not isinstance(variables, dict):
        issues.append(Issue("error", "$.variables", "variables 必须是对象", "key 是变量名，value 是默认值或 {value: ...}"))
        return
    for name, node in variables.items():
        path = f"$.variables.{name}"
        if name.startswith(RESERVED_VAR_PREFIXES):
            issues.append(Issue(
                "warning", path, "使用了系统保留前缀（BK_CI_ / CI_），可能与内置变量冲突",
                "自定义变量用小写短横线命名，如 `node-agent-id`；要指定构建机请写 job 的 runs-on",
            ))
        if not isinstance(node, dict):
            continue  # 标量默认值是合法简写
        if "value" not in node:
            issues.append(Issue("error", path, "对象写法必须有 value", "补 `value: \"\"`"))
        for key in node:
            if key not in VARIABLE_KEYS:
                hint = ("把它挪到 props 下面" if key in PROPS_KEYS else
                        f"允许：{', '.join(sorted(VARIABLE_KEYS))}")
                issues.append(Issue("error", f"{path}.{key}", "不是变量允许的字段", hint))
        if "props" in node:
            _check_props(node["props"], f"{path}.props", issues)


def _check_steps(steps: Any, path: str, issues: List[Issue]) -> None:
    if not isinstance(steps, list):
        return
    for idx, step in enumerate(steps):
        step_path = f"{path}[{idx}]"
        if not isinstance(step, dict):
            issues.append(Issue("error", step_path, "step 必须是对象", ""))
            continue
        if not any(k in step for k in ("uses", "run", "checkout")):
            issues.append(Issue(
                "error", step_path, "step 必须有 uses / run / checkout 之一",
                "市场插件用 `uses: atomCode@1.*`，脚本用 `run: |`",
            ))
        uses = step.get("uses")
        if isinstance(uses, str) and not re.match(r"^[\w.-]+@[\w.*-]+$", uses):
            issues.append(Issue("error", f"{step_path}.uses", f"{uses!r} 不是 atomCode@version 格式", "如 `uses: linuxScript@1.*`"))


def _check_structure(doc: Any, issues: List[Issue]) -> None:
    if not isinstance(doc, dict):
        issues.append(Issue("error", "$", "YAML 顶层必须是对象", ""))
        return
    for key in doc:
        if key not in ROOT_KEYS:
            issues.append(Issue("error", f"$.{key}", "不是允许的顶层字段", f"允许：{', '.join(sorted(ROOT_KEYS))}"))
    if "on" in doc:
        _check_on(doc["on"], issues)
    if "variables" in doc:
        _check_variables(doc["variables"], issues)
    if "steps" in doc:
        _check_steps(doc["steps"], "$.steps", issues)
    for s_idx, stage in enumerate(doc.get("stages") or []):
        if not isinstance(stage, dict):
            continue
        for j_idx, job in enumerate(stage.get("jobs", {}).values() if isinstance(stage.get("jobs"), dict) else (stage.get("jobs") or [])):
            if isinstance(job, dict):
                _check_steps(job.get("steps"), f"$.stages[{s_idx}].jobs[{j_idx}].steps", issues)


# ==================== JSON Schema 校验 ====================

def find_schema_path(explicit: Optional[str] = None) -> Optional[str]:
    if explicit:
        return explicit if os.path.isfile(explicit) else None
    here = os.path.dirname(os.path.abspath(__file__))
    candidates = [
        os.path.join(here, SCHEMA_FILENAME),
        os.path.join(here, "..", "references", "generate", SCHEMA_FILENAME),
        os.path.join(here, "..", "references", SCHEMA_FILENAME),
        os.path.join(here, "..", "reference", "generate", SCHEMA_FILENAME),
        os.path.join(here, "..", "reference", SCHEMA_FILENAME),
        os.path.join(here, "..", "..", "..", ".docs", SCHEMA_FILENAME),
    ]
    for path in candidates:
        if os.path.isfile(path):
            return os.path.normpath(path)
    return None


def schema_validate(doc: Any, schema_path: str) -> Tuple[List[Issue], Optional[str]]:
    """返回 (issues, skip_reason)。jsonschema 未安装时 issues 为空并给出原因。"""
    try:
        import jsonschema  # type: ignore
    except ImportError:
        return [], "未安装 jsonschema，已跳过全量 schema 校验（pip install jsonschema 可开启）"

    with open(schema_path, "r", encoding="utf-8") as f:
        schema = json.load(f)

    validator = jsonschema.Draft7Validator(schema)
    issues: List[Issue] = []
    seen = set()
    for error in validator.iter_errors(doc):
        # oneOf/anyOf 会产生一堆同级噪音，取路径最深的子错误更接近真实问题
        best = error
        while best.context:
            best = max(best.context, key=lambda e: (len(list(e.absolute_path)), -len(e.message)))
        path = "$" + "".join(
            f"[{p}]" if isinstance(p, int) else f".{p}" for p in best.absolute_path
        )
        key = (path, best.message)
        if key in seen:
            continue
        seen.add(key)
        issues.append(Issue("error", path, best.message))
    return issues, None


# ==================== 自动修复 ====================

def autofix(doc: Any) -> Tuple[Any, List[str]]:
    """只修「唯一解」的机械错误，其余交给人。返回 (doc, 已修列表)。"""
    applied: List[str] = []
    if not isinstance(doc, dict):
        return doc, applied

    for trigger in _as_trigger_list(doc.get("on")) if "on" in doc else []:
        if not isinstance(trigger, dict):
            continue
        manual = trigger.get("manual")
        if isinstance(manual, dict) and "enabled" in manual:
            value = manual.pop("enabled")
            if manual:
                manual["enable"] = bool(value)
                applied.append("on.manual.enabled → on.manual.enable")
            else:
                trigger["manual"] = "enabled" if value else "disabled"
                applied.append("on.manual: {enabled: true} → manual: enabled")
        remote = trigger.get("remote")
        if isinstance(remote, dict) and isinstance(remote.get("enable"), bool):
            remote["enable"] = "enabled" if remote["enable"] else "disabled"
            applied.append("on.remote.enable 布尔 → 字符串")

    for name, node in (doc.get("variables") or {}).items():
        if not isinstance(node, dict):
            continue
        props = node.get("props")
        if isinstance(props, dict):
            ptype = str(props.get("type", "")).strip().lower()
            if props.get("type") not in PROPS_TYPE_ENUM and ptype in PROPS_TYPE_ALIASES:
                props["type"] = PROPS_TYPE_ALIASES[ptype]
                applied.append(f"variables.{name}.props.type → {props['type']}")
        for key in list(node.keys()):
            if key not in VARIABLE_KEYS and key in PROPS_KEYS:
                node.setdefault("props", {})[key] = node.pop(key)
                applied.append(f"variables.{name}.{key} → variables.{name}.props.{key}")
    return doc, applied


# ==================== 对外入口 ====================

def lint_yaml_text(
    yaml_text: str, schema_path: Optional[str] = None, use_schema: bool = True
) -> Tuple[List[Issue], Optional[str]]:
    """返回 (issues, note)。note 为 schema 校验被跳过的原因。"""
    try:
        doc = load_yaml(yaml_text)
    except yaml.YAMLError as e:
        return [Issue("error", "$", f"YAML 语法错误：{e}")], None

    issues: List[Issue] = []
    _check_structure(doc, issues)

    note: Optional[str] = None
    if use_schema:
        path = find_schema_path(schema_path)
        if path:
            schema_issues, skip = schema_validate(doc, path)
            note = skip
            # 规则校验已经在更精确的路径上报过的问题，不再重复贴 schema 原文
            covered = [i.path for i in issues if i.level == "error"]
            issues.extend(
                i for i in schema_issues
                if not any(p == i.path or p.startswith(i.path + ".") for p in covered)
            )
        else:
            note = f"未找到 {SCHEMA_FILENAME}，仅做规则校验"
    return issues, note


def format_report(issues: List[Issue], note: Optional[str] = None) -> str:
    errors = [i for i in issues if i.level == "error"]
    warnings = [i for i in issues if i.level == "warning"]
    lines: List[str] = []
    if errors:
        lines.append(f"YAML 校验不通过：{len(errors)} 个错误")
        lines.extend("  " + i.render() for i in errors)
    if warnings:
        lines.append(f"提醒：{len(warnings)} 个可能的问题")
        lines.extend("  " + i.render() for i in warnings)
    if not errors and not warnings:
        lines.append("YAML 校验通过")
    if note:
        lines.append(f"（{note}）")
    return "\n".join(lines)


def main() -> None:
    parser = argparse.ArgumentParser(
        description="蓝盾普通流水线 YAML 本地校验（save-draft 前置）",
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--yaml-file", help="待校验的 YAML 文件")
    parser.add_argument("--yaml", help="待校验的 YAML 字符串")
    parser.add_argument("--schema", help="pipeline-yaml-schema.json 路径（默认自动查找）")
    parser.add_argument("--no-schema", action="store_true", help="只跑规则校验，跳过 JSON Schema")
    parser.add_argument("--fix", action="store_true", help="自动修复机械错误并输出修好的 YAML")
    parser.add_argument("--in-place", action="store_true", help="配合 --fix，直接写回原文件")
    parser.add_argument("--json", dest="as_json", action="store_true", help="以 JSON 输出结果")
    args = parser.parse_args()

    if args.yaml_file:
        try:
            with open(args.yaml_file, "r", encoding="utf-8") as f:
                text = f.read()
        except OSError as e:
            print(f"读取文件失败: {e}", file=sys.stderr)
            sys.exit(1)
    elif args.yaml:
        text = args.yaml
    else:
        print("错误：需要 --yaml-file 或 --yaml", file=sys.stderr)
        sys.exit(1)

    if args.fix:
        try:
            doc = load_yaml(text)
        except yaml.YAMLError as e:
            print(f"YAML 语法错误，无法自动修复：{e}", file=sys.stderr)
            sys.exit(2)
        doc, applied = autofix(doc)
        text = yaml.dump(doc, allow_unicode=True, sort_keys=False, default_flow_style=False)
        if args.in_place and args.yaml_file:
            with open(args.yaml_file, "w", encoding="utf-8") as f:
                f.write(text)
            print(f"已修复 {len(applied)} 处并写回 {args.yaml_file}：", file=sys.stderr)
        else:
            print(f"已修复 {len(applied)} 处：", file=sys.stderr)
        for item in applied:
            print(f"  - {item}", file=sys.stderr)
        if not args.in_place:
            sys.stdout.write(text)

    issues, note = lint_yaml_text(text, args.schema, use_schema=not args.no_schema)

    if args.as_json:
        print(json.dumps({
            "ok": not any(i.level == "error" for i in issues),
            "note": note,
            "issues": [{"level": i.level, "path": i.path, "message": i.message, "fix": i.fix} for i in issues],
        }, ensure_ascii=False, indent=2))
    else:
        print(format_report(issues, note), file=sys.stderr if args.fix else sys.stdout)

    sys.exit(2 if any(i.level == "error" for i in issues) else 0)


if __name__ == "__main__":
    main()
