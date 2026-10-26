package identity

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
	"github.com/shichao402/Dec/internal/secrets"
)

// Bootstrapper 负责初始化、设备登记与根密钥恢复这三条流程。
//
// 三条流程的共同点：都要先向 Bitwarden 证明“这是账户主人”，
// 然后才能签发设备证书。区别只在初始化时额外生成根密钥并写入冷备。
type Bootstrapper struct {
	store       *ssot.Store
	ca          *CA
	reg         *Registry
	rootKeyPath string

	// vault 是 Bitwarden 访问入口，便于测试替换。
	vault VaultFactory

	mu sync.Mutex
	// bootstrapOpen 在没有任何设备时打开，登记第一台设备后永久关闭（需求 10.3）。
	bootstrapOpen bool
	// recoveryMode 表示本地根密钥文件缺失或损坏，服务只能完成恢复（需求 11.1）。
	recoveryMode bool
	// key 是当前根密钥，只存在内存与本地文件。
	keyMu sync.RWMutex
	key   *crypt.Key

	// failures 记录失败次数，用于失败限流（需求 10.7）。
	failures failureTracker
}

// RootKeyPath 返回根密钥文件路径。
//
// 只读访问器：部署与测试都需要确认“根密钥到底在哪”，
// 但没有任何调用方应该改它——改了就等于丢密钥。
func (b *Bootstrapper) RootKeyPath() string { return b.rootKeyPath }

// readCheck 读取 git 里的根密钥校验值。
//
// 尚未记录时返回零值与 false：这既覆盖“全新服务”，
// 也覆盖“本次改动之前就上线、仓库里还没有这个文件”的历史数据。
// 两种情况都必须能正常启动，不能因为缺一个校验文件就拒绝服务。
func (b *Bootstrapper) readCheck() (ssot.RootKeyCheck, bool, error) {
	check, err := ssot.ReadJSON[ssot.RootKeyCheck](b.store, ssot.FileRootKeyCheck)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return ssot.RootKeyCheck{}, false, nil
		}
		return ssot.RootKeyCheck{}, false, err
	}
	return check, true, nil
}

// writeCheck 把当前根密钥的 key id 与摘要写入 git（需求 9.3）。
func (b *Bootstrapper) writeCheck(key *crypt.Key, createdAt time.Time, rotatedFrom string, author ssot.Author) error {
	check := ssot.RootKeyCheck{
		SchemaVersion: ssot.SchemaVersion,
		KeyID:         key.ID(),
		Digest:        key.Digest(),
		CreatedAt:     createdAt.UTC(),
		RotatedFrom:   rotatedFrom,
	}
	_, err := b.store.Update(author, func(tx *ssot.Tx) (string, error) {
		return "记录根密钥校验值 " + key.ID(), tx.WriteJSON(ssot.FileRootKeyCheck, check)
	})
	return err
}

// VaultFactory 创建一个 Bitwarden 短生命周期会话。
type VaultFactory func() (Vault, error)

// Vault 是 Bootstrapper 需要的 Bitwarden 能力子集。
//
// 只声明这三个方法，便于测试替换，也说明云端确实只需要“登录 + 读写一条 note”。
type Vault interface {
	Unlock(ctx context.Context, email, password, totp string) error
	UpsertNote(ctx context.Context, folder, name, content string) (bool, error)
	GetNote(ctx context.Context, folder, name string) (string, bool, error)
	Close()
}

// Options 是构造参数。
type Options struct {
	Store       *ssot.Store
	CA          *CA
	Registry    *Registry
	RootKeyPath string
	Vault       VaultFactory
}

