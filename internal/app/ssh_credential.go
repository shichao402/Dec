package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/shichao402/Dec/internal/secrets"
)

// ADR 0036：SSH 凭据生命周期 app 层操作。
//
// 三个操作都走"产品 folder + SyncTarget"的既有归属模型：
//   - GenerateSSHKey：本机生成 → 建 BW item → 落地 ~/.ssh → 返回公钥/指纹
//   - ImportSSHKey：从本机私钥文件导入（只收路径），建 BW item → 落地
//   - InstallSSHKey：密码引导安装——凭据请求通道拿登录密码 → 密码 ssh 登录
//     一次 → 追加标记公钥到 authorized_keys → 回写设备密码 note
//
// 私钥材料、口令、登录密码永不进 MCP 参数；敏感交互经 servicehost 的
// credentialBroker（阶段 B）在 Console 中完成。app 层通过 CredentialAsker
// 接口拿到"向用户要凭据"的能力，servicehost 注入 broker 实现，测试注入 stub。

// CredentialAsker 是凭据请求通道在 app 层的抽象。
// Ask 在 headless（无 GUI）或用户取消/超时时返回错误；秘密只在返回值内存流转。
type CredentialAsker interface {
	// AskLoginPassword 询问设备登录密码。prefilled=true 表示 GUI 层已有可
	// 自动填充的存量（如 BW 已存 .password 条目），仅剩确认。
	AskLoginPassword(ctx context.Context, prompt, operation string) (username, password string, err error)
	// AskKeyPassphrase 询问带口令私钥的口令。
	AskKeyPassphrase(ctx context.Context, prompt, operation string) (passphrase string, err error)
}

// passwordSSHAppend 是密码引导安装的执行点；包内变量便于测试注入。
var passwordSSHAppend = PasswordSSHAppendAuthorizedKey

// credentialAsker 是进程级注入点；nil 表示凭据请求通道不可用（headless）。
var credentialAsker CredentialAsker

// SetCredentialAsker 注入凭据请求通道实现（servicehost 启动时调用）。
func SetCredentialAsker(asker CredentialAsker) { credentialAsker = asker }

// GenerateSSHKeyInput dec_generate_sshkey 的输入。
type GenerateSSHKeyInput struct {
	Project string
	Name    string
	Comment string
}

