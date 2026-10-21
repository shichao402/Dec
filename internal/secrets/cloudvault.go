package secrets

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// 这个文件为 Dec Cloud 提供无状态的 Bitwarden 短生命周期会话。
//
// 与进程内单例会话（authenticate.go / session.go）的区别：
//
//   - 不写任何进程全局状态，不碰 device.json 里的 remember token；
//   - 一次会话的生命周期由调用方显式结束（Close）；
//   - vault key 存在实例上，用完清零。
//
// 云端只在三个时刻需要它：初始化写根密钥冷备、设备登记核对身份、恢复时取回根密钥。

// ErrNeed2FA 表示还需要二次验证码。
var ErrNeed2FA = errors.New("need 2FA")

// CloudVault 是一次 Bitwarden 会话。
type CloudVault struct {
	cfg      *Config
	identity *IdentityClient
	api      *APIClient
	userKey  []byte
	email    string

	// pending 保存登录阶段需要 2FA 时的中间状态。
	// 密码只在需要 2FA 期间短暂保留，验证码提交后立刻清除。
	pending      *loginAttempt
	pendingPass  string
	pendingEmail string
	// plainPassword 仅在无 2FA 的登录路径上短暂保存在内存里，解出 vault key 后立即清空。
	plainPassword string
}

// NewCloudVault 创建会话，尚未登录。
//
// serverURL 为空时使用官方默认地址。
func NewCloudVault(serverURL string, httpClient *http.Client) (*CloudVault, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		serverURL = "https://vault.bitwarden.com"
	}
	cfg := &Config{ServerURL: serverURL}
	identityURL, _, err := cfg.Endpoints()
	if err != nil {
		return nil, fmt.Errorf("解析 Bitwarden 端点失败: %w", err)
	}
	deviceID, err := EnsureDeviceIdentifier()
	if err != nil {
		// 云端没有可写的用户目录时，退化为进程内随机标识。
		// Bitwarden 只把它当作设备指纹，不参与密钥派生，随机值同样可用。
		deviceID = newDeviceID()
	}
	return &CloudVault{
		cfg: cfg,
		identity: &IdentityClient{
			IdentityURL: identityURL,
			DeviceID:    deviceID,
			HTTP:        httpClient,
		},
	}, nil
}

// Unlock 完成登录。需要二次验证时返回 ErrNeed2FA，调用方应在拿到验证码后再调一次。
//
// 每次调用都会重新走一遍登录，不复用 remember token：
// 云端是长期在线的服务，把「记住设备」写进服务器磁盘等于把 2FA 永久关掉。
func (v *CloudVault) Unlock(ctx context.Context, email, password, totp string) error {
	if v == nil {
		return fmt.Errorf("会话为空")
	}
	email = strings.TrimSpace(email)
	password = strings.TrimSpace(password)
	totp = strings.TrimSpace(totp)

	// 第二次调用（带验证码）走这里。
	if v.pending != nil {
		if totp == "" {
			return ErrNeed2FA
		}
		provider := v.pending.twoFactorProvider
		if provider == "" {
			provider = twoFactorProviderAuthenticator
		}
		attempt, err := v.identity.Login(ctx, v.pendingPass, totp, v.pending.twoFactorSession, provider, LoginOptions{})
		if err != nil {
			return fmt.Errorf("二次验证失败: %w", err)
		}
		if attempt.need2FA {
			return fmt.Errorf("验证码不正确")
		}
		return v.finish(ctx, attempt.accessToken)
	}

	if email == "" {
		return fmt.Errorf("Bitwarden 邮箱不能为空")
	}
	if password == "" {
		return fmt.Errorf("Bitwarden 主密码不能为空")
	}
	v.identity.Email = email
	v.email = email

	attempt, err := v.identity.Login(ctx, password, totp, "", "", LoginOptions{})
	if err != nil {
		return fmt.Errorf("Bitwarden 登录失败: %w", err)
	}
	if attempt.need2FA {
		if totp == "" {
			v.pending = attempt
			v.pendingPass = password
			v.pendingEmail = email
			return ErrNeed2FA
		}
		return fmt.Errorf("验证码不正确")
	}
	v.plainPassword = password
	return v.finish(ctx, attempt.accessToken)
}

// finish 用 access token 解出 vault key，并建立 API 客户端。
func (v *CloudVault) finish(ctx context.Context, accessToken string) error {
	password := v.pendingPass
	if password == "" {
		// 无 2FA 路径下密码没有暂存在 pendingPass，需要由调用方传入。
		// 为避免额外状态，这里使用 identity 上保存的密码副本。
		password = v.plainPassword
	}
	if err := unlockVaultKeyInto(ctx, v.cfg, v.identity, password, accessToken, v); err != nil {
		return err
	}
	api, err := NewAPIClient(v.cfg, accessToken, v.identity.HTTP)
	if err != nil {
		return err
	}
	api.SetVaultKey(v.userKey)
	v.api = api
	v.pending = nil
	v.pendingPass = ""
	v.plainPassword = ""
	return nil
}

// unlockVaultKeyInto 解出 vault key 并写进会话，而不是写进程全局。
func unlockVaultKeyInto(ctx context.Context, cfg *Config, identity *IdentityClient, password, accessToken string, target *CloudVault) error {
	if cfg == nil || identity == nil {
		return fmt.Errorf("Bitwarden 配置不完整")
	}
	pre, err := identity.Prelogin(ctx)
	if err != nil {
		return err
	}
	encryptedKey, err := fetchProfileKey(ctx, cfg, accessToken, identity.httpClient())
	if err != nil {
		return err
	}
	key, err := decryptUserKey(encryptedKey, password, preloginSalt(pre, identity.Email), pre.KdfIterations)
	if err != nil {
		return err
	}
	target.userKey = key
	return nil
}