// NewBootstrapper 构造。
func NewBootstrapper(opts Options) (*Bootstrapper, error) {
	if opts.Store == nil || opts.CA == nil || opts.Registry == nil {
		return nil, fmt.Errorf("Bootstrapper 缺少必要依赖")
	}
	path := opts.RootKeyPath
	if path == "" {
		return nil, fmt.Errorf("根密钥路径不能为空")
	}
	vault := opts.Vault
	if vault == nil {
		vault = func() (Vault, error) { return secrets.NewCloudVault("", nil) }
	}
	return &Bootstrapper{
		store:       opts.Store,
		ca:          opts.CA,
		reg:         opts.Registry,
		rootKeyPath: path,
		vault:       vault,
	}, nil
}

// LoadOrEnterRecovery 在启动时加载根密钥。
//
// 三种情况需要区分清楚：
//
//   - 全新服务（无根密钥、无设备）：正常启动，初始化入口打开；
//   - 已登记过设备但根密钥文件缺失或损坏：进入恢复模式，只允许恢复流程（需求 11.1）；
//   - 根密钥正常：正常工作。
func (b *Bootstrapper) LoadOrEnterRecovery() error {
	initialized, err := b.reg.EnsureInitialized()
	if err != nil {
		return err
	}

	key, keyErr := crypt.LoadRootKey(b.rootKeyPath)
	if keyErr == nil {
		// 文件能解析还不够：必须与 git 里记录的校验值一致（需求 9.3）。
		//
		// 少了这一步，把另一份根密钥文件拷进来也能“正常启动”，
		// 然后所有密文静默解不开——那正是校验值要防的事。
		if err := b.verifyAgainstCheck(key); err != nil {
			key.Wipe()
			b.mu.Lock()
			b.recoveryMode = initialized
			b.bootstrapOpen = !initialized
			b.mu.Unlock()
			return nil
		}
		b.setKey(key)
		b.mu.Lock()
		b.recoveryMode = false
		b.bootstrapOpen = !initialized
		b.mu.Unlock()
		return nil
	}
	if !errors.Is(keyErr, crypt.ErrRootKeyMissing) && !errors.Is(keyErr, crypt.ErrRootKeyCorrupt) {
		return keyErr
	}

	b.mu.Lock()
	// 只有“登记过设备却拿不到根密钥”才算灾难，需要人工走恢复流程。
	b.recoveryMode = initialized
	// 没有任何设备时，初始化入口打开；这也覆盖了“初始化做到一半失败”的重试。
	b.bootstrapOpen = !initialized
	b.mu.Unlock()
	return nil
}

// verifyAgainstCheck 核对本地根密钥与 git 里的校验值。
//
// 没有记录时放行：那是本功能上线前的历史仓库，或者初始化写到一半。
// 有记录就必须完全一致，key id 与材料摘要两项都要对。
func (b *Bootstrapper) verifyAgainstCheck(key *crypt.Key) error {
	check, ok, err := b.readCheck()
	if err != nil {
		return err
	}
	if !ok || check.KeyID == "" {
		return nil
	}
	if check.KeyID != key.ID() {
		return fmt.Errorf("%w: git 记录的 key id 是 %s，本地文件是 %s",
			crypt.ErrRootKeyCorrupt, check.KeyID, key.ID())
	}
	if check.Digest != "" && check.Digest != key.Digest() {
		return fmt.Errorf("%w: 本地根密钥材料与 git 记录的摘要不一致", crypt.ErrRootKeyCorrupt)
	}
	return nil
}

// RootKey 返回当前根密钥，可能是 nil（未加载或恢复模式）。
func (b *Bootstrapper) RootKey() *crypt.Key {
	b.keyMu.RLock()
	defer b.keyMu.RUnlock()
	return b.key
}

func (b *Bootstrapper) setKey(key *crypt.Key) {
	b.keyMu.Lock()
	old := b.key
	b.key = key
	b.keyMu.Unlock()
	if old != nil && old != key {
		old.Wipe()
	}
}

// State 是当前引导状态，供管理面展示。
type State struct {
	BootstrapOpen bool
	RecoveryMode  bool
	KeyID         string
}

