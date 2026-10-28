package acceptance

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

// 凭据落点验收（.agent/credential-landing 需求 8.3）。
//
// 用一对临时 dec_test_* 密钥和一个临时项目 secret 走完整链路：写密钥、写 file 条目、
// 订阅、等落盘、核对权限/行尾/.gitignore、退订、等删除。真实的 ~/.ssh/config
// 只读不写：它已经由用户的托管块条目占着，同一落点写第二条会被云端判为冲突。

// PlaintextMarker 是验收密钥值里的固定前缀。
//
// 线上验收（scripts/acceptance-prod.sh）在服务端磁盘与 git 历史里搜它：
// 一旦搜到，说明明文在哪一步被落盘了（需求 8.4）。两边必须保持一致。
const PlaintextMarker = "DEC-ACCEPTANCE-PLAINTEXT-"

// credRun 是一次凭据验收的运行时状态，清理时按它回收。
type credRun struct {
	bundle   string
	keyName  string
	projName string
	sshName  string
	// subsBackup 为非 nil 表示改过设备订阅，清理时原样写回。
	subsBackup []string
	wrote      bool
}

var credNameSanitize = regexp.MustCompile(`[^a-z0-9._-]+`)

func (r *runner) newCredRun() *credRun {
	suffix := strings.TrimPrefix(r.opts.Bundle, "dec-acceptance-")
	suffix = credNameSanitize.ReplaceAllString(strings.ToLower(suffix), "-")
	suffix = strings.Trim(suffix, "-.")
	if suffix == "" {
		suffix = fmt.Sprintf("%d", time.Now().Unix())
	}
	return &credRun{
		bundle:   r.opts.Bundle + "-cred",
		keyName:  r.opts.Bundle + "-ssh",
		projName: r.opts.Bundle + "-proj",
		sshName:  "dec_test_" + suffix,
	}
}

func (c *credRun) keyPath() string { return filepath.Join(homeDir(), ".ssh", c.sshName) }
func (c *credRun) pubPath() string { return c.keyPath() + ".pub" }

func (r *runner) secretsBase() string { return filepath.Join(r.opts.ProjectPath, ".secrets") }
func (r *runner) projSecretPath() string {
	return filepath.Join(r.secretsBase(), "acceptance", "token.json")
}

