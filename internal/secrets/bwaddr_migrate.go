package secrets

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// BWFlatMove 是一条待搬移的远端条目：从旧的「整串 folder 名」布局搬到
// 「folder 只有项目名 + 平面进条目名」的布局。
type BWFlatMove struct {
	Scope     RemoteScope
	Kind      string // note | sshkey
	OldFolder string
	OldName   string
	NewFolder string
	NewName   string

	cipher bwCipher
}

// String 分开渲染 folder 与条目名。两者拼成路径后前后完全相同，唯一的变化就是
// 这条分界线的位置，拼起来打印等于什么都没说。
func (m BWFlatMove) String() string {
	return fmt.Sprintf("%s: folder[%s] 条目[%s] → folder[%s] 条目[%s]",
		m.Kind, m.OldFolder, m.OldName, m.NewFolder, m.NewName)
}

// BWFlatMigrationPlan 是一次性扁平化迁移的只读预览。
type BWFlatMigrationPlan struct {
	Moves []BWFlatMove
	// LegacyFolders 是搬空后应删除的旧 folder 名（<p>/private/<plane>）。
	LegacyFolders []string
	// Blockers 是必须人工处理的冲突；非空时拒绝执行。
	Blockers []string
	// Untouched 是既非旧布局也非项目 folder 的 folder 名，迁移不动它们。
	Untouched []string
}

// Fingerprint 是计划内容的稳定摘要，用于 dry-run 与 apply 之间确认远端未变。
func (p *BWFlatMigrationPlan) Fingerprint() string {
	if p == nil {
		return ""
	}
	lines := make([]string, 0, len(p.Moves)+len(p.LegacyFolders))
	for _, move := range p.Moves {
		lines = append(lines, fmt.Sprintf("%s|%s|%s|%s|%s|%s",
			move.Kind, move.OldFolder, move.OldName, move.NewFolder, move.NewName, move.cipher.ID))
	}
	for _, folder := range p.LegacyFolders {
		lines = append(lines, "folder|"+folder)
	}
	sort.Strings(lines)
	sum := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(sum[:8])
}

// PlanFlatMigration 预览把 <p>/private/<plane> folder 合并为 <p> folder 的搬移。
// 只读：不创建 folder、不改条目。
func (c *APIClient) PlanFlatMigration(ctx context.Context) (*BWFlatMigrationPlan, error) {
	userKey := UserKey()
	if len(userKey) == 0 {
		return nil, errVaultKeyNotReady
	}
	folders, err := c.listFolders(ctx)
	if err != nil {
		return nil, err
	}

	legacy := make(map[string]RemoteScope) // folderID → 旧布局 scope
	folderNameByID := make(map[string]string)
	targetFolderID := make(map[string]string) // 项目名 → 已存在的裸 folder ID
	plan := &BWFlatMigrationPlan{}
	for _, folder := range folders {
		name, decErr := decryptVaultString(folder.Name, userKey)
		if decErr != nil {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		folderNameByID[folder.ID] = name
		if scope, parseErr := ParseRemoteScope(name); parseErr == nil {
			legacy[folder.ID] = scope
			continue
		}
		if scope, scopeErr := NewRemoteScope(name, SyncPlaneProject); scopeErr == nil {
			targetFolderID[scope.P] = folder.ID
			continue
		}
		plan.Untouched = append(plan.Untouched, name)
	}

	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return nil, err
	}

	// 目标 folder 里已有的条目名，用于跨平面冲突检测。
	occupied := make(map[string]map[string]struct{})
	for _, cipher := range ciphers {
		pName, ok := pNameOfFolderID(cipher.FolderID, targetFolderID)
		if !ok {
			continue
		}
		name, decErr := decryptCipherName(cipher, userKey)
		if decErr != nil {
			continue
		}
		if occupied[pName] == nil {
			occupied[pName] = make(map[string]struct{})
		}
		occupied[pName][name] = struct{}{}
	}

	for _, cipher := range ciphers {
		scope, ok := legacy[cipher.FolderID]
		if !ok {
			continue
		}
		kind := ""
		switch cipher.Type {
		case cipherTypeSecureNote:
			kind = "note"
		case cipherTypeSSHKey:
			kind = "sshkey"
		default:
			plan.Blockers = append(plan.Blockers,
				fmt.Sprintf("folder %s 含 Dec 不认识的条目类型 %d，请先人工移出", scope.String(), cipher.Type))
			continue
		}
		oldName, decErr := decryptCipherName(cipher, userKey)
		if decErr != nil {
			plan.Blockers = append(plan.Blockers,
				fmt.Sprintf("folder %s 有条目名无法解密（cipher %s）", scope.String(), cipher.ID))
			continue
		}

		newName := oldName
		if _, already := bwPlaneSegmentOfItemName(oldName); !already {
			encoded, encErr := scope.encodeItemName(oldName)
			if encErr != nil {
				plan.Blockers = append(plan.Blockers,
					fmt.Sprintf("folder %s 的条目 %q 无法编码: %v", scope.String(), oldName, encErr))
				continue
			}
			newName = encoded
		}
		if _, conflict := occupied[scope.P][newName]; conflict {
			plan.Blockers = append(plan.Blockers,
				fmt.Sprintf("folder %s 已存在条目 %q，与 %s 的 %q 冲突", scope.P, newName, scope.String(), oldName))
			continue
		}
		if occupied[scope.P] == nil {
			occupied[scope.P] = make(map[string]struct{})
		}
		occupied[scope.P][newName] = struct{}{}

		plan.Moves = append(plan.Moves, BWFlatMove{
			Scope:     scope,
			Kind:      kind,
			OldFolder: scope.String(),
			OldName:   oldName,
			NewFolder: scope.folderName(),
			NewName:   newName,
			cipher:    cipher,
		})
	}

	for id := range legacy {
		plan.LegacyFolders = append(plan.LegacyFolders, folderNameByID[id])
	}
	sort.Slice(plan.Moves, func(i, j int) bool {
		if plan.Moves[i].OldFolder != plan.Moves[j].OldFolder {
			return plan.Moves[i].OldFolder < plan.Moves[j].OldFolder
		}
		return plan.Moves[i].OldName < plan.Moves[j].OldName
	})
	sort.Strings(plan.LegacyFolders)
	sort.Strings(plan.Untouched)
	return plan, nil
}

