package vault

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/ide"
	"github.com/shichao402/Dec/pkg/paths"
	"github.com/shichao402/Dec/pkg/types"
)

// Vault 个人知识仓库
type Vault struct {
	Dir   string
	Index *VaultIndex
	Git   *GitOps
}

// GetVaultDir 获取 vault 本地目录 (~/.dec/vault/)
func GetVaultDir() (string, error) {
	rootDir, err := paths.GetRootDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(rootDir, "vault"), nil
}

// IsInitialized 检查 vault 是否已初始化
func IsInitialized() (bool, error) {
	vaultDir, err := GetVaultDir()
	if err != nil {
		return false, err
	}

	info, err := os.Stat(vaultDir)
	if err != nil {
		return false, nil
	}

	if !info.IsDir() {
		return false, nil
	}

	gitDir := filepath.Join(vaultDir, ".git")
	if _, err := os.Stat(gitDir); err != nil {
		return false, nil
	}

	return true, nil
}

// Open 打开已有的 vault
func Open() (*Vault, error) {
	ok, err := IsInitialized()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("Vault 未初始化\n\n运行 dec vault init 初始化个人知识仓库")
	}

	vaultDir, err := GetVaultDir()
	if err != nil {
		return nil, err
	}

	index, err := LoadIndex(vaultDir)
	if err != nil {
		return nil, err
	}

	return &Vault{
		Dir:   vaultDir,
		Index: index,
		Git:   NewGitOps(vaultDir),
	}, nil
}

// Init 使用已有的 GitHub 仓库初始化 vault
func Init(repoURL string) (*Vault, error) {
	vaultDir, err := GetVaultDir()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(filepath.Join(vaultDir, ".git")); err == nil {
		return nil, fmt.Errorf("Vault 已初始化: %s", vaultDir)
	}

	git := NewGitOps(vaultDir)
	if err := git.Clone(repoURL); err != nil {
		return nil, fmt.Errorf("克隆仓库失败: %w", err)
	}

	index, err := LoadIndex(vaultDir)
	if err != nil {
		return nil, err
	}

	v := &Vault{Dir: vaultDir, Index: index, Git: git}
	if err := v.ensureDirs(); err != nil {
		return nil, err
	}

	return v, nil
}

// InitCreate 创建新的 GitHub 仓库并初始化 vault
func InitCreate(repoName string) (*Vault, error) {
	vaultDir, err := GetVaultDir()
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(filepath.Join(vaultDir, ".git")); err == nil {
		return nil, fmt.Errorf("Vault 已初始化: %s", vaultDir)
	}

	repoURL, err := CreateGitHubRepo(repoName)
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(vaultDir, 0755); err != nil {
		return nil, fmt.Errorf("创建 vault 目录失败: %w", err)
	}

	git := NewGitOps(vaultDir)
	if err := git.Init(); err != nil {
		return nil, fmt.Errorf("初始化 Git 仓库失败: %w", err)
	}
	if err := git.SetRemote(repoURL); err != nil {
		return nil, fmt.Errorf("设置远程仓库失败: %w", err)
	}

	v := &Vault{
		Dir:   vaultDir,
		Index: &VaultIndex{Version: indexVersion},
		Git:   git,
	}

	if err := v.ensureDirs(); err != nil {
		return nil, err
	}
	if err := v.Index.Save(vaultDir); err != nil {
		return nil, err
	}

	// 创建 README
	readme := "# Dec Vault\n\n个人 AI 知识仓库，由 [Dec](https://github.com/shichao402/Dec) 管理。\n"
	if err := os.WriteFile(filepath.Join(vaultDir, "README.md"), []byte(readme), 0644); err != nil {
		return nil, err
	}

	if err := git.Add("."); err != nil {
		return nil, err
	}
	if err := git.Commit("init: 初始化 Dec Vault"); err != nil {
		return nil, err
	}

	// 尝试推送，失败不阻塞（可能远程仓库还没准备好）
	_ = git.Push()

	return v, nil
}

// ensureDirs 确保 vault 子目录存在
func (v *Vault) ensureDirs() error {
	dirs := []string{"skills", "rules", "mcp"}
	for _, d := range dirs {
		if err := os.MkdirAll(filepath.Join(v.Dir, d), 0755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", d, err)
		}
	}
	return nil
}

// ManagedName 返回 Dec 托管资源名称
func ManagedName(name string) string {
	if strings.HasPrefix(name, "dec-") {
		return name
	}
	return "dec-" + name
}