// GenerateSSHKeyResult 只回可安全展示的字段，绝不回私钥。
type GenerateSSHKeyResult struct {
	KeyName     string `json:"key_name"`
	TargetName  string `json:"target_name"`
	Address     string `json:"address"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
	LandingPath string `json:"landing_path,omitempty"`
}

func GenerateSSHKey(ctx context.Context, in GenerateSSHKeyInput, reporter Reporter) (*GenerateSSHKeyResult, error) {
	reporter = defaultReporter(reporter)
	project := strings.TrimSpace(in.Project)
	if project == "" {
		return nil, fmt.Errorf("project 不能为空")
	}
	name, err := sshKeyName(in.Name)
	if err != nil {
		return nil, err
	}
	target, err := secrets.NewPSyncTarget(project, secrets.SyncPlaneGlobal)
	if err != nil {
		return nil, err
	}
	if err := ensureBitwardenSession(ctx, reporter, "generate_sshkey"); err != nil {
		return nil, err
	}
	comment := strings.TrimSpace(in.Comment)
	if comment == "" {
		comment = name
	}
	mat, err := secrets.GenerateSSHKeyMaterial(comment)
	if err != nil {
		return nil, err
	}
	client := secretsClientFactory()
	key := secrets.SSHKeyItem{
		Name:           name,
		PrivateKey:     mat.PrivateKey,
		PublicKey:      mat.PublicKey,
		KeyFingerprint: mat.KeyFingerprint,
	}
	if err := client.CreateSSHKey(ctx, secrets.CreateSSHKeyRequest{Target: target, Key: key}); err != nil {
		return nil, err
	}
	landing, landErr := landDeviceSSHKey(target, key)
	msg := fmt.Sprintf("已生成并登记 SSH Key %s → %s", name, target.Address)
	if landing != "" {
		msg += "；已写入 ~/.ssh"
	} else if landErr != nil {
		msg += "；本机落地跳过: " + landErr.Error()
	}
	emit(reporter, EventInfo, "generate_sshkey", msg, nil)
	return &GenerateSSHKeyResult{
		KeyName:     name,
		TargetName:  target.Name,
		Address:     target.Address,
		PublicKey:   mat.PublicKey,
		Fingerprint: mat.KeyFingerprint,
		LandingPath: landing,
	}, nil
}

// ImportSSHKeyInput dec_import_sshkey 的输入。
type ImportSSHKeyInput struct {
	Project        string
	Name           string
	PrivateKeyPath string
}

// ImportSSHKeyResult 只回可安全展示的字段。
type ImportSSHKeyResult struct {
	KeyName     string `json:"key_name"`
	TargetName  string `json:"target_name"`
	Address     string `json:"address"`
	PublicKey   string `json:"public_key"`
	Fingerprint string `json:"fingerprint"`
	LandingPath string `json:"landing_path,omitempty"`
}

func ImportSSHKey(ctx context.Context, in ImportSSHKeyInput, reporter Reporter) (*ImportSSHKeyResult, error) {
	reporter = defaultReporter(reporter)
	project := strings.TrimSpace(in.Project)
	if project == "" {
		return nil, fmt.Errorf("project 不能为空")
	}
	name, err := sshKeyName(in.Name)
	if err != nil {
		return nil, err
	}
	privPath := strings.TrimSpace(in.PrivateKeyPath)
	if privPath == "" {
		return nil, fmt.Errorf("private_key_path 不能为空")
	}
	target, err := secrets.NewPSyncTarget(project, secrets.SyncPlaneGlobal)
	if err != nil {
		return nil, err
	}
	if err := ensureBitwardenSession(ctx, reporter, "import_sshkey"); err != nil {
		return nil, err
	}
	mat, err := loadSSHKeyMaterialInteractive(ctx, privPath, reporter)
	if err != nil {
		return nil, err
	}
	client := secretsClientFactory()
	key := secrets.SSHKeyItem{
		Name:           name,
		PrivateKey:     mat.PrivateKey,
		PublicKey:      mat.PublicKey,
		KeyFingerprint: mat.KeyFingerprint,
	}
	if err := client.CreateSSHKey(ctx, secrets.CreateSSHKeyRequest{Target: target, Key: key}); err != nil {
		if strings.Contains(err.Error(), "已存在") {
			return nil, fmt.Errorf("%w；如需覆盖请先删除该条目", err)
		}
		return nil, err
	}
	landing, landErr := landDeviceSSHKey(target, key)
	msg := fmt.Sprintf("已导入 SSH Key %s → %s", name, target.Address)
	if landing != "" {
		msg += "；已写入 ~/.ssh"
	} else if landErr != nil {
		msg += "；本机落地跳过: " + landErr.Error()
	}
	emit(reporter, EventInfo, "import_sshkey", msg, nil)
	return &ImportSSHKeyResult{
		KeyName:     name,
		TargetName:  target.Name,
		Address:     target.Address,
		PublicKey:   mat.PublicKey,
		Fingerprint: mat.KeyFingerprint,
		LandingPath: landing,
	}, nil
}

// InstallSSHKeyInput dec_install_ssh_key 的输入。
type InstallSSHKeyInput struct {
	Project   string
	Alias     string
	Name      string // 复用已有条目时填；留空自动生成新钥
	SSHTarget string
	Confirmed bool
}

// InstallSSHKeyResult 只回可安全展示的字段。
type InstallSSHKeyResult struct {
	KeyName        string `json:"key_name"`
	Address        string `json:"address"`
	PublicKey      string `json:"public_key"`
	Marker         string `json:"marker"`
	Username       string `json:"username"`
	SSHHost        string `json:"ssh_host"`
	PasswordStored bool   `json:"password_stored"`
}

func InstallSSHKey(ctx context.Context, in InstallSSHKeyInput, reporter Reporter) (*InstallSSHKeyResult, error) {
	reporter = defaultReporter(reporter)
	project := strings.TrimSpace(in.Project)
	if project == "" {
		return nil, fmt.Errorf("project 不能为空")
	}
	alias := strings.TrimSpace(in.Alias)
	if alias == "" {
		return nil, fmt.Errorf("alias 不能为空")
	}
	sshTarget := strings.TrimSpace(in.SSHTarget)
	if sshTarget == "" {
		return nil, fmt.Errorf("ssh_target 不能为空")
	}
	if !in.Confirmed {
		return nil, fmt.Errorf("confirmed 必须为 true：首次写远端 authorized_keys 属远程变更")
	}
	target, err := secrets.NewPSyncTarget(project, secrets.SyncPlaneGlobal)
	if err != nil {
		return nil, err
	}
	if err := ensureBitwardenSession(ctx, reporter, "install_ssh_key"); err != nil {
		return nil, err
	}
	client := secretsClientFactory()

	// 1. 取密钥材料：复用已有 .sshkey 条目，或自动生成新钥（name 留空用 alias）。
	rawName := strings.TrimSpace(in.Name)
	if rawName == "" {
		rawName = alias
	}
	name, err := sshKeyName(rawName)
	if err != nil {
		return nil, err
	}
	mat, found, err := readOrGenerateSSHKey(ctx, client, target, name, alias)
	if err != nil {
		return nil, err
	}

	// 2. 登录密码：BW 已存 .password/<alias> 自动填充；否则经凭据请求通道。
	username, password, stored, err := resolveDevicePassword(ctx, client, target, alias, sshTarget, reporter)
	if err != nil {
		return nil, err
	}

	// 3. 密码登录 + 追加标记公钥（# dec:<alias>）。
	marker := fmt.Sprintf("# dec:%s", alias)
	markerLine := strings.TrimSpace(mat.PublicKey) + " " + marker
	if err := passwordSSHAppend(ctx, sshTarget, username, password, markerLine); err != nil {
		return nil, err
	}

	// 4. 登录成功后回写设备密码（下次自动填充）。
	passwordStored := stored
	if !stored {
		if err := writeDevicePasswordNote(ctx, client, target, alias, username, password); err == nil {
			passwordStored = true
		} else {
			emit(reporter, EventWarn, "install_ssh_key",
				fmt.Sprintf("密码回写 %s/.password/%s 失败: %v", target.Address, alias, err), nil)
		}
	}

	generated := !found
	action := "复用"
	if generated {
		action = "生成"
	}
	emit(reporter, EventInfo, "install_ssh_key",
		fmt.Sprintf("已在 %s 追加标记公钥（%s条目 %s，marker %s）；后续置备用 dec_provision_remote",
			sshTarget, action, name, marker), nil)
	return &InstallSSHKeyResult{
		KeyName:        name,
		Address:        target.Address,
		PublicKey:      mat.PublicKey,
		Marker:         marker,
		Username:       username,
		SSHHost:        sshTarget,
		PasswordStored: passwordStored,
	}, nil
}

// readOrGenerateSSHKey 复用已有条目（返回 found=true）或按 alias 生成新钥并登记。
func readOrGenerateSSHKey(ctx context.Context, client secrets.Client, target secrets.SyncTarget, name, alias string) (secrets.SSHKeyMaterial, bool, error) {
	bundle, err := client.PullBundle(ctx, secrets.PullBundleRequest{Target: target})
	if err != nil {
		return secrets.SSHKeyMaterial{}, false, err
	}
	for _, k := range bundle.SSHKeys {
		if k.Name == name {
			return secrets.SSHKeyMaterial{
				PrivateKey:     k.PrivateKey,
				PublicKey:      k.PublicKey,
				KeyFingerprint: k.KeyFingerprint,
			}, true, nil
		}
	}
	mat, err := secrets.GenerateSSHKeyMaterial(alias)
	if err != nil {
		return secrets.SSHKeyMaterial{}, false, err
	}
	key := secrets.SSHKeyItem{
		Name:           name,
		PrivateKey:     mat.PrivateKey,
		PublicKey:      mat.PublicKey,
		KeyFingerprint: mat.KeyFingerprint,
	}
	if err := client.CreateSSHKey(ctx, secrets.CreateSSHKeyRequest{Target: target, Key: key}); err != nil {
		return secrets.SSHKeyMaterial{}, false, err
	}
	return mat, false, nil
}

// sshKeyName 规范化 .sshkey/<name> 逻辑名。
func sshKeyName(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("name 不能为空")
	}
	proc, ok := secrets.LookupProcessor(string(secrets.SecretTypeSSHKey))
	if !ok {
		return "", fmt.Errorf("sshkey processor 未注册")
	}
	return proc.NormalizeName(raw)
}

// landDeviceSSHKey 把新 key 落到 ~/.ssh（机器平面语义），已存在同名文件则跳过。
func landDeviceSSHKey(target secrets.SyncTarget, key secrets.SSHKeyItem) (string, error) {
	exists, err := secrets.LocalSSHKeyExists(target.Name, key.Name)
	if err != nil {
		return "", err
	}
	if exists {
		return "", fmt.Errorf("本地已有同名密钥文件，未覆盖；可稍后 Pull")
	}
	landings, err := secrets.PrepareSSHKeyLandings(target.Name, []secrets.SSHKeyItem{key})
	if err != nil {
		return "", err
	}
	if err := secrets.WriteSSHKeyLandings(landings); err != nil {
		return "", err
	}
	if len(landings) > 0 {
		return landings[0].PrivatePath, nil
	}
	return "", nil
}

// resolveDevicePassword 解析登录密码：BW 已存 .password/<alias> 则自动填充；
// 否则经凭据请求通道向用户要。sshTarget 用于弹窗文案指明目标主机。
func resolveDevicePassword(ctx context.Context, client secrets.Client, target secrets.SyncTarget, alias, sshTarget string, reporter Reporter) (username, password string, stored bool, err error) {
	notePath := ".password/" + alias
	if note, gerr := client.GetNote(ctx, target, notePath); gerr == nil && note != nil {
		if u, p, perr := parseDevicePasswordNote(note.Content); perr == nil && strings.TrimSpace(p) != "" {
			emit(reporter, EventInfo, "install_ssh_key",
				fmt.Sprintf("已从 %s 自动填充设备密码", notePath), nil)
			return u, p, true, nil
		}
	}
	if credentialAsker == nil {
		return "", "", false, fmt.Errorf("凭据请求通道不可用（headless）：请先在 BW 手工登记 %s 或在桌面环境重试", notePath)
	}
	u, p, aerr := credentialAsker.AskLoginPassword(ctx,
		fmt.Sprintf("AI 正在给 SSH 主机 %s 安装 Dec 登录密钥（工具 dec_install_ssh_key，设备别名 %s）。需要这台机器上 %s 账号的登录密码，只用这一次，用于密码登录并追加公钥，之后 SSH 免密。密码会加密保存到密码库 %s/.password/%s，下次自动使用。5 分钟内不提交或点取消，本次安装将失败。",
			sshTarget, alias, defaultSSHUser, target.Address, alias),
		"dec_install_ssh_key")
	if aerr != nil {
		return "", "", false, aerr
	}
	username = strings.TrimSpace(u)
	if username == "" {
		username = defaultSSHUser
	}
	return username, p, false, nil
}

// defaultSSHUser 是凭据弹窗未提供用户名时的默认 SSH 账号（prompt 文案与
// 实际登录账号必须一致，故抽为常量）。
const defaultSSHUser = "root"

// writeDevicePasswordNote 把设备密码写回 BW .password/<alias>。
func writeDevicePasswordNote(ctx context.Context, client secrets.Client, target secrets.SyncTarget, alias, username, password string) error {
	body := fmt.Sprintf("username: %s\npassword: %s\n", username, password)
	_, err := client.PushBundle(ctx, secrets.PushBundleRequest{Target: target}, []secrets.SecureNote{
		{RelativePath: ".password/" + alias, Content: body},
	})
	return err
}

// parseDevicePasswordNote 解析 .password 条目正文（两行 username: / password:）。
func parseDevicePasswordNote(content string) (username, password string, err error) {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "username:"):
			username = strings.TrimSpace(strings.TrimPrefix(line, "username:"))
		case strings.HasPrefix(line, "password:"):
			password = strings.TrimSpace(strings.TrimPrefix(line, "password:"))
		}
	}
	if username == "" || password == "" {
		return "", "", fmt.Errorf(".password 正文缺少 username 或 password 行")
	}
	return username, password, nil
}

// loadSSHKeyMaterialInteractive 从私钥文件加载材料；带口令私钥经凭据请求通道
// 采集口令后去口令入库（ADR 0036 §5）。
func loadSSHKeyMaterialInteractive(ctx context.Context, privPath string, reporter Reporter) (secrets.SSHKeyMaterial, error) {
	mat, err := secrets.LoadSSHKeyMaterialFromPrivatePath(privPath)
	if err == nil {
		return mat, nil
	}
	// 无口令加载失败且错误来自 ssh-keygen 派生段 → 疑似带口令私钥。
	if !strings.Contains(err.Error(), "派生公钥失败") {
		return secrets.SSHKeyMaterial{}, err
	}
	if credentialAsker == nil {
		return secrets.SSHKeyMaterial{}, fmt.Errorf("私钥带口令但凭据请求通道不可用（headless）：%w", err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		passphrase, aerr := credentialAsker.AskKeyPassphrase(ctx,
			fmt.Sprintf("AI 正在把私钥文件 %s 导入 Dec 密码库（工具 dec_import_sshkey）。这把私钥设置了口令，需要你输入口令解开它。口令只在内存里用于校验，校验通过后私钥会去掉口令入库，由密码库加密统一保护，口令本身不会被保存。输错可再试一次；5 分钟内不提交或点取消，本次导入将失败。",
				privPath),
			"dec_import_sshkey")
		if aerr != nil {
			return secrets.SSHKeyMaterial{}, aerr
		}
		mat, err = secrets.LoadSSHKeyMaterialWithPassphrase(privPath, passphrase)
		if err == nil {
			emit(reporter, EventInfo, "import_sshkey",
				"带口令私钥已校验；去口令入库（库加密为唯一保护）", nil)
			return mat, nil
		}
	}
	return secrets.SSHKeyMaterial{}, fmt.Errorf("私钥口令校验失败（两次尝试均未通过）")
}