// ApplyFlatMigration 重新预览、比对指纹后执行搬移，并删除搬空的旧 folder。
func (c *APIClient) ApplyFlatMigration(ctx context.Context, expectFingerprint string, progress func(string)) error {
	report := func(msg string) {
		if progress != nil {
			progress(msg)
		}
	}
	userKey := UserKey()
	if len(userKey) == 0 {
		return errVaultKeyNotReady
	}
	plan, err := c.PlanFlatMigration(ctx)
	if err != nil {
		return err
	}
	if len(plan.Blockers) > 0 {
		return fmt.Errorf("存在 %d 个阻断冲突，拒绝执行", len(plan.Blockers))
	}
	if fp := strings.TrimSpace(expectFingerprint); fp != "" && fp != plan.Fingerprint() {
		return fmt.Errorf("远端已变化：预览指纹 %s，当前 %s", fp, plan.Fingerprint())
	}

	folderIDs := make(map[string]string)
	for _, move := range plan.Moves {
		if _, ok := folderIDs[move.NewFolder]; ok {
			continue
		}
		id, findErr := c.findFolderID(ctx, move.NewFolder, userKey)
		if findErr != nil {
			return findErr
		}
		if id == "" {
			id, findErr = c.createFolder(ctx, move.NewFolder, userKey)
			if findErr != nil {
				return fmt.Errorf("创建 folder %q: %w", move.NewFolder, findErr)
			}
			report(fmt.Sprintf("创建 folder %s", move.NewFolder))
		}
		folderIDs[move.NewFolder] = id
	}

	for _, move := range plan.Moves {
		cipherType := cipherTypeSecureNote
		if move.Kind == "sshkey" {
			cipherType = cipherTypeSSHKey
		}
		if err := c.renameCipherName(ctx, move.cipher, userKey, move.NewName, cipherType, folderIDs[move.NewFolder]); err != nil {
			return fmt.Errorf("搬移 %s: %w", move, err)
		}
		report(move.String())
	}

	for _, folder := range plan.LegacyFolders {
		id, findErr := c.findFolderID(ctx, folder, userKey)
		if findErr != nil {
			return findErr
		}
		if id == "" {
			continue
		}
		remaining, countErr := c.countFolderCiphers(ctx, id)
		if countErr != nil {
			return countErr
		}
		if remaining > 0 {
			return fmt.Errorf("旧 folder %q 仍有 %d 个条目，未删除", folder, remaining)
		}
		if err := c.deleteFolderByID(ctx, id); err != nil {
			return fmt.Errorf("删除旧 folder %q: %w", folder, err)
		}
		report("删除旧 folder " + folder)
	}
	return nil
}

func (c *APIClient) countFolderCiphers(ctx context.Context, folderID string) (int, error) {
	ciphers, err := c.listCiphers(ctx)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, cipher := range ciphers {
		if cipher.FolderID == folderID {
			count++
		}
	}
	return count, nil
}

func (c *APIClient) deleteFolderByID(ctx context.Context, folderID string) error {
	c.invalidateSnapshot()
	reqURL := strings.TrimRight(c.APIURL, "/") + "/folders/" + folderID
	return c.doAuthenticatedJSON(ctx, http.MethodDelete, reqURL, nil, nil)
}

func decryptCipherName(cipher bwCipher, userKey []byte) (string, error) {
	itemKey, err := itemDecryptionKey(cipher.Key, userKey)
	if err != nil {
		return "", err
	}
	name, err := decryptVaultString(strings.TrimSpace(cipher.Name), itemKey)
	if err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("条目名为空")
	}
	return name, nil
}

func pNameOfFolderID(folderID string, targetFolderID map[string]string) (string, bool) {
	if strings.TrimSpace(folderID) == "" {
		return "", false
	}
	for pName, id := range targetFolderID {
		if id == folderID {
			return pName, true
		}
	}
	return "", false
}
