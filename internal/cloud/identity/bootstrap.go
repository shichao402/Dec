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
	reg         *Registry
	rootKeyPath string
	// caDir 是服务端 CA 本机文件所在目录（ADR 0052）。
	caDir string

	// caMu 保护 CA 与它的状态。CA 会在恢复或宣告丢失后被替换，
	// 读取方（签发、校验设备证书）必须经 CurrentCA 拿当前值。
	caMu     sync.RWMutex
	ca       *CA
	caState  CAState
	caRecord ssot.ServerCARecord
	caDetail string

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
//
// CADir 与 CA 二选一：生产传 CADir，由 ResolveCA 判定状态；
// 测试可直接传一份 CA，视为已就绪且已备份。
type Options struct {
	Store       *ssot.Store
	CA          *CA
	CADir       string
	Registry    *Registry
	RootKeyPath string
	Vault       VaultFactory
}

// NewBootstrapper 构造。
func NewBootstrapper(opts Options) (*Bootstrapper, error) {
	if opts.Store == nil || opts.Registry == nil || (opts.CA == nil && opts.CADir == "") {
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
	b := &Bootstrapper{
		store:       opts.Store,
		reg:         opts.Registry,
		rootKeyPath: path,
		caDir:       opts.CADir,
		vault:       vault,
	}
	if opts.CA != nil {
		b.ca = opts.CA
		b.caState = CAReady
		b.caRecord = ssot.ServerCARecord{
			Fingerprint: CAFingerprint(opts.CA),
			CertPEM:     string(opts.CA.CertPEM()),
			BackupState: ssot.CABackupBitwarden,
		}
		return b, nil
	}
	res, err := ResolveCA(opts.CADir, opts.Store, ssot.Author{Name: "cloud", Source: "startup"})
	if err != nil {
		return nil, err
	}
	b.applyCA(res)
	return b, nil
}

// applyCA 切换当前 CA 与状态。
func (b *Bootstrapper) applyCA(res CAResolution) {
	b.caMu.Lock()
	defer b.caMu.Unlock()
	b.ca = res.CA
	b.caState = res.State
	b.caRecord = res.Record
	b.caDetail = res.Detail
}

// CurrentCA 返回当前服务端 CA；非 ready 时为 nil。
func (b *Bootstrapper) CurrentCA() *CA {
	b.caMu.RLock()
	defer b.caMu.RUnlock()
	if b.caState != CAReady {
		return nil
	}
	return b.ca
}

// CAStatus 是服务端 CA 的状态快照，供健康检查与管理面展示。
type CAStatus struct {
	State         CAState
	Fingerprint   string
	BackupPending bool
	Detail        string
}

// CAStatus 返回服务端 CA 状态。
func (b *Bootstrapper) CAStatus() CAStatus {
	b.caMu.RLock()
	defer b.caMu.RUnlock()
	return CAStatus{
		State:         b.caState,
		Fingerprint:   b.caRecord.Fingerprint,
		BackupPending: b.caState == CAReady && b.caRecord.BackupState != ssot.CABackupBitwarden,
		Detail:        b.caDetail,
	}
}

// requireCA 返回可用于签发的 CA，不可用时给出准确原因。
func (b *Bootstrapper) requireCA() (*CA, error) {
	if ca := b.CurrentCA(); ca != nil {
		return ca, nil
	}
	st := b.CAStatus()
	return nil, fmt.Errorf("%w: %s", ErrCAUnavailable, st.Detail)
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
	ca, err := b.requireCA()
	if err != nil {
		return nil, err
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

	// 2. 写冷备并读回校验：根密钥与服务端 CA 用同一个 Bitwarden 会话（ADR 0052）。
	// 任何一样失败都要回退本地根密钥文件，避免留下“有根密钥但没备份”的状态。
	payload, err := crypt.RootKeyPayload(key, createdAt, "")
	if err != nil {
		key.Wipe()
		return nil, err
	}
	vault, err := b.openVault(ctx, req.Email, req.Password, req.TOTP)
	if err != nil {
		key.Wipe()
		_ = removeFile(b.rootKeyPath)
		return nil, err
	}
	backupErr := writeRootKeyBackup(ctx, vault, key, payload)
	if backupErr == nil {
		backupErr = b.backupCAIfPending(ctx, vault)
	}
	vault.Close()
	if backupErr != nil {
		key.Wipe()
		_ = removeFile(b.rootKeyPath)
		return nil, backupErr
	}

	// 3. 签发证书并登记设备。
	issued, err := ca.SignDevice(deviceID, pub, time.Time{})
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
		CA:           string(ca.CertPEM()),
		CertNotAfter: issued.NotAfter,
		IsFirst:      true,
	}, nil
}

// openVault 打开一个已解锁的 Bitwarden 会话，调用方负责 Close。
func (b *Bootstrapper) openVault(ctx context.Context, email, password, totp string) (Vault, error) {
	vault, err := b.vault()
	if err != nil {
		return nil, err
	}
	if err := vault.Unlock(ctx, email, password, totp); err != nil {
		vault.Close()
		if errors.Is(err, secrets.ErrNeed2FA) {
			return nil, fmt.Errorf("%w: 需要二次验证码", ErrNeed2FA)
		}
		return nil, fmt.Errorf("Bitwarden 登录失败: %w", err)
	}
	return vault, nil
}

// backupRootKey 用独立会话写入根密钥冷备并读回校验。
func (b *Bootstrapper) backupRootKey(ctx context.Context, req proto.EnrollBeginRequest, key *crypt.Key, payload []byte) error {
	vault, err := b.openVault(ctx, req.Email, req.Password, req.TOTP)
	if err != nil {
		return err
	}
	defer vault.Close()
	return writeRootKeyBackup(ctx, vault, key, payload)
}

// writeRootKeyBackup 在已解锁的会话里写入根密钥冷备并读回校验。
func writeRootKeyBackup(ctx context.Context, vault Vault, key *crypt.Key, payload []byte) error {
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

// writeCABackup 把服务端 CA 写进 Bitwarden 并读回校验（ADR 0052）。
//
// 读回后不只比字节：还要能解析回同一份 CA（指纹一致、私钥配对），
// 否则恢复时才发现条目损坏，那时已经没有本机副本可以重写了。
func writeCABackup(ctx context.Context, vault Vault, ca *CA) error {
	payload, err := CABackupPayload(ca)
	if err != nil {
		return err
	}
	fp := CAFingerprint(ca)
	name := CABackupName(fp)
	if _, err := vault.UpsertNote(ctx, ssot.DirRootKeyBackup, name, payload); err != nil {
		return fmt.Errorf("写入服务端 CA 冷备失败: %w", err)
	}
	got, ok, err := vault.GetNote(ctx, ssot.DirRootKeyBackup, name)
	if err != nil {
		return fmt.Errorf("读取服务端 CA 冷备失败: %w", err)
	}
	if !ok {
		return fmt.Errorf("服务端 CA 冷备写入后读不到")
	}
	if got != payload {
		return fmt.Errorf("服务端 CA 冷备读回内容不一致")
	}
	back, err := ParseCABackupPayload(got)
	if err != nil {
		return fmt.Errorf("服务端 CA 冷备读回后无法解析: %w", err)
	}
	if CAFingerprint(back) != fp {
		return fmt.Errorf("服务端 CA 冷备读回后指纹不一致")
	}
	return nil
}

// backupCAIfPending 在已解锁的会话里补写服务端 CA 冷备。
//
// 走的是正常流程：首次初始化、登记新设备、根密钥轮换都要输主密码，
// 顺手把还没进 Bitwarden 的 CA 写进去，用户不需要为此单独操作一次。
func (b *Bootstrapper) backupCAIfPending(ctx context.Context, vault Vault) error {
	b.caMu.RLock()
	ca, state, rec := b.ca, b.caState, b.caRecord
	b.caMu.RUnlock()
	if state != CAReady || ca == nil || rec.BackupState == ssot.CABackupBitwarden {
		return nil
	}
	if err := writeCABackup(ctx, vault, ca); err != nil {
		return err
	}
	now := time.Now().UTC()
	rec.BackupState = ssot.CABackupBitwarden
	rec.BackedUpAt = &now
	if err := WriteCARecord(b.store, rec, "服务端 CA "+shortFP(rec.Fingerprint)+" 已写入 Bitwarden",
		ssot.Author{Name: "cloud", Source: string(proto.SourceEnroll)}); err != nil {
		return err
	}
	b.caMu.Lock()
	if b.ca == ca {
		b.caRecord = rec
	}
	b.caMu.Unlock()
	return nil
}

// fetchCA 按 git 记录的指纹从 Bitwarden 精确取回服务端 CA（ADR 0052）。
//
// 找不到时列出现有的 server-ca 条目，便于判断是登错账户还是条目被删。
func fetchCA(ctx context.Context, vault Vault, fingerprint string) (*CA, error) {
	name := CABackupName(fingerprint)
	got, ok, err := vault.GetNote(ctx, ssot.DirRootKeyBackup, name)
	if err != nil {
		return nil, fmt.Errorf("读取服务端 CA 冷备失败: %w", err)
	}
	if !ok {
		var available []string
		if lister, ok := vault.(interface {
			ListNotes(ctx context.Context, folder string) (map[string]string, error)
		}); ok {
			if all, lerr := lister.ListNotes(ctx, ssot.DirRootKeyBackup); lerr == nil {
				for n := range all {
					if strings.HasPrefix(n, ssot.FolderServerCA+"/") {
						available = append(available, n)
					}
				}
			}
		}
		sort.Strings(available)
		list := "无"
		if len(available) > 0 {
			list = strings.Join(available, ", ")
		}
		return nil, fmt.Errorf("%w: Bitwarden 里找不到 %s/%s（现有 CA 条目：%s）",
			ErrCANotInBackup, ssot.DirRootKeyBackup, name, list)
	}
	ca, err := ParseCABackupPayload(got)
	if err != nil {
		return nil, fmt.Errorf("服务端 CA 冷备条目 %s 无法使用: %w", name, err)
	}
	if CAFingerprint(ca) != fingerprint {
		return nil, fmt.Errorf("服务端 CA 冷备条目 %s 的内容与指纹不符", name)
	}
	return ca, nil
}

// Enroll 登记一台新设备。
//
// 唯一路径是“Bitwarden 主密码 + 2FA 核对”，没有登记码、没有邀请链接（需求 18.4）。
//
// 若这把设备公钥属于一条被标记为 NeedsReenroll 的旧记录（服务端 CA 重建过），
// 则沿用原设备 ID 重新签发证书：订阅、项目登记与设备类型原样保留（ADR 0052）。
func (b *Bootstrapper) Enroll(ctx context.Context, req proto.EnrollBeginRequest) (*proto.EnrollBeginResponse, error) {
	if b.RootKey() == nil {
		return nil, fmt.Errorf("%w: 根密钥未加载，请先完成恢复", ErrRecoveryRequired)
	}
	ca, err := b.requireCA()
	if err != nil {
		return nil, err
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

	fp, err := PublicKeyFingerprint(pub)
	if err != nil {
		return nil, err
	}
	if prev, ok := b.reenrollCandidate(fp); ok {
		issued, err := ca.SignDevice(prev.ID, pub, time.Time{})
		if err != nil {
			return nil, err
		}
		if _, err := b.reg.Update(prev.ID, ssot.Author{Name: prev.ID, Source: string(proto.SourceEnroll)},
			"设备 "+prev.ID+" 用原设备密钥重新登记", func(d *ssot.Device) {
				d.CertSerial = issued.Serial
				d.CertNotAfter = issued.NotAfter
				d.Fingerprint = issued.Fingerprint
				d.NeedsReenroll = false
				if req.OS != "" {
					d.OS = req.OS
				}
				if req.Arch != "" {
					d.Arch = req.Arch
				}
			}); err != nil {
			return nil, err
		}
		return &proto.EnrollBeginResponse{
			DeviceID:     prev.ID,
			Certificate:  string(issued.CertPEM),
			CA:           string(ca.CertPEM()),
			CertNotAfter: issued.NotAfter,
		}, nil
	}

	deviceID, err := newDeviceID()
	if err != nil {
		return nil, err
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = "我的设备"
	}
	issued, err := ca.SignDevice(deviceID, pub, time.Time{})
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
		CA:           string(ca.CertPEM()),
		CertNotAfter: issued.NotAfter,
	}, nil
}

// reenrollCandidate 找出“同一把设备公钥”的未吊销旧记录。
//
// 同一把公钥再次通过主密码 + 2FA 登记，说明就是那台设备本身：
// 沿用原 ID 重签证书，订阅与项目登记原样保留，不会多出一条重复设备。
// 服务端 CA 重建后的重新登记（NeedsReenroll）是最主要的场景。
// 吊销过的设备不能借此复活，必须换一把密钥重新登记。
func (b *Bootstrapper) reenrollCandidate(fingerprint string) (ssot.Device, bool) {
	devices, err := b.reg.Devices()
	if err != nil {
		return ssot.Device{}, false
	}
	for _, d := range devices {
		if !d.Revoked && d.Fingerprint != "" && d.Fingerprint == fingerprint {
			return d, true
		}
	}
	return ssot.Device{}, false
}

// verifyOwner 用 Bitwarden 凭据核对“这是账户主人”。
//
// 具体做法：登录成功即证明身份；额外读一次根密钥冷备确认能取到当前根密钥，
// 这样既验证了凭据，也验证了冷备确实可用。会话还开着，
// 顺手把尚未备份的服务端 CA 写进去（ADR 0052）。
func (b *Bootstrapper) verifyOwner(ctx context.Context, req proto.EnrollBeginRequest) error {
	vault, err := b.openVault(ctx, req.Email, req.Password, req.TOTP)
	if err != nil {
		return err
	}
	defer vault.Close()
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
	return b.backupCAIfPending(ctx, vault)
}

// RecoverResult 描述一次恢复取回了什么。
type RecoverResult struct {
	// Enrolled 是恢复时顺带登记的设备；RestoreOnly 时为 nil。
	Enrolled *proto.EnrollBeginResponse
	// RootKeyRestored 表示本次取回了根密钥。
	RootKeyRestored bool
	// CARestored 表示本次从 Bitwarden 取回了服务端 CA。
	CARestored bool
	// CARebuilt 表示用户宣告 CA 丢失，本次生成了新 CA。
	CARebuilt bool
	// ReenrollDevices 是因 CA 重建而需要重新登记的设备数。
	ReenrollDevices int
}

// Recover 从 Bitwarden 取回缺失的根密钥与服务端 CA（需求 11、ADR 0052）。
//
// 一次登录、一个会话完成全部取回：服务器连磁盘都丢了的时候，
// 用户只需要输一次主密码和 2FA。
//
// 顺序是先根密钥、后 CA，全部成功后才切换内存状态。任何一步失败都不留下
// 半恢复的局面——根密钥文件若已写入，会在 CA 失败时撤回。
func (b *Bootstrapper) Recover(ctx context.Context, req proto.RecoverRequest) (*RecoverResult, error) {
	if err := b.failures.check(); err != nil {
		return nil, err
	}
	// 只有“登记过设备却拿不到根密钥”才需要取回根密钥；全新服务没有可取的东西。
	needKey := b.RootKey() == nil && b.State().RecoveryMode
	caSt := b.CAStatus()
	needCA := caSt.State == CANeedsRestore
	if caSt.State == CAMismatch {
		return nil, fmt.Errorf("%w: %s；本机 CA 与 git 记录冲突，需要人工确认保留哪一份", ErrCAUnavailable, caSt.Detail)
	}
	if !needKey && !needCA {
		return nil, fmt.Errorf("%w: 根密钥与服务端 CA 都在，无需恢复", ErrBootstrapClosed)
	}
	var pub *ecdsa.PublicKey
	if !req.RestoreOnly {
		p, err := ParsePublicKey([]byte(req.DevicePublicKey))
		if err != nil {
			return nil, err
		}
		pub = p
	}

	vault, err := b.openVault(ctx, req.Email, req.Password, req.TOTP)
	if err != nil {
		b.failures.fail()
		return nil, err
	}
	defer vault.Close()

	out := &RecoverResult{}

	// 1. 根密钥。
	var key *crypt.Key
	var createdAt time.Time
	var rotatedFrom string
	if needKey {
		key, createdAt, rotatedFrom, err = b.fetchRootKey(ctx, vault)
		if err != nil {
			b.failures.fail()
			return nil, err
		}
	}

	// 2. 服务端 CA：按指纹精确取；取不到且用户已确认丢失，才重建。
	var newCA *CA
	var newRec ssot.ServerCARecord
	if needCA {
		b.caMu.RLock()
		rec := b.caRecord
		b.caMu.RUnlock()
		got, ferr := fetchCA(ctx, vault, rec.Fingerprint)
		switch {
		case ferr == nil:
			newCA = got
			newRec = rec
			now := time.Now().UTC()
			newRec.BackupState = ssot.CABackupBitwarden
			if newRec.BackedUpAt == nil {
				newRec.BackedUpAt = &now
			}
			out.CARestored = true
		case errors.Is(ferr, ErrCANotInBackup) && req.DeclareCALost:
			rebuilt, rerr := NewCA()
			if rerr != nil {
				wipeKey(key)
				return nil, rerr
			}
			if werr := writeCABackup(ctx, vault, rebuilt); werr != nil {
				wipeKey(key)
				return nil, werr
			}
			now := time.Now().UTC()
			newCA = rebuilt
			newRec = ssot.ServerCARecord{
				Fingerprint:         CAFingerprint(rebuilt),
				CertPEM:             string(rebuilt.CertPEM()),
				BackupState:         ssot.CABackupBitwarden,
				CreatedAt:           now,
				BackedUpAt:          &now,
				ReplacedFingerprint: rec.Fingerprint,
			}
			out.CARebuilt = true
		default:
			wipeKey(key)
			if !errors.Is(ferr, ErrCANotInBackup) {
				b.failures.fail()
			}
			return nil, ferr
		}
	}
	b.failures.reset()

	// 3. 落地：根密钥文件、CA 文件，再写 git 记录。
	if key != nil {
		if err := crypt.WriteRootKey(b.rootKeyPath, key, createdAt, rotatedFrom); err != nil {
			key.Wipe()
			return nil, err
		}
	}
	if newCA != nil {
		if err := WriteLocalCA(b.caDir, newCA); err != nil {
			if key != nil {
				key.Wipe()
				_ = removeFile(b.rootKeyPath)
			}
			return nil, err
		}
		msg := "从 Bitwarden 恢复服务端 CA " + shortFP(newRec.Fingerprint)
		if out.CARebuilt {
			msg = "服务端 CA " + shortFP(newRec.ReplacedFingerprint) + " 宣告丢失，重建为 " + shortFP(newRec.Fingerprint)
		}
		if err := WriteCARecord(b.store, newRec, msg, ssot.Author{Name: "cloud", Source: string(proto.SourceEnroll)}); err != nil {
			return nil, err
		}
		if out.CARebuilt {
			n, err := b.markAllNeedReenroll()
			if err != nil {
				return nil, err
			}
			out.ReenrollDevices = n
		}
		b.applyCA(CAResolution{CA: newCA, State: CAReady, Record: newRec, HasRecord: true})
	}
	if key != nil {
		b.setKey(key)
		// 重写校验值：把“这次恢复用的就是这把钥匙”记进历史，也修复缺校验值的历史仓库。
		if err := b.writeCheck(key, createdAt, rotatedFrom,
			ssot.Author{Name: "cloud", Source: string(proto.SourceEnroll)}); err != nil {
			return nil, err
		}
		out.RootKeyRestored = true
	}
	b.mu.Lock()
	b.recoveryMode = false
	b.mu.Unlock()

	// 4. 可选：登记发起恢复的这台设备。
	if req.RestoreOnly {
		return out, nil
	}
	ca, err := b.requireCA()
	if err != nil {
		return nil, err
	}
	fp, err := PublicKeyFingerprint(pub)
	if err != nil {
		return nil, err
	}
	if prev, ok := b.reenrollCandidate(fp); ok {
		issued, err := ca.SignDevice(prev.ID, pub, time.Time{})
		if err != nil {
			return nil, err
		}
		if _, err := b.reg.Update(prev.ID, ssot.Author{Name: prev.ID, Source: string(proto.SourceEnroll)},
			"设备 "+prev.ID+" 在恢复时重新登记", func(d *ssot.Device) {
				d.CertSerial = issued.Serial
				d.CertNotAfter = issued.NotAfter
				d.Fingerprint = issued.Fingerprint
				d.NeedsReenroll = false
			}); err != nil {
			return nil, err
		}
		out.Enrolled = &proto.EnrollBeginResponse{
			DeviceID: prev.ID, Certificate: string(issued.CertPEM), CA: string(ca.CertPEM()),
			CertNotAfter: issued.NotAfter,
		}
		return out, nil
	}
	deviceID, err := newDeviceID()
	if err != nil {
		return nil, err
	}
	deviceName := strings.TrimSpace(req.DeviceName)
	if deviceName == "" {
		deviceName = "恢复用设备"
	}
	issued, err := ca.SignDevice(deviceID, pub, time.Time{})
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
	out.Enrolled = &proto.EnrollBeginResponse{
		DeviceID:     deviceID,
		Certificate:  string(issued.CertPEM),
		CA:           string(ca.CertPEM()),
		CertNotAfter: issued.NotAfter,
		IsFirst:      true,
	}
	return out, nil
}

// markAllNeedReenroll 把全部未吊销设备标记为需要重新登记，返回条数。
//
// 旧 CA 签出的证书在新 CA 下全部验不过，这是事实；
// 但记录不删，同一把设备公钥重新登记时会接回原 ID 与订阅。
func (b *Bootstrapper) markAllNeedReenroll() (int, error) {
	devices, err := b.reg.Devices()
	if err != nil {
		return 0, err
	}
	n := 0
	for _, d := range devices {
		if d.Revoked || d.NeedsReenroll {
			continue
		}
		if _, err := b.reg.Update(d.ID, ssot.Author{Name: "cloud", Source: string(proto.SourceEnroll)},
			"服务端 CA 重建，设备 "+d.ID+" 需要重新登记", func(x *ssot.Device) {
				x.NeedsReenroll = true
			}); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

func wipeKey(k *crypt.Key) {
	if k != nil {
		k.Wipe()
	}
}

// fetchRootKey 按 git 记录的 key id 取回根密钥（需求 11.2、11.3）。
//
// 早期实现是“取最新的一条”，那样在轮换过的仓库里会挑错：
// 最新条目未必是当前在用的那把，用错钥匙启动的后果是所有旧密文都解不开，
// 而且因为能“成功启动”，问题会拖到第一次读密钥才暴露。
// 现在必须按 key id 精确匹配，取不到就拒绝恢复并列出候选供排查。
func (b *Bootstrapper) fetchRootKey(ctx context.Context, vault Vault) (*crypt.Key, time.Time, string, error) {
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
// 服务端 CA 不受根密钥保护，轮换不涉及它（需求 9.5、ADR 0052）。
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
