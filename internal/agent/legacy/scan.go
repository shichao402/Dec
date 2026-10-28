package legacy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/shichao402/Dec/internal/notice"
	"github.com/shichao402/Dec/internal/proto"
)

// ErrExpectedUnknown 表示还拿不到云端为本设备期望的条目，扫描不能进行（需求 2.3.1）。
var ErrExpectedUnknown = errors.New("还没有和云端完成过一次对账，拿不到本设备的期望条目，不能判断哪些是新系统的落点")

// Expected 是云端当前为本设备期望的条目：绝对路径与共享配置里的键名。
type Expected struct {
	paths map[string]bool
	// keys 是共享配置文件里由新系统托管的键，按文件绝对路径索引。
	keys map[string]map[string]bool
}

// NewExpected 从云端给出的核对清单构造期望条目。
//
// 空清单是合法的（设备没订阅任何 bundle），和“拿不到清单”是两回事：
// 后者由调用方传 nil 表达。
func NewExpected(entries []proto.ManifestEntry) *Expected {
	e := &Expected{paths: map[string]bool{}, keys: map[string]map[string]bool{}}
	for _, entry := range entries {
		for _, p := range entry.Paths {
			p = filepath.Clean(p)
			if entry.Format == proto.FormatJSONKey && entry.Key != "" {
				if e.keys[p] == nil {
					e.keys[p] = map[string]bool{}
				}
				e.keys[p][entry.Key] = true
				continue
			}
			e.paths[p] = true
		}
	}
	return e
}

// Input 是一次扫描的输入。零值字段取默认值。
type Input struct {
	// Home 是用户目录，必填。
	Home string
	// StateDir 是新代理的状态目录，默认 <Home>/.dec-agent。
	StateDir string
	// Projects 是新代理登记的项目根。
	Projects []string
	// Expected 是云端期望条目；为 nil 时扫描失败。
	Expected *Expected
	// GOOS 默认取当前平台，测试用来覆盖 macOS 专属规则。
	GOOS string
	// Applications 是查找旧 Console 的应用目录，默认 /Applications 与 <Home>/Applications。
	Applications []string
	// SystemBin 是系统级二进制目录，默认 /usr/local/bin。
	SystemBin string
	// ExtraProtected 是额外的硬排除路径，例如当前代理的可执行文件。
	ExtraProtected []string
}

// Item 是一条命中。
type Item struct {
	Rule    string `json:"rule"`
	Group   Group  `json:"group"`
	Action  Action `json:"action"`
	Path    string `json:"path"`
	Project string `json:"project,omitempty"`
	// Entries 是 strip 动作要删的条目（MCP 键、配置行），或 list 动作的补充说明。
	Entries []string `json:"entries,omitempty"`
	// Size 是会进入归档的字节数；list 动作为 0。
	Size  int64 `json:"size"`
	IsDir bool  `json:"is_dir,omitempty"`
}

