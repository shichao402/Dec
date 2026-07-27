package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shichao402/Dec/pkg/config"
	"github.com/shichao402/Dec/pkg/secrets"
)

type secretsSyncPlan struct {
	EnabledBundles     []string
	ProjectSecretsName string
	Total              int
}

func planSecretsSync(projectRoot string, enabledBundles []string, cfg *secrets.Config) (*secretsSyncPlan, error) {
	plan := &secretsSyncPlan{
		EnabledBundles: append([]string(nil), enabledBundles...),
	}
	mgr := config.NewProjectConfigManager(projectRoot)
	projectConfig, err := mgr.LoadProjectConfig()
	if err != nil {
		return nil, err
	}
	projectName, _ := ResolveProjectName(projectRoot, projectConfig)
	if cfg != nil {
		if name, enabled := cfg.ResolveProjectSecrets(projectName); enabled {
			plan.ProjectSecretsName = name
		}
	}
	plan.Total = len(plan.EnabledBundles)
	if plan.ProjectSecretsName != "" {
		plan.Total++
	}
	return plan, nil
}

// secretsClientFactory 供测试注入 stub Client。
var secretsClientFactory = secrets.DefaultClient

type secretsPullSummary struct {
	SkippedReason string
	NoteCount     int
	SSHKeyCount   int
	LandingPaths  []string
	SSHKeyNames   []string
}

// secretsPullTarget 是一个待同步的 secrets 目标（bundle 或 project 级）。
// 两者唯一的区别是 Bitwarden folder 来源，同步链路完全一致。
type secretsPullTarget struct {
	Label         string
	DecBundleName string
	Binding       secrets.BundleBinding
}

// warnUnignoredSecrets 提示落地的密文件尚未被 .gitignore 忽略。
// dec 不代改 .gitignore，只给出建议行。
func warnUnignoredSecrets(projectRoot string, landingPaths []string, reporter Reporter) {
	unignored := secrets.UnignoredLandingPaths(projectRoot, landingPaths)
	if len(unignored) == 0 {
		return
	}
	emit(reporter, EventInfo, "pull.secrets",
		fmt.Sprintf("⚠️  %d 个密文件未被 .gitignore 忽略，建议在 .gitignore 中加入：", len(unignored)), nil)
	for _, rel := range unignored {
		emit(reporter, EventInfo, "pull.secrets", "  /"+rel, nil)
	}
}