// State 返回当前状态。
func (b *Bootstrapper) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.keyMu.RLock()
	keyID := ""
	if b.key != nil {
		keyID = b.key.ID()
	}
	b.keyMu.RUnlock()
	return State{
		BootstrapOpen: b.bootstrapOpen,
		RecoveryMode:  b.recoveryMode,
		KeyID:         keyID,
	}
}

// Initialize 完成首次初始化。
//
// 步骤固定：生成根密钥 → 写本地文件 → 写 Bitwarden 冷备 → 读回校验 → 登记第一台设备。
// 只有冷备读回校验通过才算成功，避免“凭据已清、冷备没写进去”（需求 10.2、10.5）。
func (b *Bootstrapper) Initialize(ctx context.Context, req proto.EnrollBeginRequest) (*proto.EnrollBeginResponse, error) {
	b.mu.Lock()
	open := b.bootstrapOpen
	b.mu.Unlock()
	if !open {
		return nil, fmt.Errorf("%w: 初始化入口已关闭", ErrBootstrapClosed)
	}
	if b.RootKey() != nil {
		return nil, fmt.Errorf("%w: 根密钥已存在，无需重新初始化", ErrBootstrapClosed)
	}
	pub, err := ParsePublicKey([]byte(req.DevicePublicKey))
	if err != nil {
		return nil, err
	}
	deviceID, err := newDeviceID()
	if err != nil {
		return nil, err
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = "我的设备"
	}

	// 1. 生成根密钥并写本地文件。
	key, err := crypt.NewKey(nil)
	if err != nil {
		return nil, err
	}
	createdAt := time.Now().UTC()
	if err := crypt.WriteRootKey(b.rootKeyPath, key, createdAt, ""); err != nil {
		key.Wipe()
		return nil, err
	}

	// 2. 写冷备并读回校验。这一步失败就要回退本地文件，避免留下“有根密钥但没备份”的状态。
	payload, err := crypt.RootKeyPayload(key, createdAt, "")
	if err != nil {
		key.Wipe()
		return nil, err
	}
	if err := b.backupRootKey(ctx, req, key, payload); err != nil {
		key.Wipe()
		_ = removeFile(b.rootKeyPath)
		return nil, err
	}

	// 3. 签发证书并登记设备。
	issued, err := b.ca.SignDevice(deviceID, pub, time.Time{})
	if err != nil {
		key.Wipe()
		return nil, err
	}
	dev := ssot.Device{
		ID:           deviceID,
		Name:         deviceName,
		Kind:         ssot.DeviceMine,
		OS:           req.OS,
		Arch:         req.Arch,
		Fingerprint:  issued.Fingerprint,
		CertSerial:   issued.Serial,
		CertNotAfter: issued.NotAfter,
		RegisteredAt: time.Now().UTC(),
	}
	if err := b.reg.Add(dev, ssot.Author{Name: "cloud", Source: string(proto.SourceEnroll)}); err != nil {
		key.Wipe()
		return nil, err
	}

	b.setKey(key)
	b.mu.Lock()
	b.bootstrapOpen = false
	b.recoveryMode = false
	b.mu.Unlock()

	// 4. 记录校验值。放在最后：它代表“这把钥匙已经可用且已备份”，
	// 提前写会让一次失败的初始化在 git 里留下指向不存在根密钥的记录。
	if err := b.writeCheck(key, createdAt, "", ssot.Author{Name: "cloud", Source: string(proto.SourceEnroll)}); err != nil {
		return nil, err
	}

	return &proto.EnrollBeginResponse{
		DeviceID:     deviceID,
		Certificate:  string(issued.CertPEM),
		CA:           string(b.ca.CertPEM()),
		CertNotAfter: issued.NotAfter,
		IsFirst:      true,
	}, nil
}