// Skipped 是命中规则但被排除面挡下的对象，展示出来便于核对排除面没有漏判。
type Skipped struct {
	Rule   string `json:"rule"`
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Report 是扫描结果。
type Report struct {
	Items    []Item    `json:"items"`
	Skipped  []Skipped `json:"skipped,omitempty"`
	Projects []string  `json:"projects"`
	// ArchiveBytes 是本次执行会归档的未压缩字节数（需求 3.1）。
	ArchiveBytes int64    `json:"archive_bytes"`
	Warnings     []string `json:"warnings,omitempty"`
}

// Empty 报告是否没有任何需要清理的旧版残留（需求 3.3）。只列出的凭据不算。
func (r *Report) Empty() bool {
	for _, it := range r.Items {
		if it.Action != ActionList {
			return false
		}
	}
	return true
}

type scanner struct {
	in        Input
	report    *Report
	protected []string
	// roots 是 RootDec 根目录本身，只能删里面的内容（需求 2.3）。
	roots map[string]bool
}

// Scan 只读扫描本机旧版残留（需求 3.1）。
func Scan(in Input) (*Report, error) {
	if in.Expected == nil {
		return nil, ErrExpectedUnknown
	}
	if strings.TrimSpace(in.Home) == "" {
		return nil, errors.New("用户目录为空")
	}
	in.Home = filepath.Clean(in.Home)
	if in.StateDir == "" {
		in.StateDir = filepath.Join(in.Home, ".dec-agent")
	}
	if in.GOOS == "" {
		in.GOOS = runtime.GOOS
	}
	if in.Applications == nil {
		in.Applications = []string{"/Applications", filepath.Join(in.Home, "Applications")}
	}
	if in.SystemBin == "" {
		in.SystemBin = "/usr/local/bin"
	}

	s := &scanner{in: in, report: &Report{Items: []Item{}}, roots: map[string]bool{}}
	s.protected = s.protectedPaths()
	s.report.Projects = s.projects()

	s.roots[filepath.Join(in.Home, ".dec")] = true
	for _, p := range s.report.Projects {
		s.roots[filepath.Join(p, ".dec")] = true
	}

	s.scanUser()
	for _, p := range s.report.Projects {
		s.scanProject(p)
	}

	sort.SliceStable(s.report.Items, func(i, j int) bool {
		a, b := s.report.Items[i], s.report.Items[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		return a.Path < b.Path
	})
	for _, it := range s.report.Items {
		if it.Action != ActionList {
			s.report.ArchiveBytes += it.Size
		}
	}
	return s.report, nil
}

// protectedPaths 是新代理自身的硬排除面（需求 2.3 第一项）。
func (s *scanner) protectedPaths() []string {
	home := s.in.Home
	out := []string{
		s.in.StateDir,
		filepath.Join(home, ".local", "bin", "dec-agent"),
		filepath.Join(home, ".local", "bin", "dec-agent.exe"),
		filepath.Join(home, ".local", "bin", "dec-tray"),
		filepath.Join(home, ".local", "bin", "dec-tray.exe"),
		// 新一代 internal/diag 在 DEC_HOME 未设置时把认证诊断写到 ~/.dec/logs，
		// 它和旧版残留共用 ~/.dec，必须单独排除。
		filepath.Join(home, ".dec", "logs"),
	}
	for _, label := range []string{"com.dec.agent", "com.dec.tray"} {
		plist := filepath.Join(home, "Library", "LaunchAgents", label+".plist")
		out = append(out, plist)
		// 启动项里引用的程序同样属于新代理，不管它装在哪里。
		out = append(out, plistPaths(plist)...)
	}
	out = append(out, s.in.ExtraProtected...)
	for i := range out {
		out[i] = filepath.Clean(out[i])
	}
	return out
}

var plistStringRE = regexp.MustCompile(`<string>([^<]+)</string>`)

// plistPaths 取出启动项里出现的绝对路径。
func plistPaths(plist string) []string {
	data, err := os.ReadFile(plist)
	if err != nil {
		return nil
	}
	var out []string
	for _, m := range plistStringRE.FindAllSubmatch(data, -1) {
		v := strings.TrimSpace(string(m[1]))
		if filepath.IsAbs(v) {
			out = append(out, v)
		}
	}
	return out
}

// projects 是新代理登记与旧版登记的并集（需求 2.4）。
//
// 旧版登记写在 ~/.dec/config.yaml，这份文件本身也会被 H01 清掉，
// 所以必须在扫描阶段先读出来。只保留仍然存在的目录。
func (s *scanner) projects() []string {
	seen := map[string]bool{}
	var out []string
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" || !filepath.IsAbs(p) {
			return
		}
		p = filepath.Clean(p)
		if seen[p] {
			return
		}
		if info, err := os.Stat(p); err != nil || !info.IsDir() {
			return
		}
		seen[p] = true
		out = append(out, p)
	}
	for _, p := range s.in.Projects {
		add(p)
	}
	for _, p := range s.oldManagedProjects() {
		add(p)
	}
	sort.Strings(out)
	return out
}

func (s *scanner) oldManagedProjects() []string {
	path := filepath.Join(s.in.Home, ".dec", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cfg struct {
		ManagedProjects []struct {
			Root string `yaml:"root"`
		} `yaml:"managed_projects"`
	}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		s.warn(fmt.Sprintf("旧版配置 %s 解析失败，旧版登记的项目不在本次范围内：%v", path, err))
		return nil
	}
	out := make([]string, 0, len(cfg.ManagedProjects))
	for _, p := range cfg.ManagedProjects {
		out = append(out, p.Root)
	}
	return out
}

func (s *scanner) warn(msg string) { s.report.Warnings = append(s.report.Warnings, msg) }

func (s *scanner) skip(rule, path, reason string) {
	s.report.Skipped = append(s.report.Skipped, Skipped{Rule: rule, Path: path, Reason: reason})
}

// blocked 判断一个待删对象是否落在排除面上，返回原因。
func (s *scanner) blocked(path string) string {
	path = filepath.Clean(path)
	for _, p := range s.protected {
		if overlaps(path, p) {
			return "新代理自身的文件"
		}
	}
	if s.roots[path] {
		return "Dec 落点根目录本身，只清理其中的内容"
	}
	for p := range s.in.Expected.paths {
		if overlaps(p, path) {
			return "云端当前为本设备下发的落点"
		}
	}
	for p := range s.in.Expected.keys {
		if within(p, path) {
			return "包含新系统托管条目的共享配置"
		}
	}
	if hasNotice(path) {
		return "带「由 Dec 管理」说明行的新系统落点"
	}
	return ""
}

