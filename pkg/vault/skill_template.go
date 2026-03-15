package vault

import (
	"fmt"
	"os"
	"path/filepath"
)

const decSkillMD = `---
name: dec
description: >
  Manage personal AI knowledge vault - save, find, and reuse Skills, Rules,
  and MCP configs across projects and machines. Use when the user wants to
  save an asset for later, find a previously created asset, sync project
  declarations, or manage their Dec knowledge vault.
---
# Dec - AI Knowledge Vault

Dec is a personal knowledge vault that helps you accumulate and reuse AI assets
(Skills, Rules, MCP configs) across projects and machines.

## When to Use

- User wants to **save** a skill/rule/MCP for reuse across projects
- User mentions needing a **previously created** skill or rule
- User wants to **find** or **search** for something they made before
- User wants to **sync** project assets to the current IDE setup
- User mentions "dec", "vault", or "knowledge vault"
- User just created or updated a skill or rule (proactively suggest saving)

## Prerequisites

Dec CLI must be installed. Verify:

` + "```bash" + `
dec --version
` + "```" + `

## Vault Initialization

Before using vault features, initialize the personal vault.

**Option A: Clone an existing vault repo**

Ask the user for their vault GitHub repo URL, then run:

` + "```bash" + `
dec vault init --repo https://github.com/<user>/<repo>
` + "```" + `

**Option B: Create a new vault repo (requires gh CLI)**

` + "```bash" + `
dec vault init --create my-dec-vault
` + "```" + `

If the user doesn't know whether they have a vault, check:

` + "```bash" + `
dec vault list
` + "```" + `

If this returns an error about vault not initialized, guide the user to choose
Option A or B above.

## Quick Reference

| User Intent | Command |
|-------------|---------|
| Save a skill to vault | ` + "`dec vault save skill <skill-dir-path>`" + ` |
| Save with tags | ` + "`dec vault save skill <path> --tag <t1> --tag <t2>`" + ` |
| Save a rule to vault | ` + "`dec vault save rule <rule-file.mdc>`" + ` |
| Save an MCP config | ` + "`dec vault save mcp <server.json>`" + ` |
| Find by keyword | ` + "`dec vault find \"<query>\"`" + ` |
| Pull skill to project | ` + "`dec vault pull skill <name>`" + ` |
| Pull rule to project | ` + "`dec vault pull rule <name>`" + ` |
| Pull MCP to project | ` + "`dec vault pull mcp <name>`" + ` |
| List all vault items | ` + "`dec vault list`" + ` |
| List only skills | ` + "`dec vault list --type skill`" + ` |
| Check local changes | ` + "`dec vault status`" + ` |
| Sync declared assets to IDE | ` + "`dec sync`" + ` |

## Project-Level Vault Declaration (vault.yaml)

Projects declare which vault assets they need in ` + "`.dec/config/vault.yaml`" + `:

` + "```yaml" + `
vault_skills:
  - create-api-test
  - fix-cors-issue

vault_rules:
  - my-security-rule

vault_mcps:
  - postgres-tool
` + "```" + `

When ` + "`dec sync`" + ` runs, it reads ` + "`.dec/config/vault.yaml`" + ` and deploys
the declared assets as managed ` + "`dec-*`" + ` copies or managed MCP entries. This
ensures team members or new machines get the right assets automatically without
overwriting the user's original local files.

To add an asset manually after saving it to vault:

1. Open ` + "`.dec/config/vault.yaml`" + `
2. Add the asset name under ` + "`vault_skills`" + `, ` + "`vault_rules`" + `, or ` + "`vault_mcps`" + `
3. Run ` + "`dec sync`" + `

## Detailed Operations

### Saving a Skill

When the user creates or modifies a skill and wants to preserve it:

1. Identify the skill directory (must contain SKILL.md)
2. Run the save command with optional tags:

` + "```bash" + `
dec vault save skill .cursor/skills/my-skill --tag testing --tag api
` + "```" + `

3. Dec copies the skill to the vault, updates the index, and pushes to GitHub
4. Confirm success to the user

**Re-saving (updating):** Running save again on the same skill updates the vault
copy. Previously set tags are preserved unless new --tag flags are provided.

### Finding an Asset

When the user needs a previously saved asset:

1. Run find with a descriptive query:

` + "```bash" + `
dec vault find "API test"
` + "```" + `

2. Review the results (name, type, description, tags)
3. If the user confirms, pull the desired asset

### Pulling an Asset

When the user wants to use a vault asset in the current project:

` + "```bash" + `
dec vault pull skill create-api-test
` + "```" + `

Dec reads the project IDE config (` + "`.dec/config/ides.yaml`" + `), deploys
the asset to all configured IDE directories as managed ` + "`dec-*`" + ` copies or
managed MCP entries, and adds the asset to ` + "`.dec/config/vault.yaml`" + ` so
future ` + "`dec sync`" + ` runs keep it.

### MCP Format

Vault MCP assets are saved as a single MCP server fragment JSON, not a full
` + "`mcp.json`" + ` file. The JSON should contain:

` + "```json" + `
{
  "command": "npx",
  "args": ["-y", "some-mcp-server"],
  "env": {
    "API_KEY": "${API_KEY}"
  }
}
` + "```" + `

Dec merges this server fragment into each IDE's live MCP config using a managed
` + "`dec-*`" + ` server name.

### Proactive Change Detection

**IMPORTANT: When an asset has been modified during the session, proactively:**

1. Run ` + "`dec vault status`" + ` to check for changes
2. If changes are detected, inform the user and suggest saving
3. Run the save command if the user agrees

` + "```bash" + `
dec vault status
` + "```" + `

If changes are detected, ask the user if they want to save the updates to vault.

## Important Notes

- Skills are saved as **directories** containing SKILL.md and optional supporting files
- Rules are saved as single **.mdc** files
- MCP configs are saved as single **server fragment JSON** files with ` + "`command`" + ` / ` + "`args`" + ` / ` + "`env`" + `
- The vault is a Git repository - all changes are version-controlled
- ` + "`dec vault pull`" + ` adds the asset to ` + "`.dec/config/vault.yaml`" + ` so future ` + "`dec sync`" + ` runs keep it
- Re-saving preserves existing tags when no --tag flag is provided
- Projects should commit Dec config files, not Dec-generated managed copies
`

// InstallDecSkill 安装 Dec Skill 到用户级目录
func InstallDecSkill() error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("获取 HOME 目录失败: %w", err)
	}

	skillDir := filepath.Join(homeDir, ".cursor", "skills", "dec")
	skillPath := filepath.Join(skillDir, "SKILL.md")

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		return fmt.Errorf("创建 Skill 目录失败: %w", err)
	}

	if err := os.WriteFile(skillPath, []byte(decSkillMD), 0644); err != nil {
		return fmt.Errorf("写入 SKILL.md 失败: %w", err)
	}

	return nil
}

// IsDecSkillInstalled 检查 Dec Skill 是否已安装
func IsDecSkillInstalled() bool {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return false
	}

	skillPath := filepath.Join(homeDir, ".cursor", "skills", "dec", "SKILL.md")
	_, err = os.Stat(skillPath)
	return err == nil
}

// GetDecSkillPath 获取 Dec Skill 安装路径
func GetDecSkillPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".cursor", "skills", "dec", "SKILL.md"), nil
}
