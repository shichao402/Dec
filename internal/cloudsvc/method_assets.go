package cloudsvc

import (
	"context"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/audit"
	"github.com/shichao402/Dec/internal/cloud/identity"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 资产写入与读取（需求 7、14）。

// mWriteAsset 同步写入一个 bundle（需求 7.1）。
//
// 写入完成后立刻广播，让在线设备的长轮询立即返回新内容。
// 这是“提交即推送”的全部实现：没有队列，也没有后台任务。
func (s *Server) mWriteAsset(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.AssetPayload)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "write_asset 载荷类型错误")
	}
	if err := requireWrite(req.Source, dev); err != nil {
		return proto.Fail(req.ID, proto.ErrForbidden, err.Error())
	}

	files := make([]assets.AssetFile, 0, len(in.Items))
	for _, it := range in.Items {
		files = append(files, assets.AssetFile{
			Path:       it.Path,
			Content:    it.Content,
			Executable: it.Executable,
		})
	}

	out, err := s.assets.WriteAsset(assets.WriteAssetInput{
		Bundle:       in.Bundle,
		Files:        files,
		Delete:       in.Delete,
		Manifest:     in.Manifest,
		BaseRevision: in.BaseRevision,
		Message:      in.Message,
		Author:       authorOf(req.Source, dev),
	})
	if err != nil {
		return failFromError(req.ID, err)
	}

	// 写入成功即推送。受影响设备清单由渲染层按订阅计算（需求 4.2）。
	s.NotifyChanged()
	s.recordRevision(nil, audit.EventAssetWrite, dev, req, in.Bundle,
		in.Message, out.Revision, true)
	return okPayload(req.ID, &proto.AssetResult{
		Bundle:   out.Bundle,
		Revision: out.Revision,
		Commit:   out.Commit,
		Pushed:   true,
	})
}

// mDeleteAsset 删除一个资产条目（需求 7.4）。
func (s *Server) mDeleteAsset(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.AssetPayload)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "delete_asset 载荷类型错误")
	}
	if err := requireWrite(req.Source, dev); err != nil {
		return proto.Fail(req.ID, proto.ErrForbidden, err.Error())
	}
	// 删除用 manifest 表达：调用方给出删完之后的 manifest 与剩余文件。
	// 这样删除和写入共用一条链路，不存在“删了但 manifest 没改”的中间态。
	var itemPath string
	if len(in.Delete) > 0 {
		itemPath = in.Delete[0]
	}
	out, err := s.assets.DeleteAsset(assets.WriteAssetInput{
		Bundle:       in.Bundle,
		Delete:       in.Delete,
		Manifest:     in.Manifest,
		BaseRevision: in.BaseRevision,
		Message:      in.Message,
		Author:       authorOf(req.Source, dev),
	}, itemPath)
	if err != nil {
		return failFromError(req.ID, err)
	}
	s.NotifyChanged()
	s.recordRevision(nil, audit.EventAssetDelete, dev, req, in.Bundle,
		itemPath, out.Revision, true)
	return okPayload(req.ID, &proto.AssetResult{
		Bundle:   out.Bundle,
		Revision: out.Revision,
		Commit:   out.Commit,
		Pushed:   true,
	})
}

// BundleInfo 是管理面看到的 bundle 概要。
type BundleInfo struct {
	Name    string   `json:"name"`
	Items   int      `json:"items"`
	Kinds   []string `json:"kinds"`
	Pending bool     `json:"pending,omitempty"`
}

// ListAssetsResponse 是资产列表。
type ListAssetsResponse struct {
	Bundles []BundleInfo `json:"bundles"`
}

