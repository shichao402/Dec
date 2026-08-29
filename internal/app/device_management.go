package app

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/repo"
)

type ManagedProjectState struct {
	Root        string
	Label       string
	Name        string
	Exists      bool
	Initialized bool
	ConfigPath  string
	Error       string
}

type DeviceSummary struct {
	Initialized   bool
	RepoConnected bool
	RepoURL       string
	HomeDir       string
	Platform      string
	Projects      []ManagedProjectState
}

type DirectoryEntry struct {
	Name string
	Path string
}

type DirectoryListing struct {
	Current string
	Parent  string
	Home    string
	Roots   []string
	Entries []DirectoryEntry
}

type ProjectScanResult struct {
	ScanRoot string
	Projects []ManagedProjectState
}

func LoadDeviceSummary() (*DeviceSummary, error) {
	cfg, err := config.LoadGlobalConfig()
	if err != nil {
		return nil, err
	}
	connected, err := repo.IsConnected()
	if err != nil {
		return nil, err
	}
	home, _ := os.UserHomeDir()
	projects, err := ListManagedProjectStates()
	if err != nil {
		return nil, err
	}
	return &DeviceSummary{
		Initialized:   connected && strings.TrimSpace(cfg.RepoURL) != "",
		RepoConnected: connected,
		RepoURL:       strings.TrimSpace(cfg.RepoURL),
		HomeDir:       home,
		Platform:      runtime.GOOS,
		Projects:      projects,
	}, nil
}

func ListManagedProjectStates() ([]ManagedProjectState, error) {
	items, err := config.ListManagedProjects()
	if err != nil {
		return nil, err
	}
	result := make([]ManagedProjectState, 0, len(items))
	for _, item := range items {
		result = append(result, inspectManagedProject(item.Root, item.Label))
	}
	return result, nil
}

func RegisterManagedProject(root, label string) (*ManagedProjectState, error) {
	info, err := os.Stat(strings.TrimSpace(root))
	if err != nil {
		return nil, fmt.Errorf("项目目录不可用: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("项目路径不是目录")
	}
	item, err := config.RegisterManagedProject(root, label)
	if err != nil {
		return nil, err
	}
	state := inspectManagedProject(item.Root, item.Label)
	return &state, nil
}

func RemoveManagedProject(root string) (map[string]any, error) {
	removed, err := config.RemoveManagedProject(root)
	if err != nil {
		return nil, err
	}
	return map[string]any{"Removed": removed, "Root": root}, nil
}

func inspectManagedProject(root, label string) ManagedProjectState {
	state := ManagedProjectState{
		Root:       root,
		Label:      label,
		Name:       filepath.Base(root),
		ConfigPath: filepath.Join(root, ".dec", "config.yaml"),
	}
	info, err := os.Stat(root)
	if err != nil {
		state.Error = err.Error()
		return state
	}
	if !info.IsDir() {
		state.Error = "路径不是目录"
		return state
	}
	state.Exists = true
	mgr := config.NewProjectConfigManager(root)
	state.Initialized = mgr.Exists()
	if !state.Initialized {
		return state
	}
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		state.Error = err.Error()
		return state
	}
	if cfg != nil && strings.TrimSpace(cfg.ProjectName) != "" {
		state.Name = strings.TrimSpace(cfg.ProjectName)
	}
	return state
}

func BrowseDirectories(path string) (*DirectoryListing, error) {
	home, _ := os.UserHomeDir()
	path = strings.TrimSpace(path)
	if path == "" {
		path = home
	}
	current, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("解析目录失败: %w", err)
	}
	current = filepath.Clean(current)
	entries, err := os.ReadDir(current)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %w", err)
	}
	result := &DirectoryListing{
		Current: current,
		Home:    home,
		Roots:   filesystemRoots(),
	}
	if parent := filepath.Dir(current); parent != current {
		result.Parent = parent
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		result.Entries = append(result.Entries, DirectoryEntry{
			Name: entry.Name(),
			Path: filepath.Join(current, entry.Name()),
		})
	}
	sort.Slice(result.Entries, func(i, j int) bool {
		return strings.ToLower(result.Entries[i].Name) < strings.ToLower(result.Entries[j].Name)
	})
	return result, nil
}

func filesystemRoots() []string {
	if runtime.GOOS != "windows" {
		return []string{string(filepath.Separator)}
	}
	var roots []string
	for drive := 'A'; drive <= 'Z'; drive++ {
		root := string(drive) + `:\`
		if info, err := os.Stat(root); err == nil && info.IsDir() {
			roots = append(roots, root)
		}
	}
	return roots
}

func ScanManagedProjects(ctx context.Context, scanRoot string, maxDepth int, reporter Reporter) (*ProjectScanResult, error) {
	reporter = defaultReporter(reporter)
	root, err := config.NormalizeManagedProjectRoot(scanRoot)
	if err != nil {
		return nil, err
	}
	if maxDepth <= 0 {
		maxDepth = 5
	}
	if maxDepth > 12 {
		maxDepth = 12
	}
	if info, statErr := os.Stat(root); statErr != nil || !info.IsDir() {
		if statErr != nil {
			return nil, statErr
		}
		return nil, fmt.Errorf("扫描路径不是目录")
	}
	result := &ProjectScanResult{ScanRoot: root}
	seen := map[string]struct{}{}
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			emit(reporter, EventWarn, "projects.scan", fmt.Sprintf("跳过 %s: %v", path, walkErr), nil)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		depth := 0
		if rel != "." {
			depth = len(strings.Split(rel, string(filepath.Separator)))
		}
		if depth > maxDepth {
			return filepath.SkipDir
		}
		name := strings.ToLower(entry.Name())
		if path != root && (name == ".git" || name == "node_modules" || name == "vendor" || name == "target") {
			return filepath.SkipDir
		}
		configPath := filepath.Join(path, ".dec", "config.yaml")
		if info, statErr := os.Stat(configPath); statErr == nil && !info.IsDir() {
			key := strings.ToLower(filepath.Clean(path))
			if _, exists := seen[key]; !exists {
				seen[key] = struct{}{}
				result.Projects = append(result.Projects, inspectManagedProject(path, ""))
				emit(reporter, EventInfo, "projects.scan", "发现项目 "+path, nil)
			}
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(result.Projects, func(i, j int) bool {
		return strings.ToLower(result.Projects[i].Root) < strings.ToLower(result.Projects[j].Root)
	})
	return result, nil
}
