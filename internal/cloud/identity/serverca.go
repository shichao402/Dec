package identity

import (
	"crypto/ecdsa"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/shichao402/Dec/internal/cloud/ssot"
)

// 服务端 CA 的存放与状态判定（ADR 0052）。
//
// CA 私钥只在两处：服务器本机 certs/ca.key，以及 Bitwarden 的
// _dec-cloud/server-ca/<指纹>。git 里只有 config/server-ca.json，
// 记录证书与指纹，作为"原来是哪一份 CA"的依据。
//
// 这里只管"本机文件 + git 记录"两件事，Bitwarden 的读写在 Bootstrapper 里，
// 因为它与根密钥共用同一个登录会话（一次主密码 + 2FA 取回两样）。

// CA 文件名。与 cloudsvc、emergency 的约定一致。
const (
	CAKeyFile  = "ca.key"
	CACertFile = "ca.crt"
)

// CAState 是启动时对服务端 CA 的判定结果。
type CAState string

const (
	// CAReady 表示本机 CA 与 git 记录一致，可以签发与校验设备证书。
	CAReady CAState = "ready"
	// CANeedsRestore 表示 git 里有记录、本机却没有私钥：服务器重建或文件丢失。
	// 必须走恢复流程，绝不自动生成新 CA。
	CANeedsRestore CAState = "needs_restore"
	// CAMismatch 表示本机有私钥但与 git 记录的指纹不一致。
	// 两边都可能是对的，自动选哪一边都可能毁掉设备身份，所以拒绝业务、等人处理。
	CAMismatch CAState = "mismatch"
)

// 错误定义。
var (
	// ErrCAUnavailable 表示服务端 CA 当前不可用（待恢复、全新或不一致）。
	ErrCAUnavailable = errors.New("server ca unavailable")
	// ErrCANotInBackup 表示 Bitwarden 里找不到对应指纹的 CA 条目。
	ErrCANotInBackup = errors.New("server ca not in backup")
	// ErrCAConfirmRequired 表示宣告 CA 丢失缺少显式确认。
	ErrCAConfirmRequired = errors.New("server ca loss not confirmed")
)

// CAFingerprint 返回 CA 证书 DER 的 SHA-256（十六进制）。
func CAFingerprint(ca *CA) string {
	if ca == nil || ca.cert == nil {
		return ""
	}
	sum := sha256.Sum256(ca.cert.Raw)
	return hex.EncodeToString(sum[:])
}

// FingerprintOfCertPEM 计算 PEM 证书的指纹，口径与 CAFingerprint 一致。
func FingerprintOfCertPEM(certPEM []byte) (string, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return "", fmt.Errorf("%w: CA 证书无法解析", ErrCertInvalid)
	}
	sum := sha256.Sum256(block.Bytes)
	return hex.EncodeToString(sum[:]), nil
}

// keyMatchesCert 确认私钥与证书里的公钥是同一对。
//
// LoadCA 只分别解析两样东西，不检查它们是否配对；从 Bitwarden 取回或
// 从磁盘读到的私钥如果与证书不配对，签出来的设备证书会全部验不过。
func keyMatchesCert(ca *CA) error {
	pub, ok := ca.cert.PublicKey.(*ecdsa.PublicKey)
	if !ok {
		return fmt.Errorf("%w: CA 证书公钥类型不支持", ErrCertInvalid)
	}
	if !pub.Equal(&ca.key.PublicKey) {
		return fmt.Errorf("%w: CA 私钥与证书不配对", ErrKeyMismatch)
	}
	return nil
}

// CABackupPayload 是写进 Bitwarden 的内容：私钥 + 证书两段 PEM。
//
// 证书一起存，是为了恢复时不依赖 git 记录里的证书也能重建 CA；
// git 记录只用来核对指纹，两边对上才算取回的是"原来那一份"。
func CABackupPayload(ca *CA) (string, error) {
	keyPEM, err := ca.KeyPEM()
	if err != nil {
		return "", err
	}
	return string(keyPEM) + string(ca.CertPEM()), nil
}