// Save 保存资产到 vault
func (v *Vault) Save(itemType, sourcePath string, tags []string) (string, []string, error) {
	absSource, err := filepath.Abs(sourcePath)
	if err != nil {
		return "", nil, fmt.Errorf("解析路径失败: %w", err)
	}

	info, err := os.Stat(absSource)
	if err != nil {
		return "", nil, fmt.Errorf("源路径不存在: %w", err)
	}

	var name, description, destRelPath string

	switch itemType {
	case "skill":
		name, description, err = v.saveSkill(absSource, info)
		if err != nil {
			return "", nil, err
		}
		destRelPath = filepath.Join("skills", name)

	case "rule":
		name, description, err = v.saveRule(absSource)
		if err != nil {
			return "", nil, err
		}
		destRelPath = filepath.Join("rules", name+".mdc")

	case "mcp":
		name, description, err = v.saveMCP(absSource)
		if err != nil {
			return "", nil, err
		}
		destRelPath = filepath.Join("mcp", name+".json")

	default:
		return "", nil, fmt.Errorf("不支持的资产类型: %s (支持: skill, rule, mcp)", itemType)
	}

	v.Index.AddOrUpdate(VaultItem{
		Name:        name,
		Type:        itemType,
		Description: description,
		Tags:        tags,
		Path:        destRelPath,
	})

	if err := v.Index.Save(v.Dir); err != nil {
		return "", nil, fmt.Errorf("更新索引失败: %w", err)
	}

	if err := v.Git.Add("."); err != nil {
		return "", nil, fmt.Errorf("Git add 失败: %w", err)
	}
	if err := v.Git.Commit(fmt.Sprintf("save: %s %s", itemType, name)); err != nil {
		return "", nil, fmt.Errorf("Git commit 失败: %w", err)
	}

	var warnings []string
	hasRemote, err := v.Git.HasRemote()
	if err != nil {
		return "", nil, fmt.Errorf("检查 Git 远程失败: %w", err)
	}
	if hasRemote {
		if err := v.Git.Push(); err != nil {
			warnings = append(warnings, fmt.Sprintf("推送到远程仓库失败，资产已保存到本地: %v", err))
		}
	}

	return name, warnings, nil
}

// saveSkill 保存 skill 到 vault（skill 是目录）
func (v *Vault) saveSkill(absSource string, info os.FileInfo) (string, string, error) {
	if !info.IsDir() {
		return "", "", fmt.Errorf("skill 必须是目录（包含 SKILL.md）: %s", absSource)
	}

	skillMD := filepath.Join(absSource, "SKILL.md")
	if _, err := os.Stat(skillMD); err != nil {
		return "", "", fmt.Errorf("skill 目录缺少 SKILL.md: %s", absSource)
	}

	name, desc := parseSkillMetadata(skillMD)
	if name == "" {
		name = filepath.Base(absSource)
	}

	destDir := filepath.Join(v.Dir, "skills", name)
	if err := CopyDir(absSource, destDir); err != nil {
		return "", "", fmt.Errorf("复制 skill 失败: %w", err)
	}

	return name, desc, nil
}

// saveRule 保存 rule 到 vault（单个 .mdc 文件）
func (v *Vault) saveRule(absSource string) (string, string, error) {
	if !strings.HasSuffix(absSource, ".mdc") {
		return "", "", fmt.Errorf("rule 文件必须是 .mdc 格式: %s", absSource)
	}

	name := strings.TrimSuffix(filepath.Base(absSource), ".mdc")
	destPath := filepath.Join(v.Dir, "rules", filepath.Base(absSource))

	if err := copyFile(absSource, destPath); err != nil {
		return "", "", fmt.Errorf("复制 rule 失败: %w", err)
	}

	desc := ""
	if d, err := parseRuleDescription(absSource); err == nil {
		desc = d
	}

	return name, desc, nil
}

// saveMCP 保存 MCP 配置到 vault（单个 .json 文件）
func (v *Vault) saveMCP(absSource string) (string, string, error) {
	if !strings.HasSuffix(absSource, ".json") {
		return "", "", fmt.Errorf("MCP 配置必须是 .json 格式: %s", absSource)
	}

	if _, err := loadMCPServer(absSource); err != nil {
		return "", "", fmt.Errorf("MCP 配置必须是单个 server 片段 JSON（包含 command/args/env）: %w", err)
	}

	name := strings.TrimSuffix(filepath.Base(absSource), ".json")
	destPath := filepath.Join(v.Dir, "mcp", filepath.Base(absSource))

	if err := copyFile(absSource, destPath); err != nil {
		return "", "", fmt.Errorf("复制 MCP 配置失败: %w", err)
	}

	return name, "", nil
}

// Find 搜索 vault
func (v *Vault) Find(query string) []VaultItem {
	return v.Index.Find(query)
}

// List 列出 vault 中的资产
func (v *Vault) List(itemType string) []VaultItem {
	return v.Index.List(itemType)
}

