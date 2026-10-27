package render

import (
	"errors"
	"fmt"
	"sort"

	"github.com/shichao402/Dec/internal/cloud/assets"
	"github.com/shichao402/Dec/internal/cloud/crypt"
	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/notice"
	"github.com/shichao402/Dec/internal/proto"
)

// Renderer 是渲染与下发引擎（需求 4）。
//
// 它不持有连接，只负责“某台设备现在应该拿到什么”。
// 推送时机由上层长连接管理决定：commit 后主动推，设备上线时对账补齐。
type Renderer struct {
	store  *ssot.Store
	writer *assets.Writer
	// states 保存每台设备的下发状态，位于仓库之外。
	//
	// 下发状态不进 git：每次回执都会变，进仓库只会产生噪声，
	// 而且它本来就不是需要版本化的内容（需求 5.2 只要求“只存在云端”）。
	states *StateStore
	// keyFn 提供当前根密钥。恢复模式下可能为空，此时任何含密钥的条目都渲染失败。
	keyFn func() *crypt.Key
}

// Options 是 Renderer 的构造参数。
type Options struct {
	Store *ssot.Store
	// StateDir 是设备状态目录。为空时使用 <store>/../state，仍不在仓库内。
	StateDir string
	// RootKey 返回当前根密钥，可以为空。
	RootKey func() *crypt.Key
}

// New 构造渲染器。
func New(opts Options) (*Renderer, error) {
	if opts.Store == nil {
		return nil, errors.New("缺少 SSOT 仓库")
	}
	stateDir := opts.StateDir
	if stateDir == "" {
		stateDir = defaultStateDir(opts.Store.Dir())
	}
	states, err := NewStateStore(stateDir)
	if err != nil {
		return nil, err
	}
	keyFn := opts.RootKey
	if keyFn == nil {
		keyFn = func() *crypt.Key { return nil }
	}
	return &Renderer{
		store:  opts.Store,
		writer: assets.NewWriter(opts.Store),
		states: states,
		keyFn:  keyFn,
	}, nil
}

// States 返回下发状态存储，供管理面查询覆盖记录与漂移。
func (r *Renderer) States() *StateStore { return r.states }

// CurrentRevision 返回当前 SSOT revision，供回执对账与状态展示。
func (r *Renderer) CurrentRevision() string { return r.store.Head() }

// 渲染产物。

// File 是一个渲染完成的落盘单元。
type File struct {
	// File 是给代理的下发描述。
	File proto.File
	// Hash 是内容哈希，服务端与代理共用同一算法（ssot.SumHex）。
	Hash string
}

// Alert 是渲染过程中的告警，由上层写进审计并推送通知（需求 4.5）。
type Alert struct {
	// ID 是出问题的条目。
	ID string
	// Reason 是可读原因。
	Reason string
}

// Rendered 是一台设备的完整期望状态。
type Rendered struct {
	Revision string
	// Files 按 ID 排序，保证同样的输入产出一致的顺序。
	Files []File
	// Alerts 是本次渲染中失败但被跳过的条目。这些条目不会出现在 Files 里，
	// 代理因此不会改动对应落点，落点保留上一次成功的内容。
	Alerts []Alert
}

// HashOf 返回某个条目本次渲染的内容哈希；不存在时返回空。
func (r *Rendered) HashOf(id string) (string, bool) {
	for _, f := range r.Files {
		if f.File.ID == id {
			return f.Hash, true
		}
	}
	return "", false
}

// index 建立 ID 到文件的索引。
func (r *Rendered) index() map[string]File {
	out := make(map[string]File, len(r.Files))
	for _, f := range r.Files {
		out[f.File.ID] = f
	}
	return out
}

