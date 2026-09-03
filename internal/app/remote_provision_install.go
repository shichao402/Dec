package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/shichao402/Dec/internal/types"
)

// remoteInstallTimeout 覆盖发起端 RUP 下载和 SSH 推送四个产物的最坏耗时。
const remoteInstallTimeout = 12 * time.Minute

// ProvisionRemoteHostInput 是注入安装的入参。
type ProvisionRemoteHostInput struct {
	Target RemoteTarget

	// Alias 是受管设备清单中的稳定别名；留空时使用 SSH 目标。
	Alias string
	// Tags 供 Console / MCP 对设备分类，不参与连接与置备。
	Tags []string

	// Confirm 承载显式确认：Console 传目标主机名，MCP 传 "PROVISION"。
	// 首次置备（目标机尚未装过 Dec）必填，见 ADR 0019 安全边界。
	Confirm string
	// Confirmed 供 MCP 沿用 dec_delete 的 confirmed=true 惯例。
	Confirmed bool

	// Branch 为兼容既有 dec_provision_remote 输入保留；SSH 推送路径不使用它。
	Branch string
	// Version 钉死由 Console 管理的运行时版本（vMAJOR.MINOR.PATCH）。
	// 留空时保持人工安装语义：安装所选分支的最新版本。
	Version string

	// SkipConfigure 跳过写入固定监听端口这一步。
	//
	// 默认不跳过：装完二进制但没配端口的机器仍然连不上，置备只做一半没有意义。
	SkipConfigure bool
}

// ProvisionTypedConfirmSpec 描述置备前必须的显式确认要求。
type ProvisionTypedConfirmSpec struct {
	Required bool
	// Expect 用户必须输入的短语：目标主机名，输入 "PROVISION" 亦可。
	Expect string
	Reason string
}

// ProvisionRemoteHostResult 是一次注入安装的结果。
type ProvisionRemoteHostResult struct {
	Target string

	// Probe 是安装前的探测结论，附在结果里便于回溯当时的机器状态。
	Probe *RemoteHostProbe
	// Verify 是安装后的复探结论。
	Verify *RemoteHostProbe

	// Skipped 表示已是最新且四件套完整，脚本自行退出，未做任何改动。
	Skipped bool
	// InstalledVersion 是安装后目标机上的版本。
	InstalledVersion string

	// Setup 是固定监听端口的配置结果；SkipConfigure 时为 nil。
	Setup *RemoteServiceSetupResult
	// Device 是置备成功后在本机 GlobalConfig 中登记的设备。
	Device *types.ManagedDevice

	// ChecksumVerified 表示发起端已校验 bundle/RUP 产物摘要。
	ChecksumVerified bool
	// Warnings 汇总不阻断但需知晓的后果（含摘要缺失降级）。
	Warnings []string

	// NextAction 是给 Console 的后续动作建议。
	NextAction string
}

// AnalyzeProvisionTypedConfirm 判断本次置备是否需要显式确认。
//
// 首次置备（目标机尚未装过 Dec）必须确认：SSH 推送会写入远端可执行文件。
// 已装过 Dec 的机器视为已建立信任关系，升级不再反复确认。
func AnalyzeProvisionTypedConfirm(target RemoteTarget, probe *RemoteHostProbe) ProvisionTypedConfirmSpec {
	if probe != nil && probe.DecInstalled {
		return ProvisionTypedConfirmSpec{}
	}
	return ProvisionTypedConfirmSpec{
		Required: true,
		Expect:   target.DisplayName(),
		Reason:   "首次置备将向该主机推送并安装 Dec 运行时（远程写入）",
	}
}

// MatchProvisionTypedConfirm 校验确认输入是否满足要求。
func MatchProvisionTypedConfirm(input string, confirmed bool, spec ProvisionTypedConfirmSpec) bool {
	if !spec.Required {
		return true
	}
	if confirmed {
		return true
	}
	got := strings.TrimSpace(input)
	if got == "" {
		return false
	}
	if got == "PROVISION" {
		return true
	}
	return got == strings.TrimSpace(spec.Expect)
}