// backupRootKey 写入冷备并读回校验。
func (b *Bootstrapper) backupRootKey(ctx context.Context, req proto.EnrollBeginRequest, key *crypt.Key, payload []byte) error {
	vault, err := b.vault()
	if err != nil {
		return err
	}
	defer vault.Close()

	if err := vault.Unlock(ctx, req.Email, req.Password, req.TOTP); err != nil {
		if errors.Is(err, secrets.ErrNeed2FA) {
			return fmt.Errorf("%w: 需要二次验证码", ErrNeed2FA)
		}
		return fmt.Errorf("Bitwarden 登录失败: %w", err)
	}

	// 冷备条目名按 key id 组织，且只增不改：轮换时新建条目，保留旧条目。
	name := ssot.FolderRootKey + "/" + key.ID()
	if _, err := vault.UpsertNote(ctx, ssot.DirRootKeyBackup, name, string(payload)); err != nil {
		return fmt.Errorf("写入根密钥冷备失败: %w", err)
	}

	// 读回校验：内容必须逐字节一致，否则冷备不可用。
	got, ok, err := vault.GetNote(ctx, ssot.DirRootKeyBackup, name)
	if err != nil {
		return fmt.Errorf("读取根密钥冷备失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("根密钥冷备写入后读不到")
	}
	if got != string(payload) {
		return fmt.Errorf("根密钥冷备读回内容不一致")
	}
	return nil
}

// Enroll 登记一台新设备。
//
// 唯一路径是“Bitwarden 主密码 + 2FA 核对”，没有登记码、没有邀请链接（需求 18.4）。
func (b *Bootstrapper) Enroll(ctx context.Context, req proto.EnrollBeginRequest) (*proto.EnrollBeginResponse, error) {
	if b.RootKey() == nil {
		return nil, fmt.Errorf("%w: 根密钥未加载，请先完成恢复", ErrRecoveryRequired)
	}
	if err := b.failures.check(); err != nil {
		return nil, err
	}
	pub, err := ParsePublicKey([]byte(req.DevicePublicKey))
	if err != nil {
		return nil, err
	}
	if err := b.verifyOwner(ctx, req); err != nil {
		b.failures.fail()
		return nil, err
	}
	b.failures.reset()

	deviceID, err := newDeviceID()
	if err != nil {
		return nil, err
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = "我的设备"
	}
	issued, err := b.ca.SignDevice(deviceID, pub, time.Time{})
	if err != nil {
		return nil, err
	}
	dev := ssot.Device{
		ID:           deviceID,
		Name:         deviceName,
		Kind:         ssot.DeviceMine,
		OS:           req.OS,
		Arch:         req.Arch,
		Fingerprint:  issued.Fingerprint,
		CertSerial:   issued.Serial,
		CertNotAfter: issued.NotAfter,
		RegisteredAt: time.Now().UTC(),
	}
	if err := b.reg.Add(dev, ssot.Author{Name: deviceID, Source: string(proto.SourceEnroll)}); err != nil {
		return nil, err
	}
	return &proto.EnrollBeginResponse{
		DeviceID:     deviceID,
		Certificate:  string(issued.CertPEM),
		CA:           string(b.ca.CertPEM()),
		CertNotAfter: issued.NotAfter,
	}, nil
}

// verifyOwner 用 Bitwarden 凭据核对“这是账户主人”。
//
// 具体做法：登录成功即证明身份；额外读一次根密钥冷备确认能取到当前根密钥，
// 这样既验证了凭据，也验证了冷备确实可用。
func (b *Bootstrapper) verifyOwner(ctx context.Context, req proto.EnrollBeginRequest) error {
	vault, err := b.vault()
	if err != nil {
		return err
	}
	defer vault.Close()
	if err := vault.Unlock(ctx, req.Email, req.Password, req.TOTP); err != nil {
		if errors.Is(err, secrets.ErrNeed2FA) {
			return fmt.Errorf("%w: 需要二次验证码", ErrNeed2FA)
		}
		return fmt.Errorf("Bitwarden 登录失败: %w", err)
	}
	key := b.RootKey()
	if key == nil {
		return fmt.Errorf("%w: 根密钥未加载", ErrRecoveryRequired)
	}
	name := ssot.FolderRootKey + "/" + key.ID()
	if _, ok, err := vault.GetNote(ctx, ssot.DirRootKeyBackup, name); err != nil {
		return fmt.Errorf("读取根密钥冷备失败: %w", err)
	} else if !ok {
		return fmt.Errorf("Bitwarden 里找不到当前根密钥的冷备条目 %s", name)
	}
	return nil
}

// Recover 从 Bitwarden 取回根密钥并登记设备。
//
// 用于根密钥文件丢失、服务器重建（需求 11）。
func (b *Bootstrapper) Recover(ctx context.Context, req proto.RecoverRequest) (*proto.EnrollBeginResponse, error) {
	if err := b.failures.check(); err != nil {
		return nil, err
	}
	pub, err := ParsePublicKey([]byte(req.DevicePublicKey))
	if err != nil {
		return nil, err
	}

	key, createdAt, rotatedFrom, err := b.fetchRootKey(ctx, req)
	if err != nil {
		b.failures.fail()
		return nil, err
	}
	if err := crypt.WriteRootKey(b.rootKeyPath, key, createdAt, rotatedFrom); err != nil {
		key.Wipe()
		return nil, err
	}
	b.setKey(key)
	b.failures.reset()

	// 恢复成功后重写校验值：重建的服务器上 git 是克隆来的，
	// 校验值本来就在，但改写一次可以把“这次恢复用的就是这把钥匙”
	// 明确记进历史，也顺带修复校验值缺失的历史仓库。
	if err := b.writeCheck(key, createdAt, rotatedFrom,
		ssot.Author{Name: "cloud", Source: string(proto.SourceEnroll)}); err != nil {
		return nil, err
	}

	deviceID, err := newDeviceID()
	if err != nil {
		return nil, err
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = "恢复用设备"
	}
	issued, err := b.ca.SignDevice(deviceID, pub, time.Time{})
	if err != nil {
		return nil, err
	}
	// 恢复后这台设备一定是“我的设备”，否则没人能管理服务。
	dev := ssot.Device{
		ID:           deviceID,
		Name:         deviceName,
		Kind:         ssot.DeviceMine,
		OS:           req.OS,
		Arch:         req.Arch,
		Fingerprint:  issued.Fingerprint,
		CertSerial:   issued.Serial,
		CertNotAfter: issued.NotAfter,
		RegisteredAt: time.Now().UTC(),
	}
	if err := b.reg.Add(dev, ssot.Author{Name: deviceID, Source: string(proto.SourceEnroll)}); err != nil {
		return nil, err
	}
	b.mu.Lock()
	b.recoveryMode = false
	b.mu.Unlock()

	return &proto.EnrollBeginResponse{
		DeviceID:     deviceID,
		Certificate:  string(issued.CertPEM),
		CA:           string(b.ca.CertPEM()),
		CertNotAfter: issued.NotAfter,
		IsFirst:      true,
	}, nil
}

// fetchRootKey 按 git 记录的 key id 取回根密钥（需求 11.2、11.3）。
//
// 早期实现是“取最新的一条”，那样在轮换过的仓库里会挑错：
// 最新条目未必是当前在用的那把，用错钥匙启动的后果是所有旧密文都解不开，
// 而且因为能“成功启动”，问题会拖到第一次读密钥才暴露。
// 现在必须按 key id 精确匹配，取不到就拒绝恢复并列出候选供排查。
func (b *Bootstrapper) fetchRootKey(ctx context.Context, req proto.RecoverRequest) (*crypt.Key, time.Time, string, error) {
	vault, err := b.vault()
	if err != nil {
		return nil, time.Time{}, "", err
	}
	defer vault.Close()
	if err := vault.Unlock(ctx, req.Email, req.Password, req.TOTP); err != nil {
		if errors.Is(err, secrets.ErrNeed2FA) {
			return nil, time.Time{}, "", fmt.Errorf("%w: 需要二次验证码", ErrNeed2FA)
		}
		return nil, time.Time{}, "", fmt.Errorf("Bitwarden 登录失败: %w", err)
	}

	notes, err := listRootKeyNotes(ctx, vault)
	if err != nil {
		return nil, time.Time{}, "", err
	}

	// 目标 key id 来自 git。缺失说明这个仓库从没记录过（本功能之前初始化的），
	// 这时退化为“取最新一条”并允许成功——否则历史仓库永远无法恢复。
	check, hasCheck, err := b.readCheck()
	if err != nil {
		return nil, time.Time{}, "", err
	}
	wantID := ""
	if hasCheck {
		wantID = check.KeyID
	}

	if len(notes) == 0 {
		// 冷备整个空掉是最糟的情形，错误信息必须直接给出该去哪找什么条目，
		// 否则用户只知道“恢复失败”，无从判断是自己删错了还是登录错账户。
		if wantID != "" {
			return nil, time.Time{}, "", fmt.Errorf(
				"Bitwarden 里没有任何根密钥冷备条目，但 git 记录当前根密钥为 %s，应存在 %s/%s",
				wantID, ssot.FolderRootKey, wantID)
		}
		return nil, time.Time{}, "", fmt.Errorf("Bitwarden 里没有根密钥冷备条目")
	}

	var best *crypt.Key
	var bestCreated time.Time
	var bestRotated string
	var available []string

	for name, content := range notes {
		key, created, rotated, err := crypt.ParseRootKeyPayload([]byte(content))
		if err != nil {
			continue
		}
		if wantID != "" && key.ID() != wantID {
			available = append(available, name)
			key.Wipe()
			continue
		}
		// 有明确目标时只会命中一条；退化路径下取创建时间最新的一条。
		if best == nil || created.After(bestCreated) {
			if best != nil {
				best.Wipe()
			}
			best, bestCreated, bestRotated = key, created, rotated
		} else {
			key.Wipe()
		}
	}

	if best == nil {
		if wantID != "" {
			sort.Strings(available)
			return nil, time.Time{}, "", fmt.Errorf(
				"git 记录当前根密钥为 %s，但 Bitwarden 里找不到 %s/%s（现有条目：%s）",
				wantID, ssot.FolderRootKey, wantID, strings.Join(available, ", "))
		}
		return nil, time.Time{}, "", fmt.Errorf("Bitwarden 里的根密钥冷备条目都无法解析")
	}
	// 校验摘要：key id 对得上但材料不对，说明冷备条目被改过，
	// 这种情况下宁可拒绝，也不要用一把来历不明的钥匙覆盖本地文件。
	if hasCheck && check.Digest != "" && check.Digest != best.Digest() {
		best.Wipe()
		return nil, time.Time{}, "", fmt.Errorf("冷备条目 %s/%s 的材料摘要与 git 记录不一致", ssot.FolderRootKey, wantID)
	}
	return best, bestCreated, bestRotated, nil
}

// listRootKeyNotes 取回冷备 folder 下的根密钥条目，保留条目名。
//
// 需要条目名而不只是内容：恢复要按 key id 精确定位，
// 并在取不到时把现有条目列给用户排查（需求 11.3）。
func listRootKeyNotes(ctx context.Context, vault Vault) (map[string]string, error) {
	lister, ok := vault.(interface {
		ListNotes(ctx context.Context, folder string) (map[string]string, error)
	})
	if !ok {
		return nil, fmt.Errorf("Bitwarden 会话不支持列出条目")
	}
	all, err := lister.ListNotes(ctx, ssot.DirRootKeyBackup)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string, len(all))
	for name, content := range all {
		if strings.HasPrefix(name, ssot.FolderRootKey+"/") {
			out[name] = content
		}
	}
	return out, nil
}

