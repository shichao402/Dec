package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/contribute"
	"github.com/shichao402/Dec/internal/install"
)

const overrideMetadataFile = "override.json"

type OfficialAssetEdit struct {
	Project        string
	Type           string
	Name           string
	BaseVersion    string
	OverrideID     string
	OverrideActive bool
	UpstreamURL    string
	Files          []OfficialAssetFile
}

type OfficialAssetFile struct {
	Path    string
	Content string
}

type OfficialAssetEditsState struct {
	Assets []OfficialAssetEdit
}

type officialOverrideMetadata struct {
	ID           string    `json:"id"`
	Project      string    `json:"project"`
	Type         string    `json:"type"`
	Name         string    `json:"name"`
	BaseVersion  string    `json:"base_version"`
	UpstreamRepo string    `json:"upstream_repo"`
	UpstreamKind string    `json:"upstream_kind"`
	UpstreamURL  string    `json:"upstream_url"`
	CreatedAt    time.Time `json:"created_at"`
}

type PreviewOfficialOverrideInput struct {
	Project string
	Type    string
	Name    string
	Files   []OfficialAssetFile
}

type PreviewOfficialOverrideResult struct {
	Diff string
}

type ApplyOfficialOverrideInput struct {
	PreviewOfficialOverrideInput
	OriginRepo string
	Title      string
	Body       string
	Mode       string
	Branch     string
}

type ApplyOfficialOverrideResult struct {
	ID   string
	Kind string
	URL  string
	Diff string
}

func overrideAssetDir(workspace Workspace, project, itemType, name string) string {
	return filepath.Join(workspace.Root, ".dec", "overrides", project, itemType, name)
}

func overrideSourcePath(workspace Workspace, asset install.CacheAsset) string {
	root := filepath.Join(overrideAssetDir(workspace, asset.Project, asset.Type, asset.Name), "files")
	return filepath.Join(root, filepath.Base(asset.Path))
}

func activeOverrideSource(workspace Workspace, asset install.CacheAsset) string {
	path := overrideSourcePath(workspace, asset)
	if _, err := os.Stat(path); err == nil {
		return path
	}
	return ""
}

func ListOfficialAssetEdits(workspace Workspace) (*OfficialAssetEditsState, error) {
	if workspace.EffectivePlane() != WorkspaceProject || strings.TrimSpace(workspace.Root) == "" {
		return nil, fmt.Errorf("本地覆写只支持具体项目")
	}
	cfg, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return nil, err
	}
	req, _ := workspaceOfficialRequires(workspace, cfg)
	state := &OfficialAssetEditsState{}
	for project := range req {
		assets, err := install.ListCache(workspaceCacheDir(workspace), project)
		if err != nil {
			return nil, err
		}
		version := install.ReadInstalledVersion(workspaceCacheDir(workspace), project)
		for _, asset := range assets {
			edit := OfficialAssetEdit{Project: project, Type: asset.Type, Name: asset.Name, BaseVersion: version}
			metaPath := filepath.Join(overrideAssetDir(workspace, project, asset.Type, asset.Name), overrideMetadataFile)
			if data, readErr := os.ReadFile(metaPath); readErr == nil {
				var meta officialOverrideMetadata
				if json.Unmarshal(data, &meta) == nil {
					edit.OverrideID = meta.ID
					edit.UpstreamURL = meta.UpstreamURL
					edit.OverrideActive = activeOverrideSource(workspace, asset) != ""
				}
			}
			state.Assets = append(state.Assets, edit)
		}
	}
	sort.Slice(state.Assets, func(i, j int) bool {
		a, b := state.Assets[i], state.Assets[j]
		return a.Project+"/"+a.Type+"/"+a.Name < b.Project+"/"+b.Type+"/"+b.Name
	})
	return state, nil
}

func LoadOfficialAssetEdit(workspace Workspace, in PreviewOfficialOverrideInput) (*OfficialAssetEdit, error) {
	asset, err := findOfficialCacheAsset(workspace, in.Project, in.Type, in.Name)
	if err != nil {
		return nil, err
	}
	source := asset.Path
	if override := activeOverrideSource(workspace, asset); override != "" {
		source = override
	}
	files, err := readEditableFiles(source)
	if err != nil {
		return nil, err
	}
	return &OfficialAssetEdit{
		Project: asset.Project, Type: asset.Type, Name: asset.Name,
		BaseVersion:    install.ReadInstalledVersion(workspaceCacheDir(workspace), asset.Project),
		OverrideActive: source != asset.Path, Files: files,
	}, nil
}