// ParseCABackupPayload 解析 Bitwarden 条目，并确认私钥与证书配对。
func ParseCABackupPayload(payload string) (*CA, error) {
	var keyPEM, certPEM []byte
	rest := []byte(payload)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		enc := pem.EncodeToMemory(block)
		switch block.Type {
		case "PRIVATE KEY":
			keyPEM = enc
		case "CERTIFICATE":
			certPEM = enc
		}
	}
	if keyPEM == nil || certPEM == nil {
		return nil, fmt.Errorf("%w: CA 冷备条目缺少私钥或证书", ErrCertInvalid)
	}
	ca, err := LoadCA(keyPEM, certPEM)
	if err != nil {
		return nil, err
	}
	if err := keyMatchesCert(ca); err != nil {
		return nil, err
	}
	return ca, nil
}

// CABackupName 返回 CA 在 Bitwarden 里的条目名。
func CABackupName(fingerprint string) string {
	return ssot.FolderServerCA + "/" + fingerprint
}

// ReadCARecord 读取 git 里的 CA 记录，不存在时返回 false。
func ReadCARecord(store *ssot.Store) (ssot.ServerCARecord, bool, error) {
	rec, err := ssot.ReadJSON[ssot.ServerCARecord](store, ssot.FileServerCA)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return ssot.ServerCARecord{}, false, nil
		}
		return ssot.ServerCARecord{}, false, err
	}
	return rec, true, nil
}

// WriteCARecord 把 CA 记录写入 git 并提交。
func WriteCARecord(store *ssot.Store, rec ssot.ServerCARecord, message string, author ssot.Author) error {
	rec.SchemaVersion = ssot.SchemaVersion
	_, err := store.Update(author, func(tx *ssot.Tx) (string, error) {
		return message, tx.WriteJSON(ssot.FileServerCA, rec)
	})
	return err
}

// LoadLocalCA 读取本机 CA 文件。两个文件都不存在时返回 (nil, nil)。
//
// 只缺一个视为损坏：证书丢了还能从 git 记录补，但私钥丢了证书留着
// 恰恰说明有人动过这个目录，不该被当成"全新"。
func LoadLocalCA(dir string) (*CA, error) {
	keyPEM, keyErr := os.ReadFile(filepath.Join(dir, CAKeyFile))
	certPEM, certErr := os.ReadFile(filepath.Join(dir, CACertFile))
	keyMissing := errors.Is(keyErr, os.ErrNotExist)
	certMissing := errors.Is(certErr, os.ErrNotExist)
	switch {
	case keyErr != nil && !keyMissing:
		return nil, fmt.Errorf("读取 CA 私钥失败: %w", keyErr)
	case certErr != nil && !certMissing:
		return nil, fmt.Errorf("读取 CA 证书失败: %w", certErr)
	case keyMissing:
		// 私钥不在，证书在不在都没用。
		return nil, nil
	case certMissing:
		return nil, fmt.Errorf("%w: 本机有 CA 私钥但没有证书", ErrCertInvalid)
	}
	ca, err := LoadCA(keyPEM, certPEM)
	if err != nil {
		return nil, err
	}
	if err := keyMatchesCert(ca); err != nil {
		return nil, err
	}
	return ca, nil
}

// WriteLocalCA 原子写入本机 CA 文件，私钥 0600。
//
// 先写证书再写私钥：私钥是"CA 是否存在"的判据，最后落地，
// 中途失败时宁可留下一个孤立证书（被判为"私钥缺失"），
// 也不留下没有证书的私钥。
func WriteLocalCA(dir string, ca *CA) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("创建证书目录失败: %w", err)
	}
	keyPEM, err := ca.KeyPEM()
	if err != nil {
		return err
	}
	if err := writeFileAtomic(filepath.Join(dir, CACertFile), ca.CertPEM(), 0o600); err != nil {
		return fmt.Errorf("写入 CA 证书失败: %w", err)
	}
	if err := writeFileAtomic(filepath.Join(dir, CAKeyFile), keyPEM, 0o600); err != nil {
		return fmt.Errorf("写入 CA 私钥失败: %w", err)
	}
	return nil
}

func writeFileAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// CAResolution 是 ResolveCA 的结果。
type CAResolution struct {
	CA     *CA
	State  CAState
	Record ssot.ServerCARecord
	// HasRecord 表示 git 里已有 CA 记录。
	HasRecord bool
	// Detail 是非 ready 状态的原因，写进日志与状态页。
	Detail string
}

// ResolveCA 在启动时判定服务端 CA 的状态（ADR 0052）。
//
//	本机有 / git 有，指纹一致   → CAReady
//	本机有 / git 有，指纹不一致 → CAMismatch（拒绝业务，等人处理）
//	本机文件损坏               → CAMismatch（同上；服务照常启动，好让人看得到原因）
//	本机有 / git 无            → 补写 git 记录（backup_state=pending），CAReady
//	                             这是本功能上线前的存量 CA
//	本机无 / git 有            → CANeedsRestore，绝不自动生成
//	本机无 / git 无            → 全新部署：生成并记录（pending），CAReady
func ResolveCA(dir string, store *ssot.Store, author ssot.Author) (CAResolution, error) {
	rec, hasRec, err := ReadCARecord(store)
	if err != nil {
		return CAResolution{}, err
	}
	local, loadErr := LoadLocalCA(dir)
	if loadErr != nil {
		return CAResolution{State: CAMismatch, Record: rec, HasRecord: hasRec,
			Detail: "本机 CA 文件无法使用: " + loadErr.Error()}, nil
	}

	switch {
	case local != nil && hasRec:
		if fp := CAFingerprint(local); fp != rec.Fingerprint {
			return CAResolution{State: CAMismatch, Record: rec, HasRecord: true,
				Detail: fmt.Sprintf("本机 CA 指纹 %s 与 git 记录 %s 不一致", shortFP(fp), shortFP(rec.Fingerprint))}, nil
		}
		return CAResolution{CA: local, State: CAReady, Record: rec, HasRecord: true}, nil
	case local != nil:
		rec = ssot.ServerCARecord{
			Fingerprint: CAFingerprint(local),
			CertPEM:     string(local.CertPEM()),
			BackupState: ssot.CABackupPending,
			CreatedAt:   time.Now().UTC(),
		}
		if err := WriteCARecord(store, rec, "记录存量服务端 CA "+shortFP(rec.Fingerprint)+"（Bitwarden 备份待补）", author); err != nil {
			return CAResolution{}, err
		}
		return CAResolution{CA: local, State: CAReady, Record: rec, HasRecord: true}, nil
	case hasRec:
		return CAResolution{State: CANeedsRestore, Record: rec, HasRecord: true,
			Detail: "本机没有服务端 CA 私钥，需要从 Bitwarden 恢复 " + CABackupName(rec.Fingerprint)}, nil
	default:
		// 全新部署：此刻没有 Bitwarden 凭据，只能先生成并本机落地。
		// 这份 CA 在首次初始化之前签不出任何设备证书（初始化前没有设备），
		// 而初始化会在同一个 Bitwarden 会话里先把它写进冷备并读回校验，
		// 备份失败则初始化失败——所以不存在"签过设备却没有备份"的 CA。
		ca, err := NewCA()
		if err != nil {
			return CAResolution{}, err
		}
		if err := WriteLocalCA(dir, ca); err != nil {
			return CAResolution{}, err
		}
		rec = ssot.ServerCARecord{
			Fingerprint: CAFingerprint(ca),
			CertPEM:     string(ca.CertPEM()),
			BackupState: ssot.CABackupPending,
			CreatedAt:   time.Now().UTC(),
		}
		if err := WriteCARecord(store, rec, "生成服务端 CA "+shortFP(rec.Fingerprint)+"（待首次初始化时写入 Bitwarden）", author); err != nil {
			return CAResolution{}, err
		}
		return CAResolution{CA: ca, State: CAReady, Record: rec, HasRecord: true}, nil
	}
}

// shortFP 截短指纹，只用于提交说明与日志。
func shortFP(fp string) string {
	if len(fp) > 12 {
		return fp[:12]
	}
	return fp
}
