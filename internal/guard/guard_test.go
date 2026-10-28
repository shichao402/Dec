// Package guard 用测试守住断代边界（需求 3.5、4.5）。
//
// 断代之后仓库里只剩新一代代码，但「删干净」这件事很难靠人肉保证：
// 某次改动可能把一个旧包重新引进来，也可能有人在注释里写一句
// 「后续阶段再接上」把没做完的事推给一个不存在的任务。这两类问题
// 都不会让编译失败，只会让下一个人误以为旧架构还在服务。
//
// 因此这里用几个测试把它们变成会失败的构建：
//
//  1. TestNoLegacyDependencies 解析 dec-cloud 与 dec-agent 的依赖图，
//     出现已删除包的路径前缀即失败。
//  2. TestLegacyPackagesGone 确认删除清单里的目录确实不在仓库里。
//  3. TestOnlyNewGenerationCommands 确认 cmd/ 下只剩两个入口。
//  4. TestNoOrphanDeferralComments 扫描源码，出现没有对应任务的推迟注释即失败。
package guard

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/sysproc"
	"gopkg.in/yaml.v3"
)

// modulePath 是本仓库的模块路径。
const modulePath = "github.com/shichao402/Dec"

// legacyPackages 是断代时删除的包（需求 3.2、3.3）。
//
// 用完整包路径而不是裸目录名：`internal/assets` 与保留的
// `internal/cloud/assets` 只差一个前缀，裸名会误伤。
// 新增删除项时追加到这里。
var legacyPackages = []string{
	modulePath + "/cmd/dec-server",
	modulePath + "/cmd/dec-exec",
	modulePath + "/cmd/dec-host-setup",
	modulePath + "/cmd/dec-registry",
	modulePath + "/internal/agenttools",
	modulePath + "/internal/app",
	modulePath + "/internal/assets",
	modulePath + "/internal/compat",
	modulePath + "/internal/contribute",
	modulePath + "/internal/ide",
	modulePath + "/internal/install",
	modulePath + "/internal/prune",
	modulePath + "/internal/publish",
	modulePath + "/internal/registry",
	modulePath + "/internal/secrets/handler",
	modulePath + "/internal/service",
	modulePath + "/internal/serviceapi",
	modulePath + "/internal/servicehost",
	modulePath + "/internal/update",
	modulePath + "/internal/vars",
	modulePath + "/internal/version",
	modulePath + "/tools/credtool",
	modulePath + "/tools/deletetool",
	modulePath + "/tools/migrate-bw-flat",
	modulePath + "/schema/gen/go/service/v1",
}

// newGenCommands 是断代后仅剩的入口（需求 3.4、18.12）。
//
// dec-emergency 同样是新一代程序：需求 18.12 要求服务器本机保留
// 吊销设备与签发证书的管理命令，作为 Bitwarden 也不可用时的最后手段。
// 它与旧架构无关，不是断代时该清掉的残留，因此列在这里。
var newGenCommands = []string{
	"./cmd/dec-cloud",
	"./cmd/dec-agent",
	"./cmd/dec-emergency",
}

// newGenCommandNames 是 newGenCommands 的目录名形式，供目录比对使用。
//
// 由 newGenCommands 推导而不是另写一份：两处清单分开维护时，
// 加了一个入口却漏改另一处，会让这个守卫测试自己变成错误的来源。
func newGenCommandNames() map[string]bool {
	names := make(map[string]bool, len(newGenCommands))
	for _, cmd := range newGenCommands {
		names[strings.TrimPrefix(cmd, "./cmd/")] = true
	}
	return names
}