// addDelete 登记一个整项删除的对象。
func (s *scanner) addDelete(rule, path, project string) {
	info, err := os.Lstat(path)
	if err != nil {
		return
	}
	if reason := s.blocked(path); reason != "" {
		s.skip(rule, path, reason)
		return
	}
	r, _ := RuleByID(rule)
	s.report.Items = append(s.report.Items, Item{
		Rule: rule, Group: r.Group, Action: ActionDelete,
		Path: path, Project: project,
		Size: treeSize(path), IsDir: info.IsDir(),
	})
}

// addList 登记一个只列出的对象。
func (s *scanner) addList(rule, path, project string, entries ...string) {
	if _, err := os.Lstat(path); err != nil {
		return
	}
	r, _ := RuleByID(rule)
	s.report.Items = append(s.report.Items, Item{
		Rule: rule, Group: r.Group, Action: ActionList,
		Path: path, Project: project, Entries: entries,
	})
}

// addStrip 登记一个共享文件里的旧版条目。
func (s *scanner) addStrip(rule, path, project string, entries []string) {
	if len(entries) == 0 {
		return
	}
	info, err := os.Lstat(path)
	if err != nil {
		return
	}
	for _, p := range s.protected {
		if overlaps(path, p) {
			s.skip(rule, path, "新代理自身的文件")
			return
		}
	}
	r, _ := RuleByID(rule)
	s.report.Items = append(s.report.Items, Item{
		Rule: rule, Group: r.Group, Action: ActionStrip,
		Path: path, Project: project, Entries: entries, Size: info.Size(),
	})
}

func (s *scanner) scanUser() {
	home := s.in.Home
	decHome := filepath.Join(home, ".dec")

	// H01：~/.dec 下的子项。secrets 是旧版本机 secret 落地，归入凭据（需求 2.6）。
	for _, name := range readDirNames(decHome) {
		p := filepath.Join(decHome, name)
		if name == "secrets" {
			s.addList(RuleDecSecrets, p, "", "旧版本机 secret 落地，可能仍被引用")
			continue
		}
		s.addDelete(RuleDecHome, p, "")
	}
	// H02
	s.addDelete(RuleDecsHome, filepath.Join(home, ".decs"), "")
	// H03
	for _, name := range shellRCFiles {
		p := filepath.Join(home, name)
		s.addStrip(RuleShellRC, p, "", shellRCLines(p))
	}
	// H04、H05：只在 macOS 上有。
	if s.in.GOOS == "darwin" {
		for _, dir := range s.in.Applications {
			s.addDelete(RuleConsoleApp, filepath.Join(dir, "dec-console.app"), "")
		}
		lib := filepath.Join(home, "Library")
		for _, rel := range []string{
			"Application Support/com.shichao402.dec.console",
			"Application Support/dec-console",
			"Caches/com.shichao402.dec.console",
			"Preferences/com.shichao402.dec.console.plist",
		} {
			s.addDelete(RuleConsoleData, filepath.Join(lib, filepath.FromSlash(rel)), "")
		}
	}
	// H06
	for _, name := range oldBinaryNames {
		s.addDelete(RuleOldBinaries, filepath.Join(home, ".local", "bin", name), "")
	}
	for _, name := range []string{"dec-console", "dec-agent"} {
		s.addDelete(RuleOldBinaries, filepath.Join(s.in.SystemBin, name), "")
	}
	// H07
	for _, name := range []string{".claude-internal", ".codex-internal"} {
		s.addDelete(RuleInternalHomes, filepath.Join(home, name), "")
	}
	// H08、H09
	for _, root := range oldUserHostDirs {
		s.scanHostAssets(filepath.Join(home, filepath.FromSlash(root)), "", true)
	}
	// H10
	for _, rel := range oldUserMCPFiles {
		p := filepath.Join(home, filepath.FromSlash(rel))
		s.addStrip(RuleUserMCP, p, "", s.legacyMCPKeys(p, true))
	}
	// H11
	s.addList(RuleGitFragments, filepath.Join(home, ".config", "dec", "gitconfig.d"), "")
	if lines := gitIncludeLines(filepath.Join(home, ".gitconfig")); len(lines) > 0 {
		s.addList(RuleGitFragments, filepath.Join(home, ".gitconfig"), "", lines...)
	}
	// H12：只列出凭据名，钥匙串里的条目无法归档（需求 5.4）。
	if names := gcmNames(filepath.Join(decHome, "secrets")); len(names) > 0 {
		s.addList(RuleGCM, filepath.Join(decHome, "secrets"), "", names...)
	}
	// H13
	s.scanSSH()
}