// ProvisionRemoteHost 从发起端缓存经 SSH 推送四件套并完成置备。
//
// 四段流程：探测 → 解析并推送运行时 → 写入固定监听端口 → 复探验证。
// 「写配置」仍由远端自己的 `dec __service-setup` 负责
// （ADR 0019：避免在 Go 侧手写远端 YAML 而绕过 config 包的合并逻辑）。
//
// 置备**不**在远端安装常驻服务：远端与本机生命周期一致，空闲即退出，
// 连接时由连接方经 SSH 按需拉起（见 EnsureRemoteServiceRunning）。
func ProvisionRemoteHost(ctx context.Context, in ProvisionRemoteHostInput, reporter Reporter) (*ProvisionRemoteHostResult, error) {
	reporter = defaultReporter(reporter)
	if err := in.Target.validate(); err != nil {
		return nil, err
	}
	if ok, reason := LocalPlatformSupportsProvisioning(); !ok {
		return nil, fmt.Errorf("%s", reason)
	}

	totalPhases := 4
	if in.SkipConfigure {
		totalPhases = 3
	}
	result := &ProvisionRemoteHostResult{Target: in.Target.DisplayName()}

	emit(reporter, EventInfo, "provision.install", "置备 "+result.Target, &Progress{
		Phase: "probe", Current: 1, Total: totalPhases,
	})
	probe, err := ProbeRemoteHost(ctx, in.Target, reporter)
	if err != nil {
		return nil, err
	}
	result.Probe = probe
	if !probe.Reachable {
		return nil, fmt.Errorf("目标机不可达: %s", probe.SSHError)
	}
	if len(probe.Blockers) > 0 {
		return nil, fmt.Errorf("目标机不满足置备条件: %s", strings.Join(probe.Blockers, "；"))
	}

	spec := AnalyzeProvisionTypedConfirm(in.Target, probe)
	if !MatchProvisionTypedConfirm(in.Confirm, in.Confirmed, spec) {
		return nil, fmt.Errorf("%s；请键入主机名 %q 以确认", spec.Reason, spec.Expect)
	}
	releaseVersion := normalizeReleaseVersion(in.Version)
	if !validReleaseVersion(releaseVersion) {
		return nil, fmt.Errorf("置备必须指定有效的 Console 运行时版本，实际 %q", in.Version)
	}
	result.Warnings = append(result.Warnings, probe.Warnings...)

	emit(reporter, EventInfo, "provision.install", "准备并推送目标运行时", &Progress{
		Phase: "install", Current: 2, Total: totalPhases,
	})
	if probe.DecInstalled && normalizeReleaseVersion(probe.DecVersion) == releaseVersion {
		result.Skipped = true
		emit(reporter, EventInfo, "provision.install", "目标四件套已是 "+releaseVersion, nil)
	} else {
		if probe.ServerRunning {
			emit(reporter, EventInfo, "provision.install", "停止远端旧服务以原子替换运行时", nil)
			if err := stopRemoteServiceForUpgrade(ctx, in.Target); err != nil {
				return nil, err
			}
		}
		installCtx, cancel := context.WithTimeout(ctx, remoteInstallTimeout)
		defer cancel()
		suite, err := ResolveRuntimeSuite(installCtx, releaseVersion, probe.OS, probe.Arch, reporter)
		if err != nil {
			return nil, err
		}
		if err := PushRuntimeSuite(installCtx, in.Target, suite, reporter); err != nil {
			return nil, err
		}
		result.ChecksumVerified = true
	}

	verifyPhase := 3
	if !in.SkipConfigure {
		verifyPhase = 4
		emit(reporter, EventInfo, "provision.install", "配置固定监听端口", &Progress{
			Phase: "configure", Current: 3, Total: totalPhases,
		})
		setup, err := ConfigureRemoteService(ctx, in.Target, reporter)
		if err != nil {
			return nil, err
		}
		result.Setup = setup
	}

	emit(reporter, EventInfo, "provision.install", "复探目标机", &Progress{
		Phase: "verify", Current: verifyPhase, Total: totalPhases,
	})
	verify, err := ProbeRemoteHost(ctx, in.Target, reporter)
	if err != nil {
		return nil, err
	}
	result.Verify = verify
	if !verify.DecInstalled {
		return nil, fmt.Errorf("安装后校验失败：目标机仍缺少 %s", strings.Join(verify.MissingBinaries, "、"))
	}
	result.InstalledVersion = verify.DecVersion

	switch {
	case verify.ListenReady:
		result.NextAction = "已完成置备并登记设备，可直接连接（连接时会按需拉起远端服务）"
	case in.SkipConfigure:
		result.NextAction = "程序组已就位；本次跳过了监听端口配置，连接前需先执行配置"
	default:
		// 配置步骤已确认写入却复探不到，多半是远端存在第二份配置或 DEC_HOME 不一致。
		return nil, fmt.Errorf("配置已写入但复探到的 management_listen 为 %q，期望 %q；请检查远端 DEC_HOME 是否一致",
			verify.ManagementListen, RemoteProvisionListen)
	}
	if verify.ListenReady {
		device, err := RegisterManagedDevice(ManagedDeviceInput{
			Alias:              in.Alias,
			Target:             in.Target,
			Tags:               in.Tags,
			ProvisionedVersion: result.InstalledVersion,
		})
		if err != nil {
			return nil, fmt.Errorf("置备已完成，但登记设备失败: %w", err)
		}
		result.Device = &device
		emit(reporter, EventInfo, "provision.register", "已登记设备 "+device.Alias, nil)
	}
	emit(reporter, EventInfo, "provision.install",
		fmt.Sprintf("置备完成：%s %s", result.Target, result.InstalledVersion), nil)
	return result, nil
}

func normalizeScriptNewlines(script string) string {
	script = strings.ReplaceAll(script, "\r\n", "\n")
	return strings.ReplaceAll(script, "\r", "\n")
}

func validReleaseVersion(value string) bool {
	value = strings.TrimPrefix(strings.TrimSpace(value), "v")
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// validBranchName 限制可注入远端命令行的分支名字符集，防止命令注入。
func validBranchName(name string) bool {
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '_', r == '.', r == '/':
		default:
			return false
		}
	}
	return true
}

// forwardRemoteOutput 把远端输出按行转发为进度事件，去掉 ANSI 颜色与装饰行。
func forwardRemoteOutput(reporter Reporter, scope, out string) {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(stripANSI(line))
		if line == "" || isBoxDrawing(line) {
			continue
		}
		level := EventInfo
		if strings.HasPrefix(line, "⚠") {
			level = EventWarn
		}
		if strings.HasPrefix(line, "✗") {
			level = EventError
		}
		emit(reporter, level, scope, line, nil)
	}
}

func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			for i < len(s) && s[i] != 'm' {
				i++
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isBoxDrawing(line string) bool {
	for _, r := range line {
		if r != '╔' && r != '╗' && r != '╚' && r != '╝' && r != '═' && r != '║' &&
			r != ' ' && r != 'D' && r != 'e' && r != 'c' {
			return false
		}
	}
	return strings.ContainsAny(line, "╔╗╚╝═║")
}