// RenderDevice 渲染一台设备的期望状态。
//
// 设备只拿到自己订阅的内容：用户平面看 Device.UserBundles，
// 项目平面看该设备下每个 Project 的 Bundles。
func (r *Renderer) RenderDevice(dev ssot.Device) (*Rendered, error) {
	if dev.Revoked {
		// 已吊销的设备不再有任何期望状态，也不允许被下发。
		return nil, fmt.Errorf("%w: 设备 %s 已吊销", ssot.ErrNotFound, dev.ID)
	}

	projects, err := r.projectsOf(dev.ID)
	if err != nil {
		return nil, err
	}

	// 收集需要渲染的 (bundle, 平面) 组合。
	type bundleUse struct {
		bundle  string
		user    bool
		projIDs []string
	}
	uses := map[string]*bundleUse{}
	var order []string
	addUse := func(bundle string) *bundleUse {
		u, ok := uses[bundle]
		if !ok {
			u = &bundleUse{bundle: bundle}
			uses[bundle] = u
			order = append(order, bundle)
		}
		return u
	}
	for _, b := range dev.UserBundles {
		if err := ssot.ValidateBundle(b); err != nil {
			return nil, fmt.Errorf("设备 %s 订阅了非法 bundle: %w", dev.ID, err)
		}
		addUse(b).user = true
	}
	for _, p := range projects {
		for _, b := range p.Bundles {
			if err := ssot.ValidateBundle(b); err != nil {
				return nil, fmt.Errorf("项目 %s 订阅了非法 bundle: %w", projectLabel(p), err)
			}
			u := addUse(b)
			u.projIDs = append(u.projIDs, p.ID)
		}
	}
	sort.Strings(order)

	ctx := newContext(dev, r.secretLookup())
	defer ctx.wipe()

	var files []File
	var alerts []Alert
	// seenID 检出“同一 ID 被两个来源渲染”的情况。写入期已按条目名拦过跨 bundle 冲突，
	// 这里再拦一次，是因为订阅组合本身也可能把两个 bundle 拼到同一个落点。
	seenID := map[string]string{}
	// seenKey 检出 json_key 落点上的键冲突。
	seenKey := map[string]string{}

	for _, bundle := range order {
		u := uses[bundle]
		manifest, err := r.writer.ReadManifest(bundle)
		if err != nil {
			if errors.Is(err, ssot.ErrNotFound) {
				// 订阅了一个不存在的 bundle：属于配置错误，跳过并告警，不阻塞其它内容下发。
				alerts = append(alerts, Alert{ID: "bundle:" + bundle, Reason: "bundle 不存在"})
				continue
			}
			return nil, err
		}
		rawFiles, err := r.writer.ReadBundleFiles(bundle)
		if err != nil {
			return nil, err
		}
		content := make(map[string][]byte, len(rawFiles))
		for _, f := range rawFiles {
			content[f.Path] = f.Content
		}

		for _, item := range manifest.Items {
			// 决定这条条目要落到哪些平面。宿主表只算一次。
			hosts, err := hostsFor(item)
			if err != nil {
				alerts = append(alerts, Alert{ID: itemID(bundle, item), Reason: err.Error()})
				continue
			}
			var targets []proto.Target
			if u.user && hasPlane(item, ssot.PlaneUser) {
				for _, h := range hosts {
					targets = append(targets, proto.Target{
						Plan: proto.PlanUser,
						Root: h,
					})
				}
			}
			if hasPlane(item, ssot.PlaneProject) {
				for _, pid := range u.projIDs {
					for _, h := range hosts {
						targets = append(targets, proto.Target{
							Plan:      proto.PlanProject,
							ProjectID: pid,
							Root:      h,
						})
					}
				}
			}
			if len(targets) == 0 {
				continue
			}

			pieces, err := piecesOf(bundle, item, content)
			if err != nil {
				alerts = append(alerts, Alert{ID: itemID(bundle, item), Reason: err.Error()})
				continue
			}

			for _, p := range pieces {
				rendered, hasSecret, err := ctx.render(p.raw)
				if err != nil {
					// 需求 4.5：密钥解密失败时保留上一次成功的结果，不用空值渲染。
					// 这里跳过该条目，代理因此不会动对应落点。
					alerts = append(alerts, Alert{ID: p.id, Reason: err.Error()})
					continue
				}

				pf := proto.File{
					ID:      p.id,
					Targets: retarget(p, targets),
					Content: string(rendered),
					Secret:  hasSecret || p.forceSecret,
					Format:  proto.FormatWhole,
				}
				if item.Kind == ssot.KindMCP {
					server, err := mcpserverOf(rendered)
					if err != nil {
						alerts = append(alerts, Alert{ID: p.id, Reason: err.Error()})
						continue
					}
					pf.Format = proto.FormatJSONKey
					pf.Key = ssot.EntryKey(item.Name)
					pf.Content = string(server)
				}
				if pf.Secret {
					pf.Mode = 0o600
				}
				// 说明行在这里加，而不是在代理落盘时加（需求 6.4）。
				//
				// 代理加说明会让它的"写下内容"比"收到内容"多一行：代理按写下的
				// 字节算哈希，服务端按未加说明的字节算哈希，两个值永远不等，
				// 每次对账都判成差异并重发，形成无限重写循环。
				pf.Content = notice.Apply(p.rel, pf.Content, pf.Secret, pf.Format,
					notice.Locator{Bundle: bundle, Path: p.src})

				if prev, dup := seenID[pf.ID]; dup {
					alerts = append(alerts, Alert{
						ID:     pf.ID,
						Reason: fmt.Sprintf("条目被多个来源渲染（另一来源 %s），已跳过", prev),
					})
					continue
				}

				if pf.Format == proto.FormatJSONKey {
					if conflict := firstKeyConflict(seenKey, pf); conflict != "" {
						alerts = append(alerts, Alert{ID: pf.ID, Reason: conflict})
						continue
					}
					for _, t := range pf.Targets {
						seenKey[keySlot(t, pf.Key)] = pf.ID
					}
				}
				seenID[pf.ID] = bundle

				// 哈希口径必须与代理一致，否则每一轮对账都会误判为漂移。
				//
				// json_key 条目不能直接哈希 pf.Content：那是条目在源文件里的
				// 原始字节（带缩进换行），代理落盘后读回来的是容器里紧凑
				// 序列化的字节，两者永远不相等，drift 会长期恒为正。
				// 所以这类条目一律按「规范化后的语义」算，见 proto.NormalizeJSONEntry。
				hash := ""
				if pf.Format == proto.FormatJSONKey {
					norm, err := proto.NormalizeJSONEntry([]byte(pf.Content))
					if err != nil {
						// 给空哈希会在下一轮被当成漂移无限重发，
						// 不如当场报出来，让人看见是哪条内容有问题。
						alerts = append(alerts, Alert{ID: p.id, Reason: err.Error()})
						continue
					}
					hash = ssot.SumHex(norm)
				} else {
					hash = ssot.SumHex([]byte(pf.Content))
				}

				files = append(files, File{File: pf, Hash: hash})
			}
		}
	}

	sort.Slice(files, func(i, j int) bool { return files[i].File.ID < files[j].File.ID })
	sort.Slice(alerts, func(i, j int) bool { return alerts[i].ID < alerts[j].ID })
	return &Rendered{Revision: r.store.Head(), Files: files, Alerts: alerts}, nil
}