// scanHostAssets 扫一个宿主根下的 skills、rules、commands。
func (s *scanner) scanHostAssets(root, project string, userPlane bool) {
	itemRule, linkRule := RuleUserHostItems, RuleBrokenLinks
	if project != "" {
		itemRule, linkRule = RuleProjectHost, RuleProjectHost
	}
	for _, dir := range assetDirs {
		base := filepath.Join(root, dir)
		for _, name := range readDirNames(base) {
			p := filepath.Join(base, name)
			oldBuiltin := userPlane && dir == "skills" && name == "dec"
			if strings.HasPrefix(name, "dec-") || oldBuiltin {
				s.addDelete(itemRule, p, project)
				continue
			}
			if brokenLink(p) {
				s.addDelete(linkRule, p, project)
			}
		}
	}
}

func (s *scanner) scanSSH() {
	ssh := filepath.Join(s.in.Home, ".ssh")
	var keys []string
	for _, name := range readDirNames(ssh) {
		if !strings.HasPrefix(name, "dec_") {
			continue
		}
		p := filepath.Join(ssh, name)
		if s.in.Expected.paths[p] {
			continue
		}
		keys = append(keys, p)
	}
	for _, p := range keys {
		s.addList(RuleSSH, p, "")
	}
	s.addList(RuleSSH, filepath.Join(ssh, "config.d", "dec.conf"), "")
	for _, name := range readDirNames(filepath.Join(ssh, "config.d")) {
		if strings.HasPrefix(name, "dec-project-") && strings.HasSuffix(name, ".conf") {
			s.addList(RuleSSH, filepath.Join(ssh, "config.d", name), "")
		}
	}
	if lines := sshIncludeLines(filepath.Join(ssh, "config")); len(lines) > 0 {
		s.addList(RuleSSH, filepath.Join(ssh, "config"), "", lines...)
	}
}

func (s *scanner) scanProject(project string) {
	// P01
	dotDec := filepath.Join(project, ".dec")
	for _, name := range readDirNames(dotDec) {
		s.addDelete(RuleProjectDec, filepath.Join(dotDec, name), project)
	}
	// P02
	for _, name := range []string{".claude-internal", ".codex-internal"} {
		s.addDelete(RuleProjectInternal, filepath.Join(project, name), project)
	}
	// P03
	for _, root := range oldProjectHostDirs {
		s.scanHostAssets(filepath.Join(project, root), project, false)
	}
	// P04
	for _, rel := range oldProjectMCPFiles {
		p := filepath.Join(project, filepath.FromSlash(rel))
		s.addStrip(RuleProjectMCP, p, project, s.legacyMCPKeys(p, false))
	}
	// P06
	secrets := filepath.Join(project, ".secrets")
	entries := []string{"可能仍被项目运行时读取"}
	entries = append(entries, gcmNames(secrets)...)
	s.addList(RuleProjectSecrets, secrets, project, entries...)
}

// legacyMCPKeys 返回一份 MCP 配置里的旧版条目名。
//
// 用户级的 dec 是旧版内置入口；dec- 前缀的键新旧两代都在用，扣掉云端期望的才算旧版。
// 新代理的 dec_vault 不匹配任何一条，天然排除。
func (s *scanner) legacyMCPKeys(path string, userPlane bool) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var names []string
	if strings.HasSuffix(path, ".toml") {
		names = tomlMCPNames(data)
	} else {
		names, err = jsonMCPNames(data)
		if err != nil {
			s.warn(fmt.Sprintf("%s 不是合法 JSON，跳过：%v", path, err))
			return nil
		}
	}
	expected := s.in.Expected.keys[filepath.Clean(path)]
	var out []string
	for _, n := range names {
		switch {
		case n == "dec" && userPlane:
		case strings.HasPrefix(n, "dec-") && !expected[n]:
		default:
			continue
		}
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

func jsonMCPNames(data []byte) ([]string, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, nil
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, err
	}
	var out []string
	for _, key := range []string{"mcpServers", "mcp_servers"} {
		servers, ok := root[key].(map[string]any)
		if !ok {
			continue
		}
		for name := range servers {
			out = append(out, name)
		}
	}
	return out, nil
}