// checkCredentials 是凭据落点验收的主流程。
//
// 失败只记 FAIL 不返回错误：它排在主链路之后，不影响前面已有的结论。
func (r *runner) checkCredentials(ctx context.Context) {
	r.section("五、凭据落点（ssh / secrets）")
	c := r.newCredRun()
	r.cred = c

	dev, err := r.deviceInfo(ctx)
	if err != nil {
		r.step("读取本机设备信息", false, err.Error())
		return
	}
	roots := strings.Join(dev.Roots, ",")
	if !contains(dev.Roots, "ssh") || !contains(dev.Roots, "secrets") {
		r.step("代理上报 ssh、secrets 根", false, "上报的根："+roots+"（代理版本过低）")
		return
	}
	r.step("代理上报 ssh、secrets 根", true, roots)

	priv, pub, err := testKeyPair()
	if err != nil {
		r.step("生成临时密钥对", false, err.Error())
		return
	}
	// 故意用 CRLF 写进云端：落盘后必须是 LF（需求 1.5）。
	crlf := strings.ReplaceAll(priv, "\n", "\r\n")
	token := PlaintextMarker + fmt.Sprintf("%x", time.Now().UnixNano())
	for name, value := range map[string]string{c.keyName: crlf, c.projName: token} {
		if _, msg := r.callTool(ctx, "dec_put_secret", map[string]any{
			"name": name, "value": value, "notes": "验收临时密钥，跑完即删", "message": "验收：写入临时密钥",
		}); msg != "" {
			r.step("写入临时密钥", false, msg)
			return
		}
	}
	r.step("写入临时密钥", true, c.keyName+"、"+c.projName)

	manifest := fmt.Sprintf(`name: %s
items:
  - kind: file
    path: files/ssh/key
    name: key
    planes: [user]
    root: ssh
    target: %s
  - kind: file
    path: files/ssh/pub
    name: pub
    planes: [user]
    root: ssh
    target: %s.pub
  - kind: file
    path: files/secrets/token
    name: token
    planes: [project]
    root: secrets
    target: acceptance/token.json
`, c.bundle, c.sshName, c.sshName)
	if _, msg := r.callTool(ctx, "dec_write_asset", map[string]any{
		"bundle":   c.bundle,
		"manifest": manifest,
		"items": []any{
			map[string]any{"path": "files/ssh/key", "content": "{{secret:" + c.keyName + "}}"},
			map[string]any{"path": "files/ssh/pub", "content": pub},
			map[string]any{"path": "files/secrets/token", "content": `{"token":"{{secret:` + c.projName + `}}"}` + "\n"},
		},
		"message": "验收：写入临时凭据 bundle " + c.bundle,
	}); msg != "" {
		r.step("写入临时凭据 bundle", false, msg)
		return
	}
	c.wrote = true
	r.step("写入临时凭据 bundle", true, c.bundle)

	// 订阅：用户平面加到设备订阅里，项目平面挂到临时项目上。
	c.subsBackup = append([]string{}, dev.UserBundles...)
	if _, msg := r.callTool(ctx, "dec_set_subscriptions", map[string]any{
		"device_id": r.deviceID, "bundles": append(append([]string{}, dev.UserBundles...), c.bundle),
	}); msg != "" {
		c.subsBackup = nil
		r.step("订阅临时凭据 bundle", false, msg)
		return
	}
	if _, msg := r.callTool(ctx, "dec_put_project", map[string]any{
		"id": r.project, "device_id": r.deviceID, "name": "dec-acceptance",
		"path": r.opts.ProjectPath, "bundles": []any{c.bundle},
	}); msg != "" {
		r.step("订阅临时凭据 bundle", false, msg)
		return
	}
	r.step("订阅临时凭据 bundle", true, "设备用户平面 + 临时项目")

	normalized := strings.TrimRight(priv, "\n") + "\n"
	want := map[string]string{
		c.keyPath():        normalized,
		c.pubPath():        pub,
		r.projSecretPath(): `{"token":"` + token + `"}` + "\n",
	}
	if err := r.waitContents(ctx, want); err != nil {
		r.step("凭据落盘", false, err.Error())
		return
	}
	r.step("凭据落盘", true, "私钥、公钥、项目 secret 内容一致")

	r.checkCredFiles(c)
	r.checkSSHConfigBlock()
	r.checkCredStatus(ctx, c)

	// 退订并删除，确认落点清干净、目录与 .gitignore 保留（需求 1.6、2.6）。
	if _, msg := r.callTool(ctx, "dec_delete_bundle", map[string]any{
		"bundle": c.bundle, "message": "验收：删除临时凭据 bundle " + c.bundle,
	}); msg != "" {
		r.step("删除临时凭据 bundle", false, msg)
		return
	}
	c.wrote = false
	if err := r.waitGone(ctx, c.keyPath(), c.pubPath(), r.projSecretPath()); err != nil {
		r.step("凭据落点已清理", false, err.Error())
		return
	}
	var kept []string
	for _, p := range []string{filepath.Join(homeDir(), ".ssh"), r.secretsBase(), filepath.Join(r.secretsBase(), ".gitignore")} {
		if _, err := os.Stat(p); err != nil {
			kept = append(kept, p+" 不在了")
		}
	}
	if _, err := os.Stat(filepath.Dir(r.projSecretPath())); err == nil {
		kept = append(kept, "变空的子目录 "+filepath.Dir(r.projSecretPath())+" 没有清掉")
	}
	if len(kept) > 0 {
		r.step("凭据落点已清理", false, strings.Join(kept, "；"))
		return
	}
	r.step("凭据落点已清理", true, "文件已删，~/.ssh、.secrets 与 .gitignore 保留")
}

