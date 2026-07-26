package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
)

// AddSecretResult 是一次「登记新 secret」的结果。
type AddSecretResult struct {
	// Folder 是 Note 落进的 Bitwarden folder。
	Folder string
	// LandingPath 是项目根相对落地路径，同时就是 Note 名。
	LandingPath string
}

// AddProjectSecret 把项目内一个已存在的文件登记成 Bitwarden Secure Note。
//
// 这是新增 secret 的唯一入口。push 只按远端 note 列表更新已有条目，不会自动发现
// 本地新文件——落地路径散在项目根，自动扫描分不清哪些文件该进保险库。
func AddProjectSecret(ctx context.Context, projectRoot, folder, relPath string, reporter Reporter) (*AddSecretResult, error) {
	reporter = defaultReporter(reporter)
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return nil, fmt.Errorf("必须指定 Bitwarden folder")
	}
	rel := strings.TrimSpace(relPath)
	if rel == "" {
		return nil, fmt.Errorf("必须指定项目根相对落地路径")
	}
	rel = filepath.ToSlash(rel)

	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return nil, fmt.Errorf("Bitwarden 未配置，请先在 Settings 页填写连接信息")
	}

	info, err := os.Stat(filepath.Join(projectRoot, filepath.FromSlash(rel)))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%s 不存在：请先在项目里把文件放到消费者读取的位置，再登记", rel)
		}
		return nil, err
	}
	if info.IsDir() {
		return nil, fmt.Errorf("%s 是目录：一个 Secure Note 对应一个文件，请逐个登记", rel)
	}

	if !secrets.HasSession() {
		if err := ensureBitwardenSession(ctx, reporter, "secrets.add"); err != nil {
			return nil, err
		}
	}

	if err := secrets.AddSecureNote(ctx, secretsClientFactory(), projectRoot, folder, rel); err != nil {
		return nil, err
	}
	emit(reporter, EventInfo, "secrets.add",
		fmt.Sprintf("已登记 %s → Bitwarden folder %q", rel, folder), nil)
	return &AddSecretResult{Folder: folder, LandingPath: rel}, nil
}

// SuggestSecretFolders 列出当前项目可选的 Bitwarden folder：project folder + 已启用 bundle 的 folder。
// 第一项是 project folder，作为登记新 secret 时的默认归属。
func SuggestSecretFolders(projectRoot string) ([]string, error) {
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}
	plan, err := planSecretsSync(projectRoot, projectConfig.EnabledBundles, cfg)
	if err != nil {
		return nil, err
	}

	seen := make(map[string]struct{})
	folders := make([]string, 0, len(plan.EnabledBundles)+1)
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" {
			return
		}
		if _, dup := seen[name]; dup {
			return
		}
		seen[name] = struct{}{}
		folders = append(folders, name)
	}

	add(plan.ProjectSecretsName)
	bundleFolders := make([]string, 0, len(plan.EnabledBundles))
	for _, bundleName := range plan.EnabledBundles {
		bundleFolders = append(bundleFolders, cfg.ResolveBinding(bundleName).SecretsBundleName)
	}
	sort.Strings(bundleFolders)
	for _, name := range bundleFolders {
		add(name)
	}
	return folders, nil
}
