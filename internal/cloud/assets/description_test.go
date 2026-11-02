package assets

import (
	"errors"
	"testing"
)

// description 写成多行时，整包写入与单文件编辑都必须拒绝：With 等宿主只认
// 单行写法，多行描述下发后 skill 列表里显示为空。
func TestDescriptionMustBeSingleLine(t *testing.T) {
	w, _ := newTestWriter(t)

	folded := []byte("---\nname: demo\ndescription: >\n  第一行\n  第二行\n---\n# Demo\n")
	_, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  []AssetFile{{Path: "demo/SKILL.md", Content: folded}},
		Author: author(),
	})
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("多行 description 的写入应被拒绝，实际 %v", err)
	}

	single := []byte("---\nname: demo\ndescription: 第一行 第二行\n---\n# Demo\n")
	if _, err := w.WriteAsset(WriteAssetInput{
		Bundle: "woa",
		Files:  []AssetFile{{Path: "demo/SKILL.md", Content: single}},
		Author: author(),
	}); err != nil {
		t.Fatalf("单行 description 应能写入: %v", err)
	}

	_, err = w.EditFile(EditFileInput{
		Bundle: "woa", Path: "demo/SKILL.md",
		OldString: "description: 第一行 第二行", NewString: "description: |\n  第一行\n  第二行",
		Author: author(),
	})
	if !errors.Is(err, ErrSchema) {
		t.Fatalf("把 description 改成多行的编辑应被拒绝，实际 %v", err)
	}
}