// Rotate 轮换根密钥。
//
// 需要当场提供 Bitwarden 凭据（需求 9.4）：轮换是不可逆的高危操作，
// 必须确认操作者能访问冷备，否则新根密钥一旦没备份进去就再也救不回来。
func (b *Bootstrapper) Rotate(ctx context.Context, req proto.RotateRequest, rekey func(oldKey, newKey *crypt.Key) (int, error)) (*proto.RotateResponse, error) {
	if err := b.failures.check(); err != nil {
		return nil, err
	}
	oldKey := b.RootKey()
	if oldKey == nil {
		return nil, fmt.Errorf("%w: 根密钥未加载", ErrRecoveryRequired)
	}
	enrollReq := proto.EnrollBeginRequest{Email: req.Email, Password: req.Password, TOTP: req.TOTP}
	if err := b.verifyOwner(ctx, enrollReq); err != nil {
		b.failures.fail()
		return nil, err
	}

	newKey, err := crypt.NewKey(nil)
	if err != nil {
		return nil, err
	}
	// 先把全部密文用新密钥重新加密，再写冷备与本地文件：
	// 顺序反过来的话，任何一步失败都会让新根密钥对着旧密文，彻底解不开。
	if _, err := rekey(oldKey, newKey); err != nil {
		newKey.Wipe()
		return nil, err
	}

	createdAt := time.Now().UTC()
	payload, err := crypt.RootKeyPayload(newKey, createdAt, oldKey.ID())
	if err != nil {
		newKey.Wipe()
		return nil, err
	}
	if err := b.backupRootKey(ctx, enrollReq, newKey, payload); err != nil {
		newKey.Wipe()
		return nil, err
	}
	if err := crypt.WriteRootKey(b.rootKeyPath, newKey, createdAt, oldKey.ID()); err != nil {
		newKey.Wipe()
		return nil, err
	}

	// 切换完成后再写校验值：写到这一步说明新密钥已备份、已落盘、已能解全部密文。
	if err := b.writeCheck(newKey, createdAt, oldKey.ID(),
		ssot.Author{Name: "cloud", Source: string(proto.SourceAdmin)}); err != nil {
		newKey.Wipe()
		return nil, err
	}

	oldID := oldKey.ID()
	b.setKey(newKey)
	b.failures.reset()
	return &proto.RotateResponse{
		OldKeyID:        oldID,
		NewKeyID:        newKey.ID(),
		BackupCheckedAt: createdAt.Format(time.RFC3339),
	}, nil
}

