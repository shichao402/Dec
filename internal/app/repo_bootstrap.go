package app

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
)

// RepoGCMCandidate 是可用于私仓 bootstrap 的 Bitwarden GCM Note 元数据。
// 只包含可展示字段；token/正文绝不离开 dec-server。
type RepoGCMCandidate struct {
	Folder    string
	NotePath  string
	Host      string
	Username  string
	Protocol  string
	Unmanaged bool // 非 bundle/* folder；Apply 允许，但正常 pull 不维护
}

type PrepareRepoGCMBootstrapResult struct {
	RepoURL    string
	RepoHost   string
	Candidates []RepoGCMCandidate
}

type ApplyRepoGCMBootstrapInput struct {
	RepoURL  string
	Folder   string
	NotePath string
}

type ApplyRepoGCMBootstrapResult struct {
	RepoURL   string
	RepoHost  string
	Candidate RepoGCMCandidate
}

var probeRepoForBootstrap = repo.Probe

// IsRepoAuthRequiredMessage 判断一次操作失败（含跨 RPC 回传的文本）是否为仓库凭证失效。
// 门面据此才提示走 GCM bootstrap，避免把 Bitwarden 401 之类的失败也导向 Git 凭证流程。
func IsRepoAuthRequiredMessage(message string) bool {
	return repo.MessageIndicatesAuthRequired(message)
}

// StripRepoAuthMarker 去掉错误文本里的机器标记，供门面展示。
func StripRepoAuthMarker(message string) string {
	return repo.StripAuthMarker(message)
}

// resolveBootstrapRepoURL 允许调用方省略 repoURL：Run 页 pull 失败时门面未必加载过 Settings，
// 此时以全局配置 / 已连接 bare repo 的 origin 为准，避免为了 bootstrap 先去补一次 Settings 加载。
func resolveBootstrapRepoURL(repoURL string) (string, error) {
	if trimmed := strings.TrimSpace(repoURL); trimmed != "" {
		return trimmed, nil
	}
	if globalConfig, err := config.LoadGlobalConfig(); err == nil && globalConfig != nil {
		if trimmed := strings.TrimSpace(globalConfig.RepoURL); trimmed != "" {
			return trimmed, nil
		}
	}
	connectedURL, err := repo.GetBareRemoteURL()
	if err != nil {
		return "", fmt.Errorf("无法确定仓库地址：请先到 Settings 页填写 Repo URL")
	}
	if trimmed := strings.TrimSpace(connectedURL); trimmed != "" {
		return trimmed, nil
	}
	return "", fmt.Errorf("无法确定仓库地址：请先到 Settings 页填写 Repo URL")
}

// PrepareRepoGCMBootstrap 复用 Bitwarden session、Note 读取与 GCM Processor，
// 但不依赖 Dec Git bundle manifest：这是为打破「拉私仓需要仓内 GCM」环依赖的特殊编排。
func PrepareRepoGCMBootstrap(ctx context.Context, repoURL string, reporter Reporter) (*PrepareRepoGCMBootstrapResult, error) {
	reporter = defaultReporter(reporter)
	repoURL, err := resolveBootstrapRepoURL(repoURL)
	if err != nil {
		return nil, err
	}
	host, err := repo.RepoHost(repoURL)
	if err != nil {
		return nil, err
	}
	if err := ensureBitwardenSession(ctx, reporter, "settings.repo.bootstrap"); err != nil {
		return nil, err
	}

	client := secretsClientFactory()
	folders, err := client.ListAllFolderNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("枚举 Bitwarden folder: %w", err)
	}
	sort.Strings(folders)

	result := &PrepareRepoGCMBootstrapResult{RepoURL: repoURL, RepoHost: host}
	for _, folder := range folders {
		notes, listErr := client.ListFolderNotes(ctx, folder)
		if listErr != nil {
			emit(reporter, EventWarn, "settings.repo.bootstrap",
				fmt.Sprintf("跳过 folder %q：%v", folder, listErr), nil)
			continue
		}
		for _, noteMeta := range notes {
			notePath := handler.NormalizeNotePath(noteMeta.Name)
			if !handler.MatchGCMPath(notePath) {
				continue
			}
			note, getErr := client.GetSecureNote(ctx, folder, noteMeta.Name)
			if getErr != nil {
				emit(reporter, EventWarn, "settings.repo.bootstrap",
					fmt.Sprintf("跳过 %s/%s：%v", folder, noteMeta.Name, getErr), nil)
				continue
			}
			identity, inspectErr := handler.InspectGCM(notePath, note.Content)
			if inspectErr != nil {
				emit(reporter, EventWarn, "settings.repo.bootstrap",
					fmt.Sprintf("跳过无效 GCM Note %s/%s：%v", folder, noteMeta.Name, inspectErr), nil)
				continue
			}
			if !strings.EqualFold(stripHostPort(identity.Host), host) {
				continue
			}
			unmanaged := !strings.HasPrefix(folder, secrets.BundleFolderPrefix)
			result.Candidates = append(result.Candidates, RepoGCMCandidate{
				Folder: folder, NotePath: notePath, Host: identity.Host,
				Username: identity.Username, Protocol: identity.Protocol, Unmanaged: unmanaged,
			})
			if unmanaged {
				emit(reporter, EventWarn, "settings.repo.bootstrap",
					fmt.Sprintf("%s/%s 不属于任何 bundle，pull 不维护；建议迁移到 bundle/<名>", folder, notePath), nil)
			}
		}
	}
	sort.Slice(result.Candidates, func(i, j int) bool {
		if result.Candidates[i].Folder != result.Candidates[j].Folder {
			return result.Candidates[i].Folder < result.Candidates[j].Folder
		}
		return result.Candidates[i].NotePath < result.Candidates[j].NotePath
	})
	emit(reporter, EventInfo, "settings.repo.bootstrap",
		fmt.Sprintf("找到 %d 个匹配 %s 的 GCM 候选", len(result.Candidates), host), nil)
	return result, nil
}

