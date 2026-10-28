package assets

import (
	"errors"
	"strings"
	"testing"
)

// fileManifest 拼出只含一条 file 条目的 manifest。
func fileManifest(extra string) []byte {
	return []byte("name: creds\nitems:\n  - kind: file\n    path: f/item\n    name: item\n" + extra)
}

func writeFileItem(t *testing.T, w *Writer, extra string) error {
	t.Helper()
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle:   "creds",
		Files:    []AssetFile{{Path: "f/item", Content: []byte("Host x\n")}},
		Manifest: fileManifest(extra),
		Author:   author(),
	})
	return err
}

func TestFileItemAcceptsValidSlots(t *testing.T) {
	cases := map[string]string{
		"ssh 公钥":    "    planes: [user]\n    root: ssh\n    target: dec_github.pub\n",
		"ssh 片段":    "    planes: [user]\n    root: ssh\n    target: config.d/dec.conf\n    mode: 420\n",
		"ssh config": "    planes: [user]\n    root: ssh\n    target: config\n    format: managed_block\n",
		"项目 secret":  "    planes: [project]\n    root: secrets\n    target: prod/app.env\n",
	}
	for name, extra := range cases {
		w, _ := newTestWriter(t)
		if err := writeFileItem(t, w, extra); err != nil {
			t.Fatalf("%s: 应通过，实际 %v", name, err)
		}
	}
}

func TestFileItemRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"缺少根":              "    planes: [user]\n    target: dec_a.pub\n",
		"未知根":              "    planes: [user]\n    root: home\n    target: dec_a.pub\n",
		"ssh 用项目平面":        "    planes: [project]\n    root: ssh\n    target: dec_a.pub\n",
		"secrets 用用户平面":    "    planes: [user]\n    root: secrets\n    target: a.env\n",
		"两个平面":             "    planes: [user, project]\n    root: ssh\n    target: dec_a.pub\n",
		"ssh 非白名单":         "    planes: [user]\n    root: ssh\n    target: id_rsa\n",
		"ssh 越界":           "    planes: [user]\n    root: ssh\n    target: ../dec_a\n",
		"config 用 whole":   "    planes: [user]\n    root: ssh\n    target: config\n",
		"非 config 用托管块":    "    planes: [user]\n    root: ssh\n    target: dec_a.pub\n    format: managed_block\n",
		"secrets 用托管块":     "    planes: [project]\n    root: secrets\n    target: a.env\n    format: managed_block\n",
		"secrets 写 ignore": "    planes: [project]\n    root: secrets\n    target: .gitignore\n",
		"secrets 越界":       "    planes: [project]\n    root: secrets\n    target: a/../../b\n",
		"未知格式":             "    planes: [user]\n    root: ssh\n    target: dec_a.pub\n    format: yaml\n",
		"权限越界":             "    planes: [user]\n    root: ssh\n    target: dec_a.pub\n    mode: 4095\n",
	}
	for name, extra := range cases {
		w, _ := newTestWriter(t)
		if err := writeFileItem(t, w, extra); !errors.Is(err, ErrSchema) {
			t.Fatalf("%s: 期望 ErrSchema，实际 %v", name, err)
		}
	}
}

func TestFileItemRejectsDuplicateSlotInBundle(t *testing.T) {
	w, _ := newTestWriter(t)
	raw := []byte(`name: creds
items:
  - kind: file
    path: a
    name: a
    planes: [user]
    root: ssh
    target: dec_a.pub
  - kind: file
    path: b
    name: b
    planes: [user]
    root: ssh
    target: dec_a.pub
`)
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle:   "creds",
		Files:    []AssetFile{{Path: "a", Content: []byte("a")}, {Path: "b", Content: []byte("b")}},
		Manifest: raw,
		Author:   author(),
	})
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("期望 ErrSchema，实际 %v", err)
	}
}

func TestNonFileItemRejectsFileFields(t *testing.T) {
	w, _ := newTestWriter(t)
	raw := []byte("name: woa\nitems:\n  - kind: rule\n    path: a.md\n    name: a\n    planes: [user]\n    root: ssh\n")
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle:   "woa",
		Files:    []AssetFile{{Path: "a.md", Content: []byte("a")}},
		Manifest: raw,
		Author:   author(),
	})
	if !errors.Is(err, ErrSchema) || !strings.Contains(err.Error(), "a") {
		t.Fatalf("期望 ErrSchema，实际 %v", err)
	}
}