// retarget 为单个 piece 生成落点。
//
// 一个 manifest 条目可能展开成多个文件（skill 目录），每个文件的相对路径不同，
// 所以 Target.Path 必须按 piece 的相对路径重写，而不是用条目的路径。
// 落点的宿主（Root）与平面不变。
func retarget(p piece, base []proto.Target) []proto.Target {
	out := make([]proto.Target, 0, len(base))
	for _, t := range base {
		t.Path = p.rel
		out = append(out, t)
	}
	return out
}

// keySlot 是同一条目在某平面某宿主上的键位。
//
// 冲突判定只看“同一个配置文件里的同一个键”，与条目路径无关：
// 两个 bundle 往 cursor 的 mcp.json 写 dec-foo，就是冲突。
func keySlot(t proto.Target, key string) string {
	return string(t.Plan) + "|" + t.ProjectID + "|" + t.Root + "|" + key
}

// firstKeyConflict 返回第一个已被占用的键位描述，没有冲突时返回空。
func firstKeyConflict(seen map[string]string, pf proto.File) string {
	for _, t := range pf.Targets {
		if prev, dup := seen[keySlot(t, pf.Key)]; dup && prev != pf.ID {
			return fmt.Sprintf("落点键冲突：%s 在 %s/%s 已由 %s 占用",
				pf.Key, t.Plan, t.Root, prev)
		}
	}
	return ""
}

// itemID 返回条目的稳定标识前缀，用于告警定位。
func itemID(bundle string, item ssot.ManifestItem) string {
	return "asset:" + bundle + ":" + item.Path
}

func projectLabel(p ssot.Project) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}

// projectsOf 读取某台设备登记的项目。
func (r *Renderer) projectsOf(deviceID string) ([]ssot.Project, error) {
	table, err := ssot.ReadJSON[ssot.ProjectTable](r.store, ssot.FileProjects)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var out []ssot.Project
	for _, p := range table.Projects {
		if p.DeviceID == deviceID {
			out = append(out, p)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// secretLookup 返回一个按名取密钥明文的函数。
func (r *Renderer) secretLookup() func(string) ([]byte, error) {
	return func(name string) ([]byte, error) {
		key := r.keyFn()
		if key == nil {
			return nil, errors.New("根密钥未加载")
		}
		return r.writer.GetSecret(key, name)
	}
}