// ApplyRepoGCMBootstrap 重新读取选中的 Note、复核 host，调用现有 GCM Handler Apply，
// 然后探测远端。正文/token 仅存在于 dec-server 当前调用栈。
func ApplyRepoGCMBootstrap(ctx context.Context, input ApplyRepoGCMBootstrapInput, reporter Reporter) (*ApplyRepoGCMBootstrapResult, error) {
	reporter = defaultReporter(reporter)
	resolvedURL, err := resolveBootstrapRepoURL(input.RepoURL)
	if err != nil {
		return nil, err
	}
	input.RepoURL = resolvedURL
	input.Folder = strings.TrimSpace(input.Folder)
	input.NotePath = handler.NormalizeNotePath(input.NotePath)
	host, err := repo.RepoHost(input.RepoURL)
	if err != nil {
		return nil, err
	}
	if input.Folder == "" || !handler.MatchGCMPath(input.NotePath) {
		return nil, fmt.Errorf("无效的 GCM bootstrap 候选")
	}
	if err := ensureBitwardenSession(ctx, reporter, "settings.repo.bootstrap"); err != nil {
		return nil, err
	}

	note, err := secretsClientFactory().GetSecureNote(ctx, input.Folder, input.NotePath)
	if err != nil {
		return nil, err
	}
	identity, err := handler.InspectGCM(input.NotePath, note.Content)
	if err != nil {
		return nil, err
	}
	if !strings.EqualFold(stripHostPort(identity.Host), host) {
		return nil, fmt.Errorf("GCM Note host=%q 与仓库 host=%q 不匹配", identity.Host, host)
	}

	item := handler.Item{
		Source: handler.SourceNote, Name: input.NotePath, NoteContent: note.Content,
		BundleName: input.Folder,
	}
	h := handler.Default().Find(handler.SourceNote, input.NotePath)
	if h == nil || h.Kind() != "gcm" {
		return nil, fmt.Errorf("未注册 GCM Processor")
	}
	emit(reporter, EventInfo, "settings.repo.bootstrap",
		fmt.Sprintf("正在应用 %s/%s 到 Git Credential Manager", input.Folder, input.NotePath), nil)
	if err := h.Apply(ctx, item); err != nil {
		return nil, err
	}
	if err := probeRepoForBootstrap(input.RepoURL); err != nil {
		return nil, fmt.Errorf("GCM 已应用，但仓库仍不可访问: %w", err)
	}
	candidate := RepoGCMCandidate{
		Folder: input.Folder, NotePath: input.NotePath, Host: identity.Host,
		Username: identity.Username, Protocol: identity.Protocol,
		Unmanaged: !strings.HasPrefix(input.Folder, secrets.BundleFolderPrefix),
	}
	emit(reporter, EventInfo, "settings.repo.bootstrap", "GCM 已应用，仓库认证验证通过", nil)
	return &ApplyRepoGCMBootstrapResult{RepoURL: input.RepoURL, RepoHost: host, Candidate: candidate}, nil
}

func stripHostPort(host string) string {
	host = strings.TrimSpace(strings.ToLower(host))
	if i := strings.IndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}
