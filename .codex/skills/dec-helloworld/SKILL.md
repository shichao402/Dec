<!-- 本文件由 `dec pull` 从 .dec/cache/default/ 渲染生成，请勿直接编辑。
     修改流程：编辑 .dec/cache/default/... → dec push → dec pull 验证 -->

---
name: helloworld
description: Create minimal hello world examples for quick demos, smoke tests, and starter snippets. Use when the user asks for a simple runnable example in a specific language, framework, runtime, or file format and the goal is to show the smallest working program.
---

# Hello World

Provide the smallest runnable example that matches the user's requested language or environment.
If the user does not specify a target, default to a single-file console example with one clear run command.

## Workflow

1. Identify the target language, runtime, or framework.
2. Produce the minimal code needed to print or display `Hello, World!`.
3. Keep dependencies at zero unless the user explicitly asks for a framework example.
4. Include the exact filename and one run command.
5. If multiple files are required, explain briefly why and keep the structure minimal.

## Output Rules

- Prefer a single file.
- Use conventional filenames such as `main.py`, `index.js`, `Main.java`, or `src/main.rs`.
- Match the platform's standard entrypoint and style.
- Do not add extra architecture, tests, or setup unless the user asks for them.
- If the example cannot be run without installation, mention the one required install step.

## Examples

User request: `写一个 Python hello world`

Return a tiny `main.py` example and `python3 main.py`.

User request: `给我一个 React 的 hello world`

Return the smallest component-based example and the minimal command needed to start it.

User request: `做一个 C 语言 helloworld`

Return a single C source file and a compile plus run command.