// pullEnabledSecretsBundles 拉取全部已启用 bundle 与 project 级 secrets。
//
// 不做「停用即清理」：落地路径就是消费者路径，散在项目根，没有一个可以安全
// os.RemoveAll 的目录。bundle 停用后的清理走 Delete 页显式单条确认。
// SSH Key 同理：Pull 不清理远端已移除的旧 key，清理由 Delete 页完成。
func pullEnabledSecretsBundles(ctx context.Context, projectRoot string, enabledBundles []string, reporter Reporter) (*secretsPullSummary, error) {
	reporter = defaultReporter(reporter)
	summary := &secretsPullSummary{}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		summary.SkippedReason = "Bitwarden 未配置"
		emit(reporter, EventInfo, "pull.secrets", "Bitwarden 未配置，跳过 secrets 同步", nil)
		return summary, nil
	}

	cfg, err := loadSecretsConfigForPull()
	if err != nil {
		return nil, err
	}
	plan, err := planSecretsSync(projectRoot, enabledBundles, cfg)
	if err != nil {
		return nil, err
	}
	if plan.Total == 0 {
		summary.SkippedReason = "无已启用 bundle 或 project secrets"
		emit(reporter, EventInfo, "pull.secrets", "无已启用 bundle 或 project secrets，跳过 secrets 同步", nil)
		return summary, nil
	}

	if !secrets.HasSession() {
		emit(reporter, EventInfo, "pull.secrets", "[auth] pull: Bitwarden session required", nil)
		if err := ensureBitwardenSession(ctx, reporter, "pull.secrets"); err != nil {
			return nil, err
		}
	}
	if !secrets.HasUserKey() {
		return nil, fmt.Errorf("Bitwarden vault 密钥未就绪，请重新解锁")
	}

	client := secretsClientFactory()
	total := plan.Total
	emit(reporter, EventInfo, "pull.secrets", fmt.Sprintf("同步 %d 个 secrets 目标（bundle + project）", total), &Progress{Phase: "secrets", Current: 0, Total: total})

	targets := make([]secretsPullTarget, 0, total)
	for _, bundleName := range plan.EnabledBundles {
		targets = append(targets, secretsPullTarget{
			Label:         fmt.Sprintf("secrets bundle %q", bundleName),
			DecBundleName: bundleName,
			Binding:       cfg.ResolveBinding(bundleName),
		})
	}
	if plan.ProjectSecretsName != "" {
		targets = append(targets, secretsPullTarget{
			Label:         fmt.Sprintf("project secrets %q", plan.ProjectSecretsName),
			DecBundleName: secrets.ProjectSecretsDecBundleName,
			Binding:       secrets.ProjectSecretsBinding(plan.ProjectSecretsName),
		})
	}

	// 先全部取回并做一次全局校验，再统一落地：跨 folder 撞同一路径必须在写任何
	// 文件之前发现，否则先写的那个已经落盘了。SSH Key 同批校验失败时 Note 也不写。
	fetchedNotes := make([][]secrets.SecureNote, len(targets))
	fetchedKeys := make([][]secrets.SSHKeyLanding, len(targets))
	var candidates []secrets.LandingCandidate
	seenSSHFiles := make(map[string]string) // filename base -> label
	seenSSHHosts := make(map[string]string) // host -> label
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress := &Progress{Phase: "secrets", Current: i + 1, Total: total}
		emit(reporter, EventInfo, "pull.secrets",
			fmt.Sprintf("拉取 %s (Bitwarden folder: %s)", target.Label, target.Binding.SecretsBundleName), progress)

		notes, keys, pullErr := secrets.ResolveBundle(ctx, client, secrets.PullBundleRequest{
			ProjectRoot:   projectRoot,
			DecBundleName: target.DecBundleName,
			Binding:       target.Binding,
		})
		if pullErr != nil {
			return nil, fmt.Errorf("拉取 %s 失败: %w", target.Label, pullErr)
		}
		fetchedNotes[i] = notes
		for _, note := range notes {
			candidates = append(candidates, secrets.LandingCandidate{
				Folder:       target.Binding.SecretsBundleName,
				RelativePath: note.RelativePath,
			})
		}

		landings, prepErr := secrets.PrepareSSHKeyLandings(target.DecBundleName, keys)
		if prepErr != nil {
			return nil, fmt.Errorf("校验 %s 的 SSH Key 失败: %w", target.Label, prepErr)
		}
		for _, landing := range landings {
			base := filepath.Base(landing.PrivatePath)
			if prev, ok := seenSSHFiles[base]; ok {
				return nil, fmt.Errorf("SSH Key 文件名冲突: %s 同时由 %s 与 %s 产生", base, prev, target.Label)
			}
			seenSSHFiles[base] = target.Label
			for _, host := range landing.Hosts {
				if prev, ok := seenSSHHosts[host]; ok {
					return nil, fmt.Errorf("SSH host %q 冲突: 同时由 %s 与 %s 声明", host, prev, target.Label)
				}
				seenSSHHosts[host] = target.Label
			}
		}
		fetchedKeys[i] = landings
	}

	emit(reporter, EventInfo, "pull.secrets", "校验落地路径（.dec/ 零重叠、跨 folder 冲突、git 跟踪）", nil)
	if err := secrets.ValidateLandingPaths(projectRoot, candidates); err != nil {
		emit(reporter, EventError, "pull.secrets", err.Error(), nil)
		return nil, err
	}

	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		progress := &Progress{Phase: "secrets", Current: i + 1, Total: total}
		paths, writeErr := secrets.WriteSecureNotes(projectRoot, fetchedNotes[i])
		if writeErr != nil {
			return nil, fmt.Errorf("落地 %s 失败: %w", target.Label, writeErr)
		}
		summary.NoteCount += len(paths)
		summary.LandingPaths = append(summary.LandingPaths, paths...)
		if len(paths) > 0 {
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  落地 %d 个 Secure Note: %s", len(paths), strings.Join(paths, ", ")), progress)
		}

		if len(fetchedKeys[i]) > 0 {
			if writeErr := secrets.WriteSSHKeyLandings(fetchedKeys[i]); writeErr != nil {
				return nil, fmt.Errorf("落地 %s 的 SSH Key 失败: %w", target.Label, writeErr)
			}
			for _, landing := range fetchedKeys[i] {
				summary.SSHKeyCount++
				summary.SSHKeyNames = append(summary.SSHKeyNames, landing.Name)
			}
			names := make([]string, 0, len(fetchedKeys[i]))
			for _, landing := range fetchedKeys[i] {
				names = append(names, landing.Name)
			}
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  落地 %d 个 SSH Key: %s", len(names), strings.Join(names, ", ")), progress)
		}

		if len(paths) == 0 && len(fetchedKeys[i]) == 0 && target.Binding.SecretsBundleName != "" {
			emit(reporter, EventInfo, "pull.secrets",
				fmt.Sprintf("  Bitwarden folder %q 无 Secure Note / SSH Key 或 folder 不存在，跳过", target.Binding.SecretsBundleName), progress)
		}
	}

	warnUnignoredSecrets(projectRoot, summary.LandingPaths, reporter)

	if summary.NoteCount == 0 && summary.SSHKeyCount == 0 {
		emit(reporter, EventInfo, "pull.secrets", "secrets 同步完成（无变更）", &Progress{Phase: "secrets", Current: total, Total: total})
	} else {
		emit(reporter, EventInfo, "pull.secrets",
			fmt.Sprintf("secrets 同步完成：%d 个文件 · %d 个 SSH Key", summary.NoteCount, summary.SSHKeyCount),
			&Progress{Phase: "secrets", Current: total, Total: total})
	}
	return summary, nil
}

func loadSecretsConfigForPull() (*secrets.Config, error) {
	configured, err := secrets.IsConfigured()
	if err != nil {
		return nil, fmt.Errorf("读取 Bitwarden 配置失败: %w", err)
	}
	if !configured {
		return &secrets.Config{}, nil
	}
	cfg, err := secrets.LoadConfig()
	if err != nil {
		return nil, err
	}
	if cfg == nil {
		return &secrets.Config{}, nil
	}
	return cfg, nil
}