// mListAssets 列出 bundle 与条目概要（需求 14.1）。
func (s *Server) mListAssets(req *proto.Request, dev ssot.Device) *proto.Response {
	bundles, err := s.assets.ListBundles()
	if err != nil {
		return failFromError(req.ID, err)
	}
	out := make([]BundleInfo, 0, len(bundles))
	for _, name := range bundles {
		m, err := s.assets.ReadManifest(name)
		if err != nil {
			// 单个 bundle 读不动不应让整个列表失败，报出来就好。
			out = append(out, BundleInfo{Name: name})
			continue
		}
		kinds := map[string]bool{}
		for _, item := range m.Items {
			kinds[string(item.Kind)] = true
		}
		out = append(out, BundleInfo{
			Name:  name,
			Items: len(m.Items),
			Kinds: sortedKeys(kinds),
		})
	}
	return okPayload(req.ID, &ListAssetsResponse{Bundles: out})
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// mReadAsset 返回一个 bundle 的完整内容（需求 14.1 的“资产浏览”）。
//
// 内容原样返回，`{{secret:...}}` 保持占位符：浏览资产不该把密钥明文
// 顺手带到浏览器里。要看明文是另一个方法，且会单独记审计。
func (s *Server) mReadAsset(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.ReadAssetRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "read_asset 载荷类型错误")
	}
	bundle := strings.TrimSpace(in.Bundle)
	if bundle == "" {
		return proto.Fail(req.ID, proto.ErrInvalid, "bundle 不能为空")
	}
	manifest, err := s.assets.ReadManifest(bundle)
	if err != nil {
		return failFromError(req.ID, err)
	}
	files, err := s.assets.ReadBundleFiles(bundle)
	if err != nil {
		return failFromError(req.ID, err)
	}
	out := &proto.ReadAssetResponse{
		Bundle:   bundle,
		Revision: manifest.Revision,
		Items:    make([]proto.AssetEntry, 0, len(manifest.Items)),
		Files:    make([]proto.AssetItem, 0, len(files)),
	}
	for _, item := range manifest.Items {
		entry := proto.AssetEntry{
			Kind: string(item.Kind),
			Path: item.Path,
			Name: item.Name,
			Host: item.Host,
		}
		for _, p := range item.Planes {
			entry.Planes = append(entry.Planes, string(p))
		}
		out.Items = append(out.Items, entry)
	}
	for _, f := range files {
		// manifest.yaml 是 Items 的落盘形式，内容完全由 manifest 表达，
		// 混在文件列表里只会让管理面多出一个不可编辑的条目。
		if f.Path == ssot.FileManifest {
			// 但它的原文要单独带回去：写入是整体替换语义，管理面必须原样写回。
			out.Manifest = f.Content
			continue
		}
		out.Files = append(out.Files, proto.AssetItem{
			Path:       f.Path,
			Content:    f.Content,
			Executable: f.Executable,
		})
	}
	return okPayload(req.ID, out)
}

// 密钥（需求 8）。

// mPutSecret 写入或更新一条密钥。
//
// 明文只存在于这次请求的内存里：加密后立刻丢弃，不进日志、不进审计详情。
func (s *Server) mPutSecret(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.SecretPayload)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "put_secret 载荷类型错误")
	}
	if err := requireWrite(req.Source, dev); err != nil {
		return proto.Fail(req.ID, proto.ErrForbidden, err.Error())
	}
	key := s.rootKeyOf()
	if key == nil {
		return proto.Fail(req.ID, proto.ErrUnauthorized, "根密钥未加载，无法加密")
	}
	revision, err := s.assets.PutSecret(key, assets.PutSecretInput{
		Name:         in.Name,
		Value:        in.Value,
		Notes:        in.Notes,
		Refs:         in.Refs,
		BaseRevision: in.BaseRevision,
		Message:      in.Message,
		Author:       authorOf(req.Source, dev),
	})
	if err != nil {
		return failFromError(req.ID, err)
	}
	// 密钥变化会改变引用它的资产渲染结果，所以要推送（需求 4.2）。
	s.NotifyChanged()
	// 审计只记名字，不记值：明文一旦进审计就永久留在 git 历史里。
	s.recordRevision(nil, audit.EventSecretWrite, dev, req, in.Name,
		in.Message, revision, true)
	return okPayload(req.ID, &proto.AssetResult{Revision: revision, Pushed: true})
}

// mDeleteSecret 删除一条密钥。
func (s *Server) mDeleteSecret(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.SecretDeleteRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "delete_secret 载荷类型错误")
	}
	if err := requireWrite(req.Source, dev); err != nil {
		return proto.Fail(req.ID, proto.ErrForbidden, err.Error())
	}
	revision, err := s.assets.DeleteSecret(assets.PutSecretInput{
		Name:    in.Name,
		Message: in.Message,
		Author:  authorOf(req.Source, dev),
	}, in.Force)
	if err != nil {
		return failFromError(req.ID, err)
	}
	s.NotifyChanged()
	s.recordRevision(nil, audit.EventSecretDelete, dev, req, in.Name,
		in.Message, revision, true)
	return okPayload(req.ID, &proto.SecretDeleteResponse{Name: in.Name, Revision: revision})
}

