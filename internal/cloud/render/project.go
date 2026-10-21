package render

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/shichao402/Dec/internal/cloud/ssot"
	"github.com/shichao402/Dec/internal/proto"
)

// 项目登记与订阅（需求 23.3、23.4）。
//
// 项目登记的内容是“设备、本机绝对路径、订阅的 bundle”。
// 服务端只保存这些，不接触项目内容，也不自动发现或创建项目。

var (
	// ErrBadProject 表示项目登记参数不合规。
	ErrBadProject = errors.New("bad project")
	// ErrProjectExists 表示同一设备上路径重复。
	ErrProjectExists = errors.New("project path exists")
)

// ProjectStore 管理项目登记。
type ProjectStore struct {
	store *ssot.Store
}

// NewProjectStore 构造项目登记存储。
func NewProjectStore(store *ssot.Store) *ProjectStore { return &ProjectStore{store: store} }

// List 返回全部项目登记，按设备 ID 与路径排序。
func (p *ProjectStore) List() ([]ssot.Project, error) {
	table, err := ssot.ReadJSON[ssot.ProjectTable](p.store, ssot.FileProjects)
	if err != nil {
		if errors.Is(err, ssot.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	out := table.Projects
	sort.Slice(out, func(i, j int) bool {
		if out[i].DeviceID != out[j].DeviceID {
			return out[i].DeviceID < out[j].DeviceID
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

// OfDevice 返回某台设备的全部项目。
func (p *ProjectStore) OfDevice(deviceID string) ([]ssot.Project, error) {
	all, err := p.List()
	if err != nil {
		return nil, err
	}
	out := make([]ssot.Project, 0, len(all))
	for _, pr := range all {
		if pr.DeviceID == deviceID {
			out = append(out, pr)
		}
	}
	return out, nil
}

// Put 新增或修改一个项目登记。
//
// 修改时不允许改所属设备：项目是“某台机器上的某个目录”，换设备就是新登记。
func (p *ProjectStore) Put(in proto.PutProjectRequest, author ssot.Author) (ssot.Project, error) {
	if in.DeviceID == "" {
		return ssot.Project{}, fmt.Errorf("%w: 缺少设备 ID", ErrBadProject)
	}
	path := normalizePath(in.Path)
	if path == "" {
		return ssot.Project{}, fmt.Errorf("%w: 项目路径不能为空", ErrBadProject)
	}
	if !isUnderHome(path) {
		// 需求 23.4：项目根目录必须位于用户目录之下。服务端只能按字符串判断，
		// 真正的存在性校验由代理完成，因为只有代理知道那台机器的文件系统。
		return ssot.Project{}, fmt.Errorf("%w: 项目路径 %q 必须位于用户目录之下", ErrBadProject, in.Path)
	}
	for _, b := range in.Bundles {
		if err := ssot.ValidateBundle(b); err != nil {
			return ssot.Project{}, err
		}
	}
	bundles := dedupStrings(in.Bundles)

	var saved ssot.Project
	_, err := p.store.Update(author, func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.ProjectTable](tx.Store(), ssot.FileProjects)
		if err != nil && !errors.Is(err, ssot.ErrNotFound) {
			return "", err
		}
		now := time.Now().UTC()

		if in.ID == "" {
			// 新增：同一设备上同一路径只能登记一次。
			for _, pr := range table.Projects {
				if pr.DeviceID == in.DeviceID && pr.Path == path {
					return "", fmt.Errorf("%w: %s 上已登记过 %s", ErrProjectExists, in.DeviceID, path)
				}
			}
			saved = ssot.Project{
				ID:           newProjectID(in.DeviceID, path),
				DeviceID:     in.DeviceID,
				Name:         in.Name,
				Path:         path,
				Bundles:      bundles,
				RegisteredAt: now,
			}
			table.Projects = append(table.Projects, saved)
			return fmt.Sprintf("登记项目 %s", displayName(saved)), tx.WriteJSON(ssot.FileProjects, table)
		}

		idx := -1
		for i, pr := range table.Projects {
			if pr.ID == in.ID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return "", fmt.Errorf("%w: 项目 %s", ssot.ErrNotFound, in.ID)
		}
		cur := table.Projects[idx]
		if cur.DeviceID != in.DeviceID {
			return "", fmt.Errorf("%w: 项目 %s 属于设备 %s，不能改到 %s",
				ErrBadProject, in.ID, cur.DeviceID, in.DeviceID)
		}
		for i, pr := range table.Projects {
			if i == idx {
				continue
			}
			if pr.DeviceID == in.DeviceID && pr.Path == path {
				return "", fmt.Errorf("%w: %s 上已登记过 %s", ErrProjectExists, in.DeviceID, path)
			}
		}
		cur.Name = in.Name
		cur.Path = path
		cur.Bundles = bundles
		table.Projects[idx] = cur
		saved = cur
		return fmt.Sprintf("修改项目 %s", displayName(saved)), tx.WriteJSON(ssot.FileProjects, table)
	})
	if err != nil {
		return ssot.Project{}, err
	}
	return saved, nil
}

// Delete 删除一个项目登记。
//
// 删除只影响云端登记与订阅，不会动那台机器上的任何文件：
// 代理会按对账结果把该项目平面的下发项删掉（需求 6.1 的单向语义）。
func (p *ProjectStore) Delete(id string, author ssot.Author) (ssot.Project, error) {
	var removed ssot.Project
	_, err := p.store.Update(author, func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.ProjectTable](tx.Store(), ssot.FileProjects)
		if err != nil {
			return "", err
		}
		idx := -1
		for i, pr := range table.Projects {
			if pr.ID == id {
				idx = i
				break
			}
		}
		if idx < 0 {
			return "", fmt.Errorf("%w: 项目 %s", ssot.ErrNotFound, id)
		}
		removed = table.Projects[idx]
		table.Projects = append(table.Projects[:idx], table.Projects[idx+1:]...)
		return fmt.Sprintf("删除项目 %s", displayName(removed)), tx.WriteJSON(ssot.FileProjects, table)
	})
	if err != nil {
		return ssot.Project{}, err
	}
	return removed, nil
}

// SetSubscriptions 修改设备的用户平面订阅（需求 23.3）。
func (p *ProjectStore) SetSubscriptions(deviceID string, bundles []string, author ssot.Author) (ssot.Device, error) {
	for _, b := range bundles {
		if err := ssot.ValidateBundle(b); err != nil {
			return ssot.Device{}, err
		}
	}
	clean := dedupStrings(bundles)

	var saved ssot.Device
	_, err := p.store.Update(author, func(tx *ssot.Tx) (string, error) {
		table, err := ssot.ReadJSON[ssot.DeviceTable](tx.Store(), ssot.FileDevices)
		if err != nil {
			return "", err
		}
		idx := -1
		for i, d := range table.Devices {
			if d.ID == deviceID {
				idx = i
				break
			}
		}
		if idx < 0 {
			return "", fmt.Errorf("%w: 设备 %s", ssot.ErrNotFound, deviceID)
		}
		table.Devices[idx].UserBundles = clean
		saved = table.Devices[idx]
		return fmt.Sprintf("修改设备 %s 的用户平面订阅", deviceID), tx.WriteJSON(ssot.FileDevices, table)
	})
	if err != nil {
		return ssot.Device{}, err
	}
	return saved, nil
}

// EnsureProjectOwner 校验项目是否属于某台设备。
//
// 代理只能看到自己的项目，所以请求里带的项目必须已在云端为该设备登记过（需求 23.4）。
func (p *ProjectStore) EnsureProjectOwner(deviceID, projectID string) error {
	all, err := p.OfDevice(deviceID)
	if err != nil {
		return err
	}
	for _, pr := range all {
		if pr.ID == projectID {
			return nil
		}
	}
	return fmt.Errorf("%w: 项目 %s 未在设备 %s 上登记", ErrBadProject, projectID, deviceID)
}

func displayName(p ssot.Project) string {
	if p.Name != "" {
		return p.Name
	}
	return p.ID
}
