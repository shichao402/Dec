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
	"strconv"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/sysproc"
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

// newGenCommands 是断代后仅剩的两个入口（需求 3.4）。
var newGenCommands = []string{
	"./cmd/dec-cloud",
	"./cmd/dec-agent",
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

// TestOnlyNewGenerationCommands 守住 cmd/ 下只剩两个入口（需求 3.2、3.4）。
func TestOnlyNewGenerationCommands(t *testing.T) {
	root := repoRoot(t)
	entries, err := os.ReadDir(filepath.Join(root, "cmd"))
	if err != nil {
		t.Fatalf("读 cmd 目录失败: %v", err)
	}
	want := map[string]bool{"dec-agent": true, "dec-cloud": true}
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
		t.Fatalf("cmd/ 下应只有 dec-agent 与 dec-cloud，实际 %v", got)
	}
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
	".relkit":      true,
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