// checkCredFiles 核对权限、行尾与 .gitignore（需求 1.3、1.5、2.3、2.4）。
func (r *runner) checkCredFiles(c *credRun) {
	var bad []string
	if runtime.GOOS != "windows" {
		for p, mode := range map[string]os.FileMode{c.keyPath(): 0o600, c.pubPath(): 0o644, r.projSecretPath(): 0o600} {
			info, err := os.Stat(p)
			if err != nil {
				bad = append(bad, err.Error())
				continue
			}
			if got := info.Mode().Perm(); got != mode {
				bad = append(bad, fmt.Sprintf("%s 权限 %o，应为 %o", filepath.Base(p), got, mode))
			}
		}
	}
	if data, err := os.ReadFile(c.keyPath()); err == nil {
		if bytes.Contains(data, []byte("\r")) {
			bad = append(bad, "私钥里仍有 CR")
		}
		if !bytes.HasSuffix(data, []byte("\n")) {
			bad = append(bad, "私钥没有以换行结尾")
		}
	}
	if data, err := os.ReadFile(filepath.Join(r.secretsBase(), ".gitignore")); err != nil {
		bad = append(bad, ".secrets/.gitignore 不存在")
	} else if string(data) != "*\n" {
		bad = append(bad, fmt.Sprintf(".secrets/.gitignore 内容为 %q", string(data)))
	}
	if len(bad) > 0 {
		r.step("权限、行尾与 .gitignore", false, strings.Join(bad, "；"))
		return
	}
	r.step("权限、行尾与 .gitignore", true, "私钥 0600 且为 LF，公钥 0644，项目 secret 0600，.gitignore 为 *")
}

// checkSSHConfigBlock 只读核对 ~/.ssh/config 的托管块位于文件最前面（需求 3.2）。
func (r *runner) checkSSHConfigBlock() {
	data, err := os.ReadFile(filepath.Join(homeDir(), ".ssh", "config"))
	if err != nil || !bytes.Contains(data, []byte("# BEGIN DEC MANAGED")) {
		r.step("~/.ssh/config 托管块在顶部", true, "本机未使用托管块，跳过")
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if t == "# BEGIN DEC MANAGED" {
			if !bytes.Contains(data, []byte("# END DEC MANAGED")) {
				r.step("~/.ssh/config 托管块在顶部", false, "托管块没有结束标记")
				return
			}
			r.step("~/.ssh/config 托管块在顶部", true, "")
			return
		}
		r.step("~/.ssh/config 托管块在顶部", false, "托管块前还有内容："+t)
		return
	}
}

// checkCredStatus 确认设备详情里的凭据状态显示为已落地（需求 8.2）。
//
// 状态来自代理回执，比落盘晚一个来回，所以要等。
func (r *runner) checkCredStatus(ctx context.Context, c *credRun) {
	want := map[string]bool{c.keyPath(): true, c.pubPath(): true, r.projSecretPath(): true}
	deadline := time.Now().Add(r.opts.Timeout)
	last := "没有读到"
	for time.Now().Before(deadline) {
		dev, err := r.deviceInfo(ctx)
		if err == nil {
			landed := 0
			var other []string
			for _, s := range dev.Credentials {
				for _, p := range s.Paths {
					if !want[expandSlot(p, r.opts.ProjectPath)] {
						continue
					}
					if s.Status == "landed" {
						landed++
					} else {
						other = append(other, p+"="+s.Status)
					}
				}
			}
			if landed == len(want) {
				r.step("设备详情显示已落地", true, fmt.Sprintf("%d 个落点", landed))
				return
			}
			last = fmt.Sprintf("已落地 %d/%d %s", landed, len(want), strings.Join(other, ","))
		}
		if sleepCtx(ctx, 2*time.Second) != nil {
			break
		}
	}
	r.step("设备详情显示已落地", false, last)
}

// expandSlot 把状态里的落点描述（~/.ssh/x 或 <pid>/.secrets/x）还原成本机绝对路径。
func expandSlot(p, project string) string {
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(homeDir(), filepath.FromSlash(p[2:]))
	}
	if i := strings.Index(p, "/.secrets/"); i >= 0 {
		return filepath.Join(project, ".secrets", filepath.FromSlash(p[i+len("/.secrets/"):]))
	}
	return p
}