// tomlMCPNames 取 [mcp_servers.<名字>] 及其子表的名字。
func tomlMCPNames(data []byte) []string {
	seen := map[string]bool{}
	var out []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		segs, ok := tomlHeader(strings.TrimSpace(sc.Text()))
		if !ok || len(segs) < 2 || segs[0] != "mcp_servers" || seen[segs[1]] {
			continue
		}
		seen[segs[1]] = true
		out = append(out, segs[1])
	}
	return out
}

// tomlHeader 解析表头 [a.b."c.d"]，数组表 [[...]] 不算。
func tomlHeader(line string) ([]string, bool) {
	if strings.HasPrefix(line, "[[") || !strings.HasPrefix(line, "[") {
		return nil, false
	}
	end := strings.LastIndex(line, "]")
	if end < 0 {
		return nil, false
	}
	body := line[1:end]
	var segs []string
	var cur strings.Builder
	quote := byte(0)
	for i := 0; i < len(body); i++ {
		c := body[i]
		switch {
		case quote != 0 && c == quote:
			quote = 0
		case quote != 0:
			cur.WriteByte(c)
		case c == '"' || c == '\'':
			quote = c
		case c == '.':
			segs = append(segs, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteByte(c)
		}
	}
	segs = append(segs, strings.TrimSpace(cur.String()))
	return segs, true
}

// shellRCLines 返回旧版安装脚本追加的行（需求：H03）。
func shellRCLines(path string) []string {
	return matchLines(path, func(line string) bool {
		t := strings.TrimSpace(line)
		if t == "# Dec" {
			return true
		}
		if strings.HasPrefix(t, "#") {
			return false
		}
		return strings.Contains(t, "/.dec/bin") || strings.Contains(t, "/.decs/bin")
	})
}

// gitIncludeLines 返回 ~/.gitconfig 里指向旧版 Git 片段的行。
func gitIncludeLines(path string) []string {
	return matchLines(path, func(line string) bool {
		return strings.Contains(filepath.ToSlash(line), "/dec/gitconfig.d/")
	})
}

// sshIncludeLines 返回 ~/.ssh/config 里引入旧版片段的 Include 行。
func sshIncludeLines(path string) []string {
	return matchLines(path, func(line string) bool {
		f := strings.Fields(line)
		if len(f) < 2 || !strings.EqualFold(f[0], "include") {
			return false
		}
		for _, v := range f[1:] {
			v = filepath.ToSlash(strings.Trim(v, `"'`))
			base := v[strings.LastIndex(v, "/")+1:]
			if strings.Contains(v, "config.d/") && (base == "dec.conf" || strings.HasPrefix(base, "dec-project-")) {
				return true
			}
		}
		return false
	})
}

// matchLines 返回命中的行，带行号，便于用户核对。
func matchLines(path string, match func(string) bool) []string {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		n++
		if match(sc.Text()) {
			out = append(out, fmt.Sprintf("%d: %s", n, strings.TrimSpace(sc.Text())))
		}
	}
	return out
}

// gcmNames 列出旧版落地的 GCM 凭据描述文件名（…/.gcm/<实例>.yaml）。
func gcmNames(root string) []string {
	var out []string
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		slash := filepath.ToSlash(p)
		if i := strings.LastIndex(slash, "/.gcm/"); i >= 0 {
			out = append(out, "GCM 凭据 "+slash[i+1:])
		}
		return nil
	})
	return out
}

// hasNotice 报告路径（文件或目录树）里是否有带「由 Dec 管理」说明行的文件。
// 不跟随软链接：链接指向的东西不归这里管。
func hasNotice(path string) bool {
	found := false
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil || found {
			return fs.SkipAll
		}
		if !d.Type().IsRegular() {
			return nil
		}
		if fileHasNotice(p) {
			found = true
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func fileHasNotice(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	head := make([]byte, 4096)
	n, err := f.Read(head)
	if err != nil && err != io.EOF {
		return false
	}
	return bytes.Contains(head[:n], []byte(notice.Text))
}

// treeSize 统计归档体积，不跟随软链接。
func treeSize(path string) int64 {
	var total int64
	_ = filepath.WalkDir(path, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				total += info.Size()
			}
		}
		return nil
	})
	return total
}

func brokenLink(p string) bool {
	info, err := os.Lstat(p)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	_, err = os.Stat(p)
	return err != nil
}

func readDirNames(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// within 报告 p 是否等于 root 或位于 root 之下。
func within(p, root string) bool {
	rel, err := filepath.Rel(root, p)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// overlaps 报告两条路径是否互相包含。
func overlaps(a, b string) bool { return within(a, b) || within(b, a) }