// mReadSecret 读取明文（需求 8.6）。
//
// 只有用户本人在管理面或 CLI 发起的请求可以看明文；
// MCP 来源永远被拒绝，避免一次提示注入就能把全部密钥读走（已定决策 12）。
func (s *Server) mReadSecret(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.ReadSecretRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "read_secret 载荷类型错误")
	}
	if !identity.CanReadSecret(req.Source) {
		return proto.Fail(req.ID, proto.ErrForbidden,
			"来源 "+string(req.Source)+" 不能读取密钥明文")
	}
	key := s.rootKeyOf()
	if key == nil {
		return proto.Fail(req.ID, proto.ErrUnauthorized, "根密钥未加载，无法解密")
	}
	value, err := s.assets.GetSecret(key, in.Name)
	if err != nil {
		return failFromError(req.ID, err)
	}
	// 明文用后即清：返回值本身是副本，交给序列化之后由 GC 回收。
	defer wipeBytes(value)
	// 查看明文属于高危操作，必须留痕（需求 8.6、14.5）。
	reason := strings.TrimSpace(in.Reason)
	if reason == "" {
		reason = "显式查看明文"
	}
	s.record(nil, audit.EventSecretRead, dev, req, in.Name, reason, true)
	return okPayload(req.ID, &proto.ReadSecretResponse{
		Name: in.Name, Value: string(value), Reason: reason,
	})
}

// wipeBytes 尽力清零敏感字节。
func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}

// ListSecretsResponse 是密钥元数据列表。
type ListSecretsResponse struct {
	Secrets []proto.SecretMeta `json:"secrets"`
}

// mListSecrets 列出密钥元数据，不含明文。
func (s *Server) mListSecrets(req *proto.Request, dev ssot.Device) *proto.Response {
	metas, err := s.assets.ListSecrets()
	if err != nil {
		return failFromError(req.ID, err)
	}
	return okPayload(req.ID, &ListSecretsResponse{Secrets: metas})
}

// 历史与回滚（需求 14.1、1.4）。

// mHistory 查询资产或密钥的历史。
func (s *Server) mHistory(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.HistoryRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "history 载荷类型错误")
	}
	kind := assets.HistoryKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	if !kind.Valid() {
		return proto.Fail(req.ID, proto.ErrInvalid, "kind 必须是 asset 或 secret")
	}
	entries, err := s.assets.History(context.Background(), kind, in.ID, in.Limit)
	if err != nil {
		return failFromError(req.ID, err)
	}
	return okPayload(req.ID, &proto.HistoryResponse{Entries: entries})
}

// mRollback 回滚到某个历史 revision。
//
// 回滚不是改写历史，而是把目标内容以新 commit 写回（需求 1.4），
// 所以误操作的回滚本身也能再被回滚。
func (s *Server) mRollback(req *proto.Request, payload any, dev ssot.Device) *proto.Response {
	in, ok := payload.(*proto.RollbackRequest)
	if !ok {
		return proto.Fail(req.ID, proto.ErrInvalid, "rollback 载荷类型错误")
	}
	if !identity.CanManageDevices(req.Source) {
		return proto.Fail(req.ID, proto.ErrForbidden, "该来源不能回滚（回滚会改变全部设备的期望状态，只允许在本机管理面发起）")
	}
	kind := assets.HistoryKind(strings.ToLower(strings.TrimSpace(in.Kind)))
	if !kind.Valid() {
		return proto.Fail(req.ID, proto.ErrInvalid, "kind 必须是 asset 或 secret")
	}
	if err := s.assets.Rollback(assets.RollbackInput{
		Kind:    kind,
		ID:      in.ID,
		Commit:  in.Commit,
		Message: in.Message,
		Author:  authorOf(req.Source, dev),
	}); err != nil {
		return failFromError(req.ID, err)
	}
	s.NotifyChanged()
	rev := s.render.CurrentRevision()
	// 回滚是高危操作，必须留痕（需求 18.11）。
	s.recordRevision(nil, audit.EventRollback, dev, req, string(kind)+":"+in.ID,
		"回滚到 "+in.Commit, rev, true)
	return okPayload(req.ID, &proto.RollbackResponse{
		Revision: rev,
		Commit:   rev,
		Pushed:   true,
	})
}