func PreviewOfficialOverride(workspace Workspace, in PreviewOfficialOverrideInput) (*PreviewOfficialOverrideResult, error) {
	asset, err := findOfficialCacheAsset(workspace, in.Project, in.Type, in.Name)
	if err != nil {
		return nil, err
	}
	base, err := readEditableFiles(asset.Path)
	if err != nil {
		return nil, err
	}
	diff := textFilesDiff(base, in.Files)
	if strings.TrimSpace(diff) == "" {
		return nil, fmt.Errorf("没有修改")
	}
	return &PreviewOfficialOverrideResult{Diff: diff}, nil
}

func ApplyOfficialOverride(ctx context.Context, workspace Workspace, in ApplyOfficialOverrideInput) (*ApplyOfficialOverrideResult, error) {
	preview, err := PreviewOfficialOverride(workspace, in.PreviewOfficialOverrideInput)
	if err != nil {
		return nil, err
	}
	asset, err := findOfficialCacheAsset(workspace, in.Project, in.Type, in.Name)
	if err != nil {
		return nil, err
	}
	id, err := newOverrideID()
	if err != nil {
		return nil, err
	}
	marker := "dec-override:" + id
	body := strings.TrimSpace(in.Body)
	if body != "" {
		body += "\n\n"
	}
	body += "Dec local override: `" + marker + "`\n"
	proposed, err := contribute.Propose(ctx, contribute.Options{
		OriginRepo: in.OriginRepo,
		Asset:      in.Project + "/" + in.Type + "/" + in.Name,
		Title:      in.Title,
		Body:       body,
		Diff:       preview.Diff,
		Mode:       contribute.Mode(in.Mode),
		Branch:     in.Branch,
	})
	if err != nil {
		return nil, err
	}

	dir := overrideAssetDir(workspace, asset.Project, asset.Type, asset.Name)
	tmp := dir + ".tmp-" + id
	defer os.RemoveAll(tmp)
	filesRoot := filepath.Join(tmp, "files", filepath.Base(asset.Path))
	if err := writeEditableFiles(filesRoot, in.Files, isDirectory(asset.Path)); err != nil {
		return nil, err
	}
	meta := officialOverrideMetadata{
		ID: id, Project: asset.Project, Type: asset.Type, Name: asset.Name,
		BaseVersion:  install.ReadInstalledVersion(workspaceCacheDir(workspace), asset.Project),
		UpstreamRepo: in.OriginRepo, UpstreamKind: proposed.Kind, UpstreamURL: proposed.URL,
		CreatedAt: time.Now().UTC(),
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(tmp, overrideMetadataFile), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return nil, err
	}
	if err := os.Rename(tmp, dir); err != nil {
		return nil, err
	}
	if err := renderOneOfficialAsset(workspace, asset, activeOverrideSource(workspace, asset)); err != nil {
		return nil, err
	}
	return &ApplyOfficialOverrideResult{ID: id, Kind: proposed.Kind, URL: proposed.URL, Diff: preview.Diff}, nil
}

func findOfficialCacheAsset(workspace Workspace, project, itemType, name string) (install.CacheAsset, error) {
	cfg, err := loadWorkspaceBundleConfig(workspace)
	if err != nil {
		return install.CacheAsset{}, err
	}
	req, _ := workspaceOfficialRequires(workspace, cfg)
	if !req.Has(project) {
		return install.CacheAsset{}, fmt.Errorf("%s 不是这个工作区的订阅", project)
	}
	assets, err := install.ListCache(workspaceCacheDir(workspace), project)
	if err != nil {
		return install.CacheAsset{}, err
	}
	for _, asset := range assets {
		if asset.Type == itemType && asset.Name == name {
			return asset, nil
		}
	}
	return install.CacheAsset{}, fmt.Errorf("未安装官方资产 %s/%s/%s", project, itemType, name)
}