// Pull 从 vault 复制资产到目标项目的所有 IDE
func (v *Vault) Pull(itemType, name, projectRoot string, ideNames []string) ([]string, error) {
	item := v.Index.Get(itemType, name)
	if item == nil {
		return nil, fmt.Errorf("未找到 %s: %s", itemType, name)
	}

	if len(ideNames) == 0 {
		ideNames = []string{"cursor"}
	}

	srcPath := filepath.Join(v.Dir, item.Path)
	var localPaths []string

	for _, ideName := range ideNames {
		ideImpl := ide.Get(ideName)

		switch itemType {
		case "skill":
			destDir := filepath.Join(ideImpl.SkillsDir(projectRoot), ManagedName(name))
			if err := CopyDir(srcPath, destDir); err != nil {
				return nil, fmt.Errorf("复制 skill 到项目失败: %w", err)
			}
			relPath, err := filepath.Rel(projectRoot, destDir)
			if err != nil {
				return nil, err
			}
			localPaths = append(localPaths, relPath)

		case "rule":
			destDir := ideImpl.RulesDir(projectRoot)
			if err := os.MkdirAll(destDir, 0755); err != nil {
				return nil, err
			}
			destPath := filepath.Join(destDir, ManagedName(name)+".mdc")
			if err := copyFile(srcPath, destPath); err != nil {
				return nil, fmt.Errorf("复制 rule 到项目失败: %w", err)
			}
			relPath, err := filepath.Rel(projectRoot, destPath)
			if err != nil {
				return nil, err
			}
			localPaths = append(localPaths, relPath)

		case "mcp":
			server, err := loadMCPServer(srcPath)
			if err != nil {
				return nil, fmt.Errorf("读取 MCP 配置失败: %w", err)
			}

			existingConfig, err := ideImpl.LoadMCPConfig(projectRoot)
			if err != nil {
				return nil, fmt.Errorf("加载 IDE MCP 配置失败: %w", err)
			}
			if existingConfig.MCPServers == nil {
				existingConfig.MCPServers = make(map[string]types.MCPServer)
			}
			existingConfig.MCPServers[ManagedName(name)] = server

			if err := ideImpl.WriteMCPConfig(projectRoot, existingConfig); err != nil {
				return nil, fmt.Errorf("写入 MCP 配置到项目失败: %w", err)
			}

			relPath, err := filepath.Rel(projectRoot, ideImpl.MCPConfigPath(projectRoot))
			if err != nil {
				return nil, err
			}
			localPaths = append(localPaths, relPath)

		default:
			return nil, fmt.Errorf("不支持的资产类型: %s", itemType)
		}
	}

	return localPaths, nil
}

// Push 推送 vault 变更到远程仓库
func (v *Vault) Push() error {
	hasRemote, err := v.Git.HasRemote()
	if err != nil {
		return err
	}
	if !hasRemote {
		return fmt.Errorf("Vault 没有配置远程仓库")
	}

	return v.Git.Push()
}

// Refresh 从远程仓库拉取最新内容
func (v *Vault) Refresh() error {
	hasRemote, err := v.Git.HasRemote()
	if err != nil {
		return err
	}
	if !hasRemote {
		return nil
	}

	if err := v.Git.Pull(); err != nil {
		return fmt.Errorf("拉取远程更新失败: %w", err)
	}

	idx, err := LoadIndex(v.Dir)
	if err != nil {
		return err
	}
	v.Index = idx

	return nil
}

// parseSkillMetadata 从 SKILL.md 解析 name 和 description
func parseSkillMetadata(skillMDPath string) (name, description string) {
	data, err := os.ReadFile(skillMDPath)
	if err != nil {
		return "", ""
	}

	content := string(data)
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}

	endIdx := strings.Index(content[3:], "---")
	if endIdx < 0 {
		return "", ""
	}

	frontMatter := content[3 : endIdx+3]
	for _, line := range strings.Split(frontMatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		}
		if strings.HasPrefix(line, "description:") {
			desc := strings.TrimSpace(strings.TrimPrefix(line, "description:"))
			desc = strings.Trim(desc, "\"'>")
			if desc != "" {
				description = desc
			}
		}
	}

	return name, description
}

// parseRuleDescription 从 .mdc 文件解析描述
func parseRuleDescription(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	content := string(data)

	if strings.HasPrefix(content, "---") {
		endIndex := strings.Index(content[3:], "---")
		if endIndex > 0 {
			frontMatter := content[3 : endIndex+3]
			for _, line := range strings.Split(frontMatter, "\n") {
				line = strings.TrimSpace(line)
				if strings.HasPrefix(line, "description:") {
					return strings.TrimSpace(strings.TrimPrefix(line, "description:")), nil
				}
			}
		}
	}

	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "# ") {
			return strings.TrimPrefix(line, "# "), nil
		}
	}

	return "", nil
}

func loadMCPServer(path string) (types.MCPServer, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return types.MCPServer{}, err
	}

	var server types.MCPServer
	if err := json.Unmarshal(data, &server); err != nil {
		return types.MCPServer{}, err
	}
	if server.Command == "" {
		return types.MCPServer{}, fmt.Errorf("缺少 command 字段")
	}

	return server, nil
}

// copyFile 复制单个文件
func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}

	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	_, err = io.Copy(dstFile, srcFile)
	return err
}

// CopyDir 递归复制目录
func CopyDir(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}

	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())

		if entry.IsDir() {
			if err := CopyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}

	return nil
}
