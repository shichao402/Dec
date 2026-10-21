package app

import (
	"fmt"
	"time"

	"github.com/shichao402/Dec/internal/prune"
)

// QuarantineEntryView 是隔离区条目的对外视图（不含绝对路径上下文之外的信息）。
type QuarantineEntryView struct {
	ID           string `json:"id"`
	OriginalPath string `json:"original_path"`
	DisplayPath  string `json:"display_path"`
	RelPath      string `json:"rel_path,omitempty"`
	Kind         string `json:"kind,omitempty"`
	Bundle       string `json:"bundle,omitempty"`
	Plane        string `json:"plane,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Detail       string `json:"detail,omitempty"`
	MovedAt      string `json:"moved_at,omitempty"`
	ExpiresAt    string `json:"expires_at,omitempty"`
	Expired      bool   `json:"expired,omitempty"`
}

// ListQuarantine 列出隔离区条目（默认排除已过期的；includeExpired=true 包含）。
func ListQuarantine(includeExpired bool) ([]QuarantineEntryView, error) {
	store, err := mustPruneQuarantineStore()
	if err != nil {
		return nil, err
	}
	entries, err := store.List(includeExpired)
	if err != nil {
		return nil, err
	}
	out := make([]QuarantineEntryView, 0, len(entries))
	for _, e := range entries {
		out = append(out, entryView(e))
	}
	return out, nil
}

// RestoreQuarantine 恢复一条或多条隔离项到原始路径。
// ids 为空表示恢复全部未过期条目；否则按 ID 精确匹配。
// 冲突/过期的条目不覆盖、跳过并入 skipped，其余继续。
func RestoreQuarantine(ids []string) (restored []QuarantineEntryView, skipped []QuarantineEntryView, err error) {
	store, err := mustPruneQuarantineStore()
	if err != nil {
		return nil, nil, err
	}
	return restoreQuarantineWith(store, ids)
}

// restoreQuarantineWith 是隔离区恢复的核心实现，store 可注入（测试用）。
func restoreQuarantineWith(store *prune.Store, ids []string) ([]QuarantineEntryView, []QuarantineEntryView, error) {
	if store == nil {
		return nil, nil, fmt.Errorf("隔离区未初始化")
	}
	selected := ids
	if len(selected) == 0 {
		entries, listErr := store.List(false)
		if listErr != nil {
			return nil, nil, listErr
		}
		for _, e := range entries {
			selected = append(selected, e.ID)
		}
	}

	// 用全部条目建 ID → 视图 索引，用于丰富跳过项的展示。
	byID := make(map[string]QuarantineEntryView)
	if all, listErr := store.List(true); listErr == nil {
		for _, e := range all {
			byID[e.ID] = entryView(e)
		}
	}

	var restored, skippedQuarantine []QuarantineEntryView
	for _, id := range selected {
		entry, rErr := store.Restore(id)
		if rErr != nil {
			view, ok := byID[id]
			if !ok {
				view = QuarantineEntryView{ID: id}
			}
			view.Detail = rErr.Error()
			skippedQuarantine = append(skippedQuarantine, view)
			continue
		}
		restored = append(restored, entryView(entry))
	}
	return restored, skippedQuarantine, nil
}

// QuarantineSummary 给 TUI/CLI 展示隔离区现状：条数、占用、保留期、到期清单。
type QuarantineSummary struct {
	Count        int                    `json:"count"`
	Bytes        int64                  `json:"bytes"`
	Retention    string                 `json:"retention"`
	MaxBytes     int64                  `json:"max_bytes"`
	ExpiringSoon []QuarantineEntryView  `json:"expiring_soon,omitempty"` // 7 天内到期
}

// SummarizeQuarantine 返回隔离区快照，供状态查询使用（需求 3.4 / 需求 8.4）。
func SummarizeQuarantine() (*QuarantineSummary, error) {
	store, err := mustPruneQuarantineStore()
	if err != nil {
		return nil, err
	}
	entries, err := store.List(true)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var bytes int64
	var expiring []QuarantineEntryView
	for _, e := range entries {
		bytes += e.SizeBytes
		if !e.Expired(now) && e.ExpiresAt.Before(now.Add(7*24*time.Hour)) {
			expiring = append(expiring, entryView(e))
		}
	}
	return &QuarantineSummary{
		Count:        len(entries),
		Bytes:        bytes,
		Retention:    store.Retention().String(),
		MaxBytes:     store.MaxBytes(),
		ExpiringSoon: expiring,
	}, nil
}

func entryView(e prune.Entry) QuarantineEntryView {
	return QuarantineEntryView{
		ID:           e.ID,
		OriginalPath: e.OriginalPath,
		DisplayPath:  e.DisplayPath,
		RelPath:      e.RelPath,
		Kind:         e.Kind,
		Bundle:       e.Bundle,
		Plane:        e.Plane,
		Reason:       string(e.Reason),
		Detail:       e.Detail,
		MovedAt:      e.QuarantinedAt.UTC().Format(time.RFC3339),
		ExpiresAt:    e.ExpiresAt.UTC().Format(time.RFC3339),
		Expired:      e.Expired(time.Now().UTC()),
	}
}
