package agenttools

import (
	"testing"

	"github.com/shichao402/Dec/internal/app"
)

func TestParsePlanes(t *testing.T) {
	planes, err := parsePlanes("both")
	if err != nil || len(planes) != 2 {
		t.Fatalf("%v %v", planes, err)
	}
	_, err = parseSinglePlane("both")
	if err == nil {
		t.Fatal("single plane should reject both")
	}
	p, err := parseSinglePlane("user")
	if err != nil || string(p) != "global" {
		t.Fatalf("%v %v", p, err)
	}
}

func TestResolveWorkspace_RejectsPlaceholder(t *testing.T) {
	_, err := resolveWorkspace(app.WorkspaceLocal, "${workspaceFolder}")
	if err == nil {
		t.Fatal("未展开占位符应失败")
	}
	root, err := resolveWorkspace(app.WorkspaceLocal, `D:\workspace\GitHub\Dec`)
	if err != nil || root == "" {
		t.Fatalf("%q %v", root, err)
	}
}