func renderOneOfficialAsset(workspace Workspace, asset install.CacheAsset, source string) error {
	if source == "" {
		source = asset.Path
	}
	cfg, err := config.NewProjectConfigManager(workspace.Root).LoadProjectConfig()
	if err != nil {
		return err
	}
	selection, err := config.ResolveEffectiveIDEs(cfg)
	if err != nil {
		return err
	}
	_, err = installAssetToIDEs(asset.Type, asset.Name, asset.Project, source, workspace, uniqueWorkspaceIDEs(workspace, selection.IDEs))
	return err
}

func readEditableFiles(root string) ([]OfficialAssetFile, error) {
	var out []OfficialAssetFile
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	base := root
	if !info.IsDir() {
		base = filepath.Dir(root)
	}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if len(data) > 1024*1024 || !utf8.Valid(data) {
			return fmt.Errorf("%s 不是可编辑文本文件", path)
		}
		rel, err := filepath.Rel(base, path)
		if err != nil {
			return err
		}
		out = append(out, OfficialAssetFile{Path: filepath.ToSlash(rel), Content: string(data)})
		return nil
	})
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, err
}

func writeEditableFiles(root string, files []OfficialAssetFile, directory bool) error {
	for _, file := range files {
		rel := filepath.Clean(filepath.FromSlash(file.Path))
		if rel == "." || rel == ".." || filepath.IsAbs(rel) || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return fmt.Errorf("非法覆写路径 %q", file.Path)
		}
		dest := root
		if directory {
			dest = filepath.Join(root, rel)
		} else if len(files) != 1 {
			return fmt.Errorf("单文件资产只能提交一个文件")
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, []byte(file.Content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func textFilesDiff(before, after []OfficialAssetFile) string {
	old := make(map[string]string, len(before))
	for _, file := range before {
		old[file.Path] = file.Content
	}
	next := make(map[string]string, len(after))
	for _, file := range after {
		next[file.Path] = file.Content
	}
	names := make([]string, 0, len(old)+len(next))
	seen := map[string]bool{}
	for name := range old {
		seen[name] = true
		names = append(names, name)
	}
	for name := range next {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		if old[name] == next[name] {
			continue
		}
		fmt.Fprintf(&b, "--- a/%s\n+++ b/%s\n", name, name)
		for _, line := range strings.Split(strings.TrimSuffix(old[name], "\n"), "\n") {
			fmt.Fprintf(&b, "-%s\n", line)
		}
		for _, line := range strings.Split(strings.TrimSuffix(next[name], "\n"), "\n") {
			fmt.Fprintf(&b, "+%s\n", line)
		}
	}
	return b.String()
}

func isDirectory(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

func newOverrideID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func officialOverrideStatus(ctx context.Context, workspace Workspace, project, available string) (active, ready bool) {
	if workspace.EffectivePlane() != WorkspaceProject || workspace.Root == "" {
		return false, false
	}
	root := filepath.Join(workspace.Root, ".dec", "overrides", project)
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != overrideMetadataFile {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var meta officialOverrideMetadata
		if json.Unmarshal(data, &meta) != nil {
			return nil
		}
		active = true
		if available == "" || available == meta.BaseVersion {
			return nil
		}
		resolved, statusErr := contribute.Resolved(ctx, meta.UpstreamURL)
		if statusErr == nil && resolved {
			ready = true
		}
		return nil
	})
	return active, ready
}

func dropResolvedOverrides(ctx context.Context, workspace Workspace, project, available string) error {
	if workspace.EffectivePlane() != WorkspaceProject || workspace.Root == "" {
		return nil
	}
	root := filepath.Join(workspace.Root, ".dec", "overrides", project)
	var remove []string
	_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || entry.Name() != overrideMetadataFile {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		var meta officialOverrideMetadata
		if json.Unmarshal(data, &meta) != nil || meta.BaseVersion == available {
			return nil
		}
		resolved, statusErr := contribute.Resolved(ctx, meta.UpstreamURL)
		if statusErr == nil && resolved {
			remove = append(remove, filepath.Dir(path))
		}
		return nil
	})
	for _, dir := range remove {
		if err := os.RemoveAll(dir); err != nil {
			return err
		}
	}
	return nil
}