// cleanupCredentials 回收凭据验收产生的一切，在主清理之前执行。
func (r *runner) cleanupCredentials(ctx context.Context) {
	c := r.cred
	if c == nil {
		return
	}
	if c.wrote {
		if _, msg := r.callTool(ctx, "dec_delete_bundle", map[string]any{"bundle": c.bundle, "message": "验收清理"}); msg != "" {
			r.res.Residue = append(r.res.Residue, "临时凭据 bundle "+c.bundle+"：删除失败 "+msg)
		}
	}
	if c.subsBackup != nil {
		if _, msg := r.callTool(ctx, "dec_set_subscriptions", map[string]any{
			"device_id": r.deviceID, "bundles": c.subsBackup,
		}); msg != "" {
			r.res.Residue = append(r.res.Residue, "设备订阅恢复失败："+msg+"，原订阅："+strings.Join(c.subsBackup, ","))
		}
	}
	for _, name := range []string{c.keyName, c.projName} {
		if _, msg := r.callTool(ctx, "dec_delete_secret", map[string]any{"name": name, "force": true, "message": "验收清理"}); msg != "" &&
			!strings.Contains(msg, "不存在") && !strings.Contains(strings.ToLower(msg), "not found") {
			r.res.Residue = append(r.res.Residue, "临时密钥 "+name+" 删除失败："+msg)
		}
	}
	// 本机落点：正常路径下云端已经删掉；这里只报告，不替云端删，免得掩盖删除链路的缺陷。
	for _, p := range []string{c.keyPath(), c.pubPath()} {
		if _, err := os.Lstat(p); err == nil {
			r.res.Residue = append(r.res.Residue, "本机残留 "+p)
		}
	}
}

// deviceInfo 经管理面转发读本机设备的详情（订阅、上报的根、凭据状态）。
func (r *runner) deviceInfo(ctx context.Context) (*deviceView, error) {
	code, body, err := r.post(ctx, "/api/call", `{"method":"devices","payload":{}}`)
	if err != nil {
		return nil, err
	}
	if code != http.StatusOK {
		return nil, fmt.Errorf("devices 返回 %d：%s", code, strings.TrimSpace(body))
	}
	var out struct {
		Devices []deviceView `json:"devices"`
	}
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("devices 返回内容不是 JSON：%w", err)
	}
	for i := range out.Devices {
		if out.Devices[i].ID == r.deviceID {
			return &out.Devices[i], nil
		}
	}
	return nil, errors.New("设备列表里没有本机 " + r.deviceID)
}

type deviceView struct {
	ID          string   `json:"id"`
	UserBundles []string `json:"user_bundles"`
	Roots       []string `json:"roots"`
	Credentials []struct {
		ID     string   `json:"id"`
		Root   string   `json:"root"`
		Paths  []string `json:"paths"`
		Status string   `json:"status"`
		Reason string   `json:"reason"`
	} `json:"credentials"`
}

// waitContents 等每个文件出现且内容与期望一致。
func (r *runner) waitContents(ctx context.Context, want map[string]string) error {
	deadline := time.Now().Add(r.opts.Timeout)
	for {
		var pending []string
		for p, w := range want {
			if data, err := os.ReadFile(p); err != nil || string(data) != w {
				pending = append(pending, p)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 %s 后仍未落盘或内容不符：%s", r.opts.Timeout, strings.Join(pending, ", "))
		}
		if err := sleepCtx(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}

// waitGone 等文件全部消失。
func (r *runner) waitGone(ctx context.Context, paths ...string) error {
	deadline := time.Now().Add(r.opts.Timeout)
	for {
		var left []string
		for _, p := range paths {
			if _, err := os.Lstat(p); err == nil {
				left = append(left, p)
			}
		}
		if len(left) == 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("等待 %s 后仍在：%s", r.opts.Timeout, strings.Join(left, ", "))
		}
		if err := sleepCtx(ctx, 2*time.Second); err != nil {
			return err
		}
	}
}

// testKeyPair 生成一对 OpenSSH 格式的临时 ed25519 密钥。
func testKeyPair() (priv, pub string, err error) {
	pk, sk, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", "", err
	}
	block, err := ssh.MarshalPrivateKey(sk, "dec-acceptance")
	if err != nil {
		return "", "", err
	}
	sp, err := ssh.NewPublicKey(pk)
	if err != nil {
		return "", "", err
	}
	return string(pem.EncodeToMemory(block)), string(ssh.MarshalAuthorizedKey(sp)), nil
}

func homeDir() string {
	h, _ := os.UserHomeDir()
	return h
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}