// TestNoLegacyDependencies 守住依赖图里不出现已删除的包（需求 3.5）。
//
// 用 go list 的真实输出而不是读源码里的 import 字面量：间接依赖、
// 只在测试里出现的依赖都能被覆盖到。
func TestNoLegacyDependencies(t *testing.T) {
	root := repoRoot(t)
	args := append([]string{"list", "-deps"}, newGenCommands...)
	cmd := sysproc.Command("go", args...)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			t.Fatalf("go list 执行失败: %v\n%s", err, ee.Stderr)
		}
		t.Fatalf("go list 执行失败: %v", err)
	}

	var offenders []string
	for _, line := range strings.Split(string(out), "\n") {
		pkg := strings.TrimSpace(line)
		if pkg == "" {
			continue
		}
		if legacy := matchLegacy(pkg); legacy != "" {
			offenders = append(offenders, pkg+"  ← "+legacy)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf("新一代二进制依赖了断代时已删除的包，请改用新包，或把该包从删除清单里撤出：\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// matchLegacy 报告 pkg 是否落在某个已删除包之下，返回命中的前缀。
//
// 只认「完全相同」或「前缀 + 斜杠」：`internal/service` 被删除，
// 但 `internal/servicefoo` 是一个合法的新包名，不该被误判。
func matchLegacy(pkg string) string {
	for _, legacy := range legacyPackages {
		if pkg == legacy || strings.HasPrefix(pkg, legacy+"/") {
			return legacy
		}
	}
	return ""
}

// TestLegacyPackagesGone 确认删除清单里的目录确实不在仓库里。
//
// 依赖图测试只能证明「没有被引用」，一个没人引用但目录还在的旧包照样会被
// 下一个人翻出来当参考。这里直接看目录在不在。
func TestLegacyPackagesGone(t *testing.T) {
	root := repoRoot(t)
	var leftovers []string
	for _, legacy := range legacyPackages {
		rel := strings.TrimPrefix(legacy, modulePath+"/")
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err == nil {
			leftovers = append(leftovers, rel)
		}
	}
	if len(leftovers) > 0 {
		t.Fatalf("断代时已删除的包目录仍在仓库里：\n  %s", strings.Join(leftovers, "\n  "))
	}
}

// TestOnlyNewGenerationCommands 守住 cmd/ 下只剩新一代入口（需求 3.2、3.4、18.12）。
func TestOnlyNewGenerationCommands(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatalf("读 cmd 目录失败: %v", err)
	}
	want := newGenCommandNames()
	var got []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		got = append(got, e.Name())
		if !want[e.Name()] {
			t.Errorf("cmd/ 下出现了非新一代入口: %s", e.Name())
		}
	}
	if len(got) != len(want) {
		t.Fatalf("cmd/ 下应只有 %v，实际 %v", sortedNames(want), got)
	}
}

// sortedNames 返回清单的稳定顺序，仅用于失败时的可读输出。
func sortedNames(set map[string]bool) []string {
	names := make([]string, 0, len(set))
	for name := range set {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// deferralMarkers 是没有主的推迟注释特征。
//
// 需求 4.5 要求：代码里不得出现「后续阶段再接上」这类推迟，除非它对应
// 一个真实任务并写进需求文档的「不在本次范围」。断代后这类注释最容易
// 藏在旧包的残留注释里，所以连同整个仓库一起扫。
var deferralMarkers = []string{
	"后续阶段再接上",
	"后续阶段接入",
	"后续阶段实现",
	"以后再接上",
	"以后再实现",
	"留到后续阶段",
	"暂未实现，后续",
	"TODO(后续阶段)",
	"M5 阶段",
	"等 M5",
	"下一阶段接上",
}

// skipDirs 是不属于本仓库源码的目录。
var skipDirs = map[string]bool{
	".git":         true,
	".cache":       true,
	"node_modules": true,
	"vendor":       true,
	"target":       true,
	"dist":         true,
	"third_party":  true,
}

// TestNoOrphanDeferralComments 守住「没有对应任务的推迟注释」（需求 4.5）。
func TestNoOrphanDeferralComments(t *testing.T) {
	root := repoRoot(t)
	var offenders []string

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if skipDirs[entry.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		// 计划与需求文档正是「推迟」的登记处，本守卫自身也含被检测的字面量。
		if strings.HasPrefix(rel, ".agent/") || rel == "internal/guard/guard_test.go" {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for i, line := range strings.Split(string(data), "\n") {
			for _, marker := range deferralMarkers {
				if strings.Contains(line, marker) {
					offenders = append(offenders,
						rel+":"+strconv.Itoa(i+1)+"  "+strings.TrimSpace(line))
					break
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历仓库失败: %v", err)
	}

	if len(offenders) > 0 {
		t.Fatalf("发现没有对应任务的推迟注释。要么现在做完，要么把这件事写进需求文档的「不在本次范围」：\n  %s",
			strings.Join(offenders, "\n  "))
	}
}

// TestProdAcceptanceStaysReadOnly 守住线上验收脚本的只读承诺（需求 4.2）。
//
// 这个脚本在生产服务器上执行，权限比本机模式大得多。一旦有人图省事
// 加一条写操作（例如「顺手清理一下残留」），下次跑验收就可能删掉真实资产。
// 因此把「只允许 hello 这一处写」变成会失败的检查，而不是靠人记得。
func TestProdAcceptanceStaysReadOnly(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "scripts", "acceptance-prod.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v（线上验收是需求 4.2 的交付物，不能缺失）", path, err)
	}
	content := string(data)

	// 写类方法一个都不许被调用：出现 call <方法名> 即意味着脚本会改生产数据。
	// hello 不在这个名单里——它只更新「这台机器最后一次在线时间」。
	// 注意这里检查的是 call 形式而不是裸名字：脚本会把这些方法名放进
	// 「无证书必须被拒」的循环里逐个断言 unauthorized，那是在验证它们调不动，
	// 与「调用它们」是相反的两件事。
	forbidden := []string{
		"write_asset", "delete_asset", "delete_bundle", "put_secret",
		"delete_secret", "put_project", "delete_project",
		"set_subscriptions", "revoke_device", "rotate_root_key",
		"set_device_kind", "read_secret", "initialize", "recover", "enroll",
	}
	for _, m := range forbidden {
		if strings.Contains(content, "call "+m+" ") || strings.Contains(content, "$(call "+m+" ") {
			t.Errorf("线上验收脚本调用了写类方法 %q，它必须在生产上保持只读", m)
		}
	}

	// 只读承诺得写进脚本本身，跑的人一眼能看到边界。
	if !strings.Contains(content, "只读") {
		t.Error("线上验收脚本应明确写出「只读」承诺，否则跑的人无法判断边界")
	}
	// 残留检查是这个脚本存在的理由之一：没有它，验收写完东西没人发现。
	if !strings.Contains(content, "dec-acceptance") {
		t.Error("线上验收脚本应检查 dec-acceptance 前缀的残留 bundle")
	}
}

// TestTestEnvStaysIsolated 守住线上验收的隔离约束。
//
// 跑完整链路要登记设备、写根密钥冷备、写测试 bundle。只要有一个变量
// 指到生产目录或生产端口，这条链路就会在真实资产库里留下测试数据，
// 而这种事故在跑之前完全看不出来。所以把隔离条件写成会失败的检查。
func TestTestEnvStaysIsolated(t *testing.T) {
	root := repoRoot(t)
	path := filepath.Join(root, "deploy", "test-e2e.sh")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读 %s 失败: %v（线上验收是需求 4.2 的交付物）", path, err)
	}
	content := string(data)

	// 隔离的四个维度必须都由脚本自己声明，不能靠外部恰好没配。
	for _, want := range []string{
		"/opt/dec-cloud-test", // 测试根目录，与生产 /var/lib/dec-cloud 分开
		"-listen 127.0.0.1",   // 只监听回环，绝不对外
		"-accept-any",         // 自签证书：测试实例没有域名，也不该申请证书
		"dec-cloud-$SLOT",     // 独立单元名
		"dec-agent-$SLOT",     // 独立单元名
	} {
		if !strings.Contains(content, want) {
			t.Errorf("test-e2e.sh 缺少隔离要素 %q", want)
		}
	}

	// 生产路径只能出现在「反例拦截」与「事后核对」里，不能作为默认值。
	// 这里检查的是默认值赋值：DATA_DIR/KEY_DIR 不能指向生产。
	for _, bad := range []string{
		`DATA_DIR="/var/lib/dec-cloud`,
		`KEY_DIR="/etc/dec-cloud`,
	} {
		if strings.Contains(content, bad) {
			t.Errorf("test-e2e.sh 把测试目录指向了生产：%s", bad)
		}
	}

	// 443 是生产端口，测试实例用它就会把生产顶掉。
	if strings.Contains(content, `SERVER_PORT="443"`) {
		t.Error("test-e2e.sh 的测试实例不能使用 443")
	}

	// 必须核对「生产库没有测试残留」：隔离漏了只有这一步能发现。
	if !strings.Contains(content, "/var/lib/dec-cloud/ssot/assets") {
		t.Error("test-e2e.sh 必须核对生产库没有测试残留，否则隔离失效时无人发现")
	}
}

// TestDeletingTagIsNotRelease 守住「删 tag 不等于发版」。
//
// 删除 tag 同样会产生 push 事件，而 github.ref 仍是 refs/tags/<那个 tag>。
// 只按前缀判断的话，清理一个 v1.4.1 就会被当成「发布 v1.4.1」，
// 把当年的旧二进制装到生产机上——清理历史本来是最安全的操作，
// 在这个漏洞下反而成了最危险的。production environment 的分支策略
// 同样只认 v* 前缀，拦不住这种情况，所以必须在 job 上显式排除。
//
// 三个 job 都要排除：任意一个漏掉，删除时都会真的跑起来。
func TestDeletingTagIsNotRelease(t *testing.T) {
	root := repoRoot(t)
	wfPath := filepath.Join(root, ".github", "workflows", "release.yml")
	wf, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", wfPath, err)
	}

	var doc struct {
		Jobs map[string]struct {
			If string `yaml:"if"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(wf, &doc); err != nil {
		t.Fatalf("解析 %s 失败: %v", wfPath, err)
	}
	if len(doc.Jobs) == 0 {
		t.Fatal("release.yml 里没有 job，解析可能失败")
	}
	for name, job := range doc.Jobs {
		if !strings.Contains(job.If, "!github.event.deleted") {
			t.Errorf("job %s 的 if 没有排除删除事件，删 tag 会被当成发版（当前: %q）",
				name, job.If)
		}
	}
}

// TestProductionDeployOnlyThroughCI 守住「生产只能经 CI 部署」这条约定。
//
// 本地把二进制推到生产会绕开三道闸门：tag 与 commit 的真实性核对、发布清单
// 一致性校验、健康检查失败后的自动回滚。绕过去之后，生产上跑的东西与
// GitHub 上的 tag 之间没有可验证的对应关系，出事时无法回答「这是哪个版本」。
//
// 所以这条约定不能只写在文档里：仓库里一旦出现「本地直连生产」的部署脚本，
// 就应该让构建失败。
func TestProductionDeployOnlyThroughCI(t *testing.T) {
	root := repoRoot(t)

	// 部署入口必须由 CI 经 SSH 调用，且真实性与回滚必须在同一条链路里。
	wfPath := filepath.Join(root, ".github", "workflows", "release.yml")
	wf, err := os.ReadFile(wfPath)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", wfPath, err)
	}
	workflow := string(wf)
	for _, want := range []string{
		"refs/tags/v",               // 只有 tag 能触发部署
		"StrictHostKeyChecking=yes", // 不接受未知 host key
		"payload.tar",               // 产物走 stdin，不给参数注入留位置
		"!github.event.deleted",     // 删 tag 不是发布，见下面 TestDeletingTagIsNotRelease
	} {
		if !strings.Contains(workflow, want) {
			t.Errorf("release.yml 缺少 %q，CI 部署链路不完整", want)
		}
	}

	// 服务器侧必须核对 tag 真实性；这是部署私钥泄露时唯一的防线。
	entryPath := filepath.Join(root, "deploy", "dec-cloud-deploy.sh")
	entry, err := os.ReadFile(entryPath)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", entryPath, err)
	}
	for _, want := range []string{
		"verify_github_release", // 向 GitHub 核对 tag 与 commit
		"github-token",          // 用只读 token 核对
	} {
		if !strings.Contains(string(entry), want) {
			t.Errorf("dec-cloud-deploy.sh 缺少 %q，无法核对部署包真实性", want)
		}
	}

	// 可选运行参数必须走 EnvironmentFile，不能硬编码进 ExecStart。
	//
	// 硬编码的后果不是「不能用」，而是「改一次配置要重跑 provision.sh」——
	// 那是重建机器用的脚本，副作用远大于改一个地址。这里守住两条：
	//   1. 单元里有 EnvironmentFile 且允许文件缺失（行首 -）；
	//   2. ExecStart 通过 $DEC_EXTRA_ARGS 承接可选参数。
	provPath := filepath.Join(root, "deploy", "provision.sh")
	prov, err := os.ReadFile(provPath)
	if err != nil {
		t.Fatalf("读 %s 失败: %v", provPath, err)
	}
	for _, want := range []string{
		"EnvironmentFile=-$ENV_FILE", // 允许文件缺失，未配置时照常启动
		`\$DEC_EXTRA_ARGS`,           // ExecStart 里保留展开位置
		`if [ ! -f "$ENV_FILE" ]`,    // 已有配置不被重跑覆盖
	} {
		if !strings.Contains(string(prov), want) {
			t.Errorf("provision.sh 缺少 %q，可选参数无法在不重跑 provisioning 的情况下调整", want)
		}
	}
	// 反向：ExecStart 里不该直接出现这些可选参数名。
	for _, forbidden := range []string{
		"-backup-url $", "-audit-url $",
	} {
		if strings.Contains(string(prov), forbidden) {
			t.Errorf("provision.sh 的 ExecStart 硬编码了可选参数 %q；"+
				"它们应通过 $DEC_EXTRA_ARGS 由 EnvironmentFile 提供", forbidden)
		}
	}

	// 部署脚本从 ExecStart 推导 listen 时必须只认第一个 -listen。
	//
	// ExecStart 现在末尾可能带 $DEC_EXTRA_ARGS，展开后参数变多。若用贪婪匹配，
	// 某个额外参数值里恰好出现 "-listen" 就会取到错误地址，健康检查随即误判、
	// 触发回滚。
	if !strings.Contains(string(entry), `[[:space:]]-listen[ =]`) {
		t.Error("dec-cloud-deploy.sh 提取 -listen 时未锚定空白边界，" +
			"额外参数值里出现 -listen 会误匹配")
	}

	// 推送目标的主机密钥必须写死并显式传参，不能靠运行时 ssh-keyscan。
	//
	// 首次连接就去抓公钥等于不做校验（TOFU）：攻击者只要在那一次抢先应答
	// 就能冒充 GitHub，冷备与审计仓库随后会把明文历史推给对手。写死的代价
	// 是换钥时要改脚本，那是极低频且可观测的事件。
	for _, want := range []string{
		"[ssh.github.com]:443 ssh-ed25519", // 443 端口的公钥条目
		"github.com ssh-ed25519",           // 22 端口的公钥条目
		"-known-hosts $KNOWN_HOSTS_FILE",   // 由单元显式传参，不依赖环境变量隐式传导
	} {
		if !strings.Contains(string(prov), want) {
			t.Errorf("provision.sh 缺少 %q，推送目标主机密钥校验可能被绕过", want)
		}
	}
	// 反向检查要排除注释行：脚本注释里正是在解释为什么不用 ssh-keyscan。
	// 注意只看行首为 # 的行，行内 # 不做处理（shell 里行内 # 未必是注释）。
	var provCode []string
	for _, line := range strings.Split(string(prov), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		provCode = append(provCode, line)
	}
	if strings.Contains(strings.Join(provCode, "\n"), "ssh-keyscan") {
		t.Error("provision.sh 使用了 ssh-keyscan；首次抓取公钥等于不做校验")
	}

	// 反向检查：scripts/ 与 Makefile 里不该出现直连生产的部署路径。
	// 验收脚本可以 SSH 到生产做只读检查，所以只拦「上传并替换二进制」这类动作。
	uploadPatterns := []string{
		"scp .*dec-cloud.*dec.firoyang.com",
		"rsync .*dec-cloud.*dec.firoyang.com",
		"install -m 755 .*dec-cloud /opt/dec-cloud",
	}
	scriptDirs := []string{filepath.Join(root, "scripts"), filepath.Join(root, "deploy")}
	for _, dir := range scriptDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".sh") {
				continue
			}
			// 部署入口本身就是要被上传到服务器的文件，不算本地部署。
			if e.Name() == "dec-cloud-deploy.sh" || e.Name() == "provision.sh" {
				continue
			}
			body, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			for _, pat := range uploadPatterns {
				if regexp.MustCompile(pat).Match(body) {
					t.Errorf("%s 出现疑似「本地部署到生产」的写法（匹配 %q）；生产只能由 CI 部署", e.Name(), pat)
				}
			}
		}
	}
}

// TestLegacyFinalTagExists 守住断代前的存档标签（需求 3.1）。
//
// 旧代码只通过 git 历史保留，`legacy-final` 是那个存档点。标签丢了，
// 「想知道上一代长什么样」就只剩翻 commit 猜，所以把它变成会失败的检查。
func TestLegacyFinalTagExists(t *testing.T) {
	root := repoRoot(t)
	cmd := sysproc.Command("git", "tag", "--list")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git tag 执行失败: %v", err)
	}
	all := strings.Fields(string(out))

	// 先区分「没拉 tags」和「标签丢了」。浅克隆或 checkout 没带 fetch-tags 时
	// 一个 tag 都看不到，这时候报「标签丢失」会把人引向错误的修复方向——
	// 实际要改的是 CI 的 checkout 配置。
	if len(all) == 0 {
		t.Skip("这个检出没有任何 tag（浅克隆或未拉取 tags），无法判断 legacy-final 是否存在；CI 需 fetch-tags: true")
	}

	for _, name := range all {
		if name == "legacy-final" {
			return
		}
	}
	t.Fatalf("缺少 legacy-final 标签，旧代码没有存档点；应执行 git tag legacy-final <断代前提交>")
}

// TestMakefileOnlyBuildsNewGeneration 守住 Makefile 只面向新一代二进制（需求 3.4）。
//
// 断代后 Makefile 里如果还留着 `go build ./cmd/dec-server`，`make build`
// 会直接失败，但更糟的情况是它指向一个已经改名成旧路径的新包，静默构建出
// 没人用的产物。这里直接检查 Makefile 里出现的 cmd/ 路径。
func TestMakefileOnlyBuildsNewGeneration(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "Makefile"))
	if err != nil {
		t.Fatalf("读 Makefile 失败: %v", err)
	}
	content := string(data)
	for _, legacy := range legacyPackages {
		if !strings.HasPrefix(legacy, modulePath+"/cmd/") {
			continue
		}
		name := strings.TrimPrefix(legacy, modulePath+"/")
		if strings.Contains(content, "./"+name) {
			t.Errorf("Makefile 仍在构建已删除的入口: %s", name)
		}
	}
	for _, want := range []string{"./cmd/dec-cloud", "./cmd/dec-agent"} {
		if !strings.Contains(content, want) {
			t.Errorf("Makefile 缺少新一代入口: %s", want)
		}
	}
}

// requiredArtifacts 是发布产物里客户端必须有的平台组合（需求 3.7）。
//
// 断代不该改变产物名：设备端的自更新按 `dec-agent-<os>-<arch>` 拼名字，
// 少一个平台就等于那批设备再也升不上来。
var requiredArtifacts = []string{
	"dec-agent-darwin/amd64",
	"dec-agent-darwin/arm64",
	"dec-agent-linux/amd64",
	"dec-agent-linux/arm64",
	"dec-agent-windows/amd64",
	"dec-agent-windows/arm64",
	"dec-cloud-linux/amd64",
	"dec-cloud-linux/arm64",
}

// TestReleaseArtifactNamesUnchanged 守住 CI 产物名不变（需求 3.7）。
//
// 直接解析 release.yml 里的构建循环：产物名是在 shell 里拼出来的，
// 光看 yml 结构看不出最终名字，所以检查拼名字的那几条语句是否还在。
func TestReleaseArtifactNamesUnchanged(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("读 release.yml 失败: %v", err)
	}
	content := string(data)
	checks := []struct{ why, needle string }{
		{"客户端产物名前缀", `out="dist/dec-agent-$os-$arch"`},
		{"Windows 加 .exe 后缀", `out="$out.exe"`},
		{"服务端产物名", `-o "dist/dec-cloud-linux-$arch"`},
		{"客户端平台组合", "darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64 windows/arm64"},
		// agent-tray-access 需求 7.3：托盘与代理同一 tag 构建，并进入下载通道。
		{"Windows 托盘产物", "dist/dec-tray-windows-amd64.exe"},
		{"托盘版本取自 tag", "DEC_TRAY_VERSION: ${{ steps.meta.outputs.version }}"},
		{"发布清单包含托盘", `"dec-tray-"`},
		{"部署包带上托盘", "cp dist/dec-tray-* payload/download/"},
		// 需求 7.3 要求下载通道的 macOS 产物必须配套托盘；托盘链接 AppKit，
		// 只能在 macOS 上构建。这条例外必须被限制住，否则会有人顺手把其他
		// 产物挪到 macOS runner 上，打穿需求 21.1 的额度约束。
		{"macOS 托盘产物名", "dist/dec-tray-darwin-arm64"},
		{"macOS 托盘另一个架构", "dist/dec-tray-darwin-amd64"},
	}
	for _, c := range checks {
		if !strings.Contains(content, c.needle) {
			t.Errorf("release.yml 缺少%s，实际写的是 %q", c.why, c.needle)
		}
	}
}

// TestMacOSRunnerOnlyForTray 守住「macOS runner 只用于托盘」这条边界。
//
// 需求 21.1 规定除托盘外的产物都在 Linux runner 上构建。代码仓库是公开的，
// Actions 分钟数不受私有仓库额度限制，但 macOS runner 仍按 10 倍计费且排队更久，
// 唯一无法回避的例外是托盘：它链接 AppKit，Linux 上没有 macOS SDK。这里检查
// release.yml 里每一个使用 macOS runner 的 job 都只构建托盘，防止例外被扩大。
func TestMacOSRunnerOnlyForTray(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("读 release.yml 失败: %v", err)
	}

	// 按 job 切分：行首两空格缩进且以冒号结尾的是 job 名。
	jobs := map[string][]string{}
	jobName := ""
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		isJobHead := strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   ") &&
			strings.HasSuffix(trimmed, ":") && !strings.Contains(trimmed, " ")
		if isJobHead {
			jobName = strings.TrimSuffix(trimmed, ":")
			jobs[jobName] = nil
			continue
		}
		if jobName != "" {
			jobs[jobName] = append(jobs[jobName], line)
		}
	}

	usedMacOS := false
	for name, lines := range jobs {
		// 注释里提到 dec-agent 是正常的（说明例外范围），只检查真正执行的语句。
		var code []string
		for _, line := range lines {
			if t := strings.TrimSpace(line); strings.HasPrefix(t, "#") {
				continue
			}
			code = append(code, line)
		}
		body := strings.Join(code, "\n")
		if !strings.Contains(body, "runs-on: macos") {
			continue
		}
		usedMacOS = true
		// 托盘之外的产物出现在 macOS runner 上就是违规。
		for _, forbidden := range []string{"dec-cloud", "dec-agent"} {
			if strings.Contains(body, forbidden) {
				t.Errorf("job %q 使用了 macOS runner 却在构建 %s；"+
					"需求 21.1 要求全部产物在 Linux runner 上交叉编译，"+
					"macOS runner 只允许用于构建托盘", name, forbidden)
			}
		}
		if !strings.Contains(body, "dec-tray") {
			t.Errorf("job %q 使用了 macOS runner 但没有构建托盘，"+
				"这不符合唯一的例外场景", name)
		}
	}
	if !usedMacOS {
		t.Log("当前没有 job 使用 macOS runner；若托盘已能跨平台构建，这条测试可以删除")
	}
}

// repoRoot 向上找 go.mod 作为仓库根。
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录失败: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("未找到 go.mod")
		}
		dir = parent
	}
}

// TestServerCAPrivateKeyStaysOutOfSnapshots 守住"服务端 CA 私钥只在本机与 Bitwarden"（ADR 0052）。
//
// 部署快照与回滚都碰数据目录，最容易在这里多出一份明文：
// 快照带着 ca.key，回滚时就会把旧 CA 写回来，新登记的设备随之全部失效。
// 两处排除规则只要少一处，这条测试就失败。
func TestServerCAPrivateKeyStaysOutOfSnapshots(t *testing.T) {
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "deploy", "dec-cloud-deploy.sh"))
	if err != nil {
		t.Fatalf("读部署脚本失败: %v", err)
	}
	script := string(data)
	if n := strings.Count(script, "--exclude='./certs/ca.key'"); n < 2 {
		t.Errorf("部署脚本应在快照与回滚两处排除 certs/ca.key，实际 %d 处", n)
	}
	if !strings.Contains(script, "purge_snapshot_secrets") {
		t.Error("部署脚本应清理历史快照中残留的私钥")
	}

	// 仓库里不得出现任何 ca.key 文件：私钥一旦提交，公开仓库的历史就收不回来了。
	err = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".cache", "target":
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() == "ca.key" {
			t.Errorf("仓库里出现了服务端 CA 私钥文件: %s", path)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("遍历仓库失败: %v", err)
	}
}

// TestInstallScriptMatchesCLI 守住 Windows 一键安装脚本与 CLI 的一致性。
//
// scripts/install-agent.ps1 是 windows 上唯一的安装入口，但它是一份独立的
// 文本：CLI 改子命令名或 flag 之后，脚本不会跟着报错，只会在用户机器上
// 跑到一半才失败（例如 `--tray` 改名后，用户看到「flag provided but not
// defined」，二进制已经下载完了）。这条测试把脚本里引用的每个子命令和
// flag 都拿去 cmd/dec-agent/main.go 里核对。
//
// 只静态比对字符串，不执行脚本：本机是 macOS，CI 也不在 Windows 上跑
// PowerShell，能守住的范围就是「名字对得上」。
func TestInstallScriptMatchesCLI(t *testing.T) {
	root := repoRoot(t)

	scriptPath := filepath.Join(root, "scripts", "install-agent.ps1")
	scriptData, err := os.ReadFile(scriptPath)
	if err != nil {
		t.Fatalf("读安装脚本失败: %v", err)
	}
	script := string(scriptData)

	mainData, err := os.ReadFile(filepath.Join(root, "cmd", "dec-agent", "main.go"))
	if err != nil {
		t.Fatalf("读 dec-agent 入口失败: %v", err)
	}
	mainGo := string(mainData)

	// 脚本里所有 `& $agentExe <sub> ...` 形式的调用。
	subRe := regexp.MustCompile(`&\s*\$agentExe\s+([a-z][a-z-]*)`)
	subs := map[string]bool{}
	for _, m := range subRe.FindAllStringSubmatch(script, -1) {
		subs[m[1]] = true
	}
	if len(subs) == 0 {
		t.Fatal("安装脚本里没有找到任何 $agentExe 调用，正则可能已经与脚本写法脱节")
	}

	// main.go 里 switch 分派的子命令集合。
	dispatched := map[string]bool{}
	for _, m := range regexp.MustCompile(`case "([a-z][a-z-]*)":`).FindAllStringSubmatch(mainGo, -1) {
		dispatched[m[1]] = true
	}

	for sub := range subs {
		if !dispatched[sub] {
			t.Errorf("安装脚本调用了 dec-agent %s，但 main.go 的分派里没有这个子命令；"+
				"脚本会在用户机器上失败", sub)
		}
	}

	// 脚本里每个 `--flag` 都必须在 main.go 的 flag 注册里出现过。
	// 只查长 flag：单横线的短选项本项目没有用。
	flagRe := regexp.MustCompile(`--([a-z][a-z-]+)`)
	registered := map[string]bool{}
	for _, m := range regexp.MustCompile(`fs\.(?:String|Bool|Int)\("([a-z][a-z-]*)"`).FindAllStringSubmatch(mainGo, -1) {
		registered[m[1]] = true
	}
	// PowerShell 自身的参数与脚本自定义的说明性短横线不算 CLI flag。
	systemFlags := map[string]bool{
		"NoProfile": true, "UseBasicParsing": true, "OutFile": true,
		"AsSecureString": true, "Algorithm": true, "Force": true,
	}
	for _, m := range flagRe.FindAllStringSubmatch(script, -1) {
		name := m[1]
		if systemFlags[name] || registered[name] {
			continue
		}
		t.Errorf("安装脚本传了 --%s，但 dec-agent 从未注册这个 flag；"+
			"用户会在下载完二进制之后才遇到「flag provided but not defined」", name)
	}
}

// TestInstallScriptDownloadsRealArtifacts 守住脚本请求的产物名确实会被发布。
//
// release.yml 里产物名是 `dist/dec-agent-$os-$arch`（Windows 追加 .exe），
// 脚本按 `dec-agent-windows-amd64.exe` 拼接 URL。两者一旦有一边改了命名，
// 下载页仍然是 200，只是 404 掉产物：用户拿到的是「下载失败」，而不是
// 一个指向真正问题的错误。这条测试把命名对齐变成可校验的约束。
func TestInstallScriptDownloadsRealArtifacts(t *testing.T) {
	root := repoRoot(t)

	scriptData, err := os.ReadFile(filepath.Join(root, "scripts", "install-agent.ps1"))
	if err != nil {
		t.Fatalf("读安装脚本失败: %v", err)
	}
	script := string(scriptData)

	wfData, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatalf("读 release.yml 失败: %v", err)
	}
	wf := string(wfData)

	// 发布通道必须包含脚本会去取的每一个平台组合。
	// 脚本拼的是 /download/dec-agent-$platform.exe，platform 只可能是
	// windows-amd64 或 windows-arm64。
	for _, arch := range []string{"windows/amd64", "windows/arm64"} {
		if !strings.Contains(wf, arch) {
			t.Errorf("release.yml 的客户端平台组合里没有 %s，但安装脚本会按它拼下载地址", arch)
		}
	}

	// 脚本给 Windows 产物拼了 .exe 后缀，构建端必须也加。
	if !strings.Contains(script, "dec-agent-$platform.exe") {
		t.Error("安装脚本没有使用 dec-agent-$platform.exe 形式的下载地址，请确认产物名约定")
	}
	if !strings.Contains(wf, `out="$out.exe"`) {
		t.Error("release.yml 不再给 Windows 产物加 .exe 后缀，安装脚本的下载地址会 404")
	}

	// 脚本依赖下载响应里的摘要头做完整性校验，服务端必须真的设置它。
	srvData, err := os.ReadFile(filepath.Join(root, "internal", "cloudsvc", "download.go"))
	if err != nil {
		t.Fatalf("读下载实现失败: %v", err)
	}
	if !strings.Contains(string(srvData), "X-Checksum-SHA256") {
		t.Error("服务端不再返回 X-Checksum-SHA256，安装脚本会退化成「跳过完整性校验」")
	}

	// 脚本把托盘当可选组件，所以发布通道里托盘缺失不算错；
	// 但它一旦出现，名字必须是脚本要找的那个。
	if strings.Contains(wf, "dec-tray-windows") && !strings.Contains(wf, "dec-tray-windows-amd64") {
		t.Error("release.yml 里的 Windows 托盘产物名与安装脚本期望的 dec-tray-windows-amd64.exe 不一致")
	}
}