// 错误定义。
var (
	// ErrBootstrapClosed 表示初始化入口已关闭。
	ErrBootstrapClosed = errors.New("bootstrap closed")
	// ErrRecoveryRequired 表示必须先完成根密钥恢复。
	ErrRecoveryRequired = errors.New("recovery required")
	// ErrNeed2FA 表示需要二次验证码。
	ErrNeed2FA = errors.New("need 2FA")
	// ErrRateLimited 表示失败次数过多，进入冷却。
	ErrRateLimited = errors.New("too many failures")
)

// failureTracker 是简单的失败限流：连续 5 次失败后冷却 30 分钟。
//
// 这条限制针对的是公网上那个无需设备证书的登记入口（需求 10.7、18.6）。
type failureTracker struct {
	mu    sync.Mutex
	count int
	until time.Time
}

const (
	maxFailures   = 5
	cooldownAfter = 30 * time.Minute
)

func (f *failureTracker) check() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if time.Now().Before(f.until) {
		return fmt.Errorf("%w: 请在 %s 后重试", ErrRateLimited, f.until.Format(time.RFC3339))
	}
	return nil
}

func (f *failureTracker) fail() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count++
	if f.count >= maxFailures {
		f.until = time.Now().Add(cooldownAfter)
		f.count = 0
	}
}

func (f *failureTracker) reset() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.count = 0
	f.until = time.Time{}
}

// newDeviceID 生成设备标识。
func newDeviceID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("生成设备 ID 失败: %w", err)
	}
	return fmt.Sprintf("dev-%x", buf), nil
}

// GenerateDeviceKey 生成一对设备密钥，供代理首次运行使用。
func GenerateDeviceKey() (privPEM, pubPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("生成设备密钥失败: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER}),
		pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER}), nil
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}