// Close 结束会话并清零密钥。
func (v *CloudVault) Close() {
	if v == nil {
		return
	}
	if v.api != nil {
		v.api.ClearVaultKey()
		v.api = nil
	}
	for i := range v.userKey {
		v.userKey[i] = 0
	}
	v.userKey = nil
	v.pending = nil
	v.pendingPass = ""
	v.plainPassword = ""
	v.identity = nil
}

// ready 检查会话是否已解锁。
func (v *CloudVault) ready() error {
	if v == nil || v.api == nil || len(v.userKey) == 0 {
		return fmt.Errorf("Bitwarden 会话未解锁")
	}
	return nil
}

// EnsureFolder 返回 folder 的 ID，不存在则创建。
func (v *CloudVault) EnsureFolder(ctx context.Context, name string) (string, error) {
	if err := v.ready(); err != nil {
		return "", err
	}
	id, err := v.api.findFolderID(ctx, name, v.userKey)
	if err != nil {
		return "", err
	}
	if id != "" {
		return id, nil
	}
	return v.api.createFolder(ctx, name, v.userKey)
}

// FindFolder 只查找不创建。返回空 ID 表示不存在。
func (v *CloudVault) FindFolder(ctx context.Context, name string) (string, error) {
	if err := v.ready(); err != nil {
		return "", err
	}
	return v.api.findFolderID(ctx, name, v.userKey)
}

// ListNotes 返回 folder 下全部 Secure Note，按条目名索引。
//
// 名字是明文（解密后），内容是 note 正文（同样已解密）。
func (v *CloudVault) ListNotes(ctx context.Context, folderName string) (map[string]string, error) {
	if err := v.ready(); err != nil {
		return nil, err
	}
	folderID, err := v.api.findFolderID(ctx, folderName, v.userKey)
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		return map[string]string{}, nil
	}
	ciphers, err := v.api.listCiphers(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, cipher := range ciphers {
		if cipher.Type != cipherTypeSecureNote || cipher.FolderID != folderID {
			continue
		}
		itemKey, err := itemDecryptionKey(cipher.Key, v.userKey)
		if err != nil {
			continue
		}
		name, err := decryptVaultString(cipher.Name, itemKey)
		if err != nil {
			continue
		}
		content, err := decryptVaultString(cipher.Notes, itemKey)
		if err != nil {
			continue
		}
		out[strings.TrimSpace(name)] = content
	}
	return out, nil
}

// GetNote 读取一条 note，不存在时返回 ok=false。
func (v *CloudVault) GetNote(ctx context.Context, folderName, noteName string) (string, bool, error) {
	notes, err := v.ListNotes(ctx, folderName)
	if err != nil {
		return "", false, err
	}
	content, ok := notes[strings.TrimSpace(noteName)]
	return content, ok, nil
}

// UpsertNote 写入一条 note。已存在且内容相同则不做事，返回 changed=false。
//
// 冷备的语义是只追加（需求 15.2）：根密钥条目按 key id 命名，轮换时新建条目，
// 从不覆盖旧条目，所以调用方没有删除旧值的机会。
func (v *CloudVault) UpsertNote(ctx context.Context, folderName, noteName, content string) (bool, error) {
	if err := v.ready(); err != nil {
		return false, err
	}
	folderID, err := v.EnsureFolder(ctx, folderName)
	if err != nil {
		return false, err
	}
	ciphers, err := v.api.listCiphers(ctx)
	if err != nil {
		return false, err
	}
	for _, cipher := range ciphers {
		if cipher.Type != cipherTypeSecureNote || cipher.FolderID != folderID {
			continue
		}
		itemKey, err := itemDecryptionKey(cipher.Key, v.userKey)
		if err != nil {
			continue
		}
		name, err := decryptVaultString(cipher.Name, itemKey)
		if err != nil {
			continue
		}
		if strings.TrimSpace(name) != strings.TrimSpace(noteName) {
			continue
		}
		existing, err := decryptVaultString(cipher.Notes, itemKey)
		if err != nil {
			continue
		}
		if existing == content {
			return false, nil
		}
		if err := v.api.updateSecureNote(ctx, cipher, v.userKey, content); err != nil {
			return false, fmt.Errorf("更新 Bitwarden 条目 %q 失败: %w", noteName, err)
		}
		return true, nil
	}
	if err := v.api.createSecureNote(ctx, folderID, v.userKey, noteName, content); err != nil {
		return false, fmt.Errorf("创建 Bitwarden 条目 %q 失败: %w", noteName, err)
	}
	return true, nil
}

// DeleteNote 删除一条 note。供吊销后清理使用，正常流程不会调用。
func (v *CloudVault) DeleteNote(ctx context.Context, folderName, noteName string) error {
	if err := v.ready(); err != nil {
		return err
	}
	folderID, err := v.api.findFolderID(ctx, folderName, v.userKey)
	if err != nil || folderID == "" {
		return err
	}
	ciphers, err := v.api.listCiphers(ctx)
	if err != nil {
		return err
	}
	for _, cipher := range ciphers {
		if cipher.Type != cipherTypeSecureNote || cipher.FolderID != folderID {
			continue
		}
		itemKey, err := itemDecryptionKey(cipher.Key, v.userKey)
		if err != nil {
			continue
		}
		name, err := decryptVaultString(cipher.Name, itemKey)
		if err != nil {
			continue
		}
		if strings.TrimSpace(name) != strings.TrimSpace(noteName) {
			continue
		}
		return v.api.deleteCipher(ctx, cipher.ID)
	}
	return nil
}
