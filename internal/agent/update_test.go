package agent

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shichao402/Dec/internal/release"
)

type updateFixture struct {
	t        *testing.T
	seed     string
	pub      string
	files    map[string][]byte
	manifest []byte
	sig      string
	srv      *httptest.Server
	stateDir string
	exe      string
	launched [][]string
}

func newUpdateFixture(t *testing.T, version, required string, bin []byte) *updateFixture {
	t.Helper()
	s := make([]byte, ed25519.SeedSize)
	for i := range s {
		s[i] = 7
	}
	f := &updateFixture{
		t:     t,
		seed:  base64.StdEncoding.EncodeToString(s),
		pub:   base64.StdEncoding.EncodeToString(ed25519.NewKeyFromSeed(s).Public().(ed25519.PublicKey)),
		files: map[string][]byte{"dec-agent-linux-amd64": bin},
	}
	f.setManifest(version, required, nil)
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/download/")
		switch name {
		case release.ManifestName:
			w.Write(f.manifest)
		case release.SignatureName:
			w.Write([]byte(f.sig))
		default:
			if b, ok := f.files[name]; ok {
				w.Write(b)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.srv.Close)
	f.stateDir = t.TempDir()
	binDir := t.TempDir()
	f.exe = filepath.Join(binDir, "dec-agent")
	if err := os.WriteFile(f.exe, []byte("old-agent"), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

// setManifest 生成并签名清单；mutate 可以在签名前改动产物描述。
func (f *updateFixture) setManifest(version, required string, mutate func(*release.File)) {
	var files []release.File
	for name, b := range f.files {
		sum := sha256.Sum256(b)
		rf := release.File{Name: name, OS: "linux", Arch: "amd64", SHA256: hex.EncodeToString(sum[:]), Size: int64(len(b))}
		if mutate != nil {
			mutate(&rf)
		}
		files = append(files, rf)
	}
	m := release.Manifest{Version: version, RequiredClient: required, Protocol: 1, Files: files}
	data, _ := json.Marshal(m)
	sig, err := release.Sign(f.seed, data)
	if err != nil {
		f.t.Fatal(err)
	}
	f.manifest, f.sig = data, sig
}

func (f *updateFixture) updater(version string) *Updater {
	return &Updater{
		ServerURL:  f.srv.URL,
		Version:    version,
		StateDir:   f.stateDir,
		Keys:       []string{f.pub},
		Executable: func() (string, error) { return f.exe, nil },
		Launch: func(bin string, args []string) error {
			f.launched = append(f.launched, append([]string{bin}, args...))
			return nil
		},
		GOOS:   "linux",
		GOARCH: "amd64",
	}
}

func TestUpdaterStagesAndLaunches(t *testing.T) {
	f := newUpdateFixture(t, "v2.1.48", "v2.1.48", []byte("new-agent"))
	started, err := f.updater("v2.1.47").CheckOnce(context.Background())
	if err != nil || !started {
		t.Fatalf("应拉起更新: started=%v err=%v", started, err)
	}
	if len(f.launched) != 1 || f.launched[0][1] != "update-apply" {
		t.Fatalf("更新进程参数不对: %v", f.launched)
	}
	if got, _ := os.ReadFile(UpdaterBinaryPath(f.stateDir)); string(got) != "old-agent" {
		t.Fatalf("更新进程应是当前可执行文件的拷贝，实际 %q", got)
	}
	var plan UpdatePlan
	data, err := os.ReadFile(updatePlanPath(f.stateDir))
	if err != nil || json.Unmarshal(data, &plan) != nil {
		t.Fatalf("读计划失败: %v", err)
	}
	if plan.From != "v2.1.47" || plan.Version != "v2.1.48" || len(plan.Items) != 1 {
		t.Fatalf("计划不对: %+v", plan)
	}
	if got, _ := os.ReadFile(plan.Items[0].Staged); string(got) != "new-agent" {
		t.Fatalf("暂存文件内容不对: %q", got)
	}
	if plan.Items[0].Target != f.exe {
		real, _ := filepath.EvalSymlinks(f.exe)
		if plan.Items[0].Target != real {
			t.Fatalf("替换目标不对: %s", plan.Items[0].Target)
		}
	}
}

func TestUpdaterSkips(t *testing.T) {
	cases := []struct {
		name, local, version, required string
		wantErr                        bool
	}{
		{"已满足要求版本", "v2.1.48", "v2.1.49", "v2.1.48", false},
		{"本地构建", "dev", "v2.1.48", "v2.1.48", false},
		{"回放旧清单不降级", "v2.1.50", "v2.1.49", "v2.1.51", true},
		{"清单低于自己的要求版本", "v2.1.40", "v2.1.45", "v2.1.48", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newUpdateFixture(t, c.version, c.required, []byte("new"))
			started, err := f.updater(c.local).CheckOnce(context.Background())
			if started || len(f.launched) != 0 {
				t.Fatal("不应拉起更新")
			}
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
		})
	}
}

func TestUpdaterRejectsBadSignature(t *testing.T) {
	f := newUpdateFixture(t, "v2.1.48", "v2.1.48", []byte("new"))
	f.manifest = []byte(strings.Replace(string(f.manifest), "v2.1.48", "v2.1.49", 1))
	_, err := f.updater("v2.1.47").CheckOnce(context.Background())
	if !errors.Is(err, release.ErrBadSignature) || len(f.launched) != 0 {
		t.Fatalf("篡改清单应拒绝，实际 %v", err)
	}
}

func TestUpdaterRejectsChecksumMismatch(t *testing.T) {
	f := newUpdateFixture(t, "v2.1.48", "v2.1.48", []byte("new"))
	f.setManifest("v2.1.48", "v2.1.48", func(rf *release.File) {
		rf.SHA256 = strings.Repeat("0", 64)
	})
	_, err := f.updater("v2.1.47").CheckOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "sha256") || len(f.launched) != 0 {
		t.Fatalf("sha256 不符应拒绝，实际 %v", err)
	}
	matches, _ := filepath.Glob(filepath.Join(UpdateDir(f.stateDir), "staged", "*", "*"))
	if len(matches) != 0 {
		t.Fatalf("校验失败不应留下文件: %v", matches)
	}
}

func TestUpdaterBacksOffAfterFailure(t *testing.T) {
	f := newUpdateFixture(t, "v2.1.48", "v2.1.48", []byte("new"))
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	res, _ := json.Marshal(UpdateResult{From: "v2.1.47", Version: "v2.1.48", OK: false, At: now.Add(-time.Hour)})
	os.MkdirAll(UpdateDir(f.stateDir), 0o700)
	os.WriteFile(updateResultPath(f.stateDir), res, 0o600)

	u := f.updater("v2.1.47")
	u.Now = func() time.Time { return now }
	if started, _ := u.CheckOnce(context.Background()); started {
		t.Fatal("同一版本刚失败过，冷却期内不应再试")
	}
	u.Now = func() time.Time { return now.Add(updateFailBackoff) }
	if started, err := u.CheckOnce(context.Background()); !started {
		t.Fatalf("冷却期过后应重试: %v", err)
	}
}

type fakeControl struct {
	stops, starts int
	stopErr       error
}

func (c *fakeControl) Stop([]UpdateItem) error  { c.stops++; return c.stopErr }
func (c *fakeControl) Start([]UpdateItem) error { c.starts++; return nil }

func writePlan(t *testing.T) (planPath, target, stateDir string) {
	t.Helper()
	stateDir = t.TempDir()
	dir := t.TempDir()
	target = filepath.Join(dir, "dec-agent")
	staged := filepath.Join(dir, "staged-agent")
	os.WriteFile(target, []byte("old"), 0o755)
	os.WriteFile(staged, []byte("new"), 0o755)
	plan := UpdatePlan{From: "v1.0.0", Version: "v1.0.1", StateDir: stateDir,
		Items: []UpdateItem{{Kind: "agent", Target: target, Staged: staged}}}
	data, _ := json.Marshal(plan)
	planPath = updatePlanPath(stateDir)
	os.MkdirAll(filepath.Dir(planPath), 0o700)
	os.WriteFile(planPath, data, 0o600)
	return
}

func readResultFile(t *testing.T, stateDir string) UpdateResult {
	t.Helper()
	var r UpdateResult
	data, err := os.ReadFile(updateResultPath(stateDir))
	if err != nil || json.Unmarshal(data, &r) != nil {
		t.Fatalf("读结果失败: %v", err)
	}
	return r
}

func TestApplyUpdateSuccess(t *testing.T) {
	planPath, target, stateDir := writePlan(t)
	ctl := &fakeControl{}
	err := ApplyUpdate(context.Background(), ApplyOptions{
		PlanPath: planPath, Control: ctl, ReplaceTimeout: time.Second,
		Health: func(context.Context, *UpdatePlan) error { return nil },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "new" {
		t.Fatalf("应换成新文件，实际 %q", got)
	}
	if _, err := os.Stat(target + ".old"); !os.IsNotExist(err) {
		t.Fatal("成功后应清掉 .old")
	}
	if r := readResultFile(t, stateDir); !r.OK {
		t.Fatalf("结果应为成功: %+v", r)
	}
	if ctl.stops != 1 || ctl.starts != 1 {
		t.Fatalf("停起次数不对: %+v", ctl)
	}
	if _, err := os.Stat(updateLockPath(stateDir)); !os.IsNotExist(err) {
		t.Fatal("结束后应释放锁")
	}
}

func TestApplyUpdateRollsBackOnUnhealthy(t *testing.T) {
	planPath, target, stateDir := writePlan(t)
	ctl := &fakeControl{}
	err := ApplyUpdate(context.Background(), ApplyOptions{
		PlanPath: planPath, Control: ctl, ReplaceTimeout: time.Second,
		Health: func(context.Context, *UpdatePlan) error { return errors.New("起不来") },
	})
	if err == nil {
		t.Fatal("健康检查失败应返回错误")
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Fatalf("应回滚到旧文件，实际 %q", got)
	}
	r := readResultFile(t, stateDir)
	if r.OK || !strings.Contains(r.Reason, "起不来") {
		t.Fatalf("结果应记录失败原因: %+v", r)
	}
	if ctl.starts < 2 {
		t.Fatalf("回滚后应再次启动服务: %+v", ctl)
	}
}

func TestApplyUpdateStopFailureKeepsOld(t *testing.T) {
	planPath, target, _ := writePlan(t)
	ctl := &fakeControl{stopErr: errors.New("停不下来")}
	err := ApplyUpdate(context.Background(), ApplyOptions{
		PlanPath: planPath, Control: ctl, ReplaceTimeout: time.Second,
		Health: func(context.Context, *UpdatePlan) error { return nil },
	})
	if err == nil {
		t.Fatal("停服务失败应返回错误")
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Fatalf("停不下来时不应换文件，实际 %q", got)
	}
}

func TestApplyUpdateBusyLock(t *testing.T) {
	planPath, target, stateDir := writePlan(t)
	os.WriteFile(updateLockPath(stateDir), []byte("1"), 0o600)
	err := ApplyUpdate(context.Background(), ApplyOptions{
		PlanPath: planPath, Control: &fakeControl{}, ReplaceTimeout: time.Second,
		Health: func(context.Context, *UpdatePlan) error { return nil },
	})
	if !errors.Is(err, ErrUpdateBusy) {
		t.Fatalf("锁被占用应返回 ErrUpdateBusy，实际 %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "old" {
		t.Fatal("锁忙时不应动文件")
	}

	// 过期锁可以被接管。
	old := time.Now().Add(-updateLockStale - time.Minute)
	os.Chtimes(updateLockPath(stateDir), old, old)
	if err := ApplyUpdate(context.Background(), ApplyOptions{
		PlanPath: planPath, Control: &fakeControl{}, ReplaceTimeout: time.Second,
		Health: func(context.Context, *UpdatePlan) error { return nil },
	}); err != nil {
		t.Fatalf("过期锁应被接管: %v", err)
	}
}
