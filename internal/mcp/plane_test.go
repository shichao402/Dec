package mcp

import (
	"context"
	"testing"

	"github.com/shichao402/Dec/internal/app"
	"github.com/shichao402/Dec/internal/repo"
)

func TestParsePlanes(t *testing.T) {
	cases := map[string][]app.WorkspacePlane{
		"":        {app.WorkspaceLocal},
		"project": {app.WorkspaceLocal},
		"local":   {app.WorkspaceLocal},
		"user":    {app.WorkspaceGlobal},
		"global":  {app.WorkspaceGlobal},
		"USER":    {app.WorkspaceGlobal},
		"both":    {app.WorkspaceLocal, app.WorkspaceGlobal},
	}
	for raw, want := range cases {
		got, err := parsePlanes(raw)
		if err != nil {
			t.Fatalf("parsePlanes(%q) err = %v", raw, err)
		}
		if len(got) != len(want) {
			t.Fatalf("parsePlanes(%q) = %v, want %v", raw, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("parsePlanes(%q)[%d] = %v, want %v", raw, i, got[i], want[i])
			}
		}
	}
	if _, err := parsePlanes("nope"); err == nil {
		t.Fatal("parsePlanes(\"nope\") 应报错")
	}
}

func TestParseSinglePlane(t *testing.T) {
	if p, err := parseSinglePlane(""); err != nil || p != app.WorkspaceLocal {
		t.Fatalf("parseSinglePlane(\"\") = %v, %v", p, err)
	}
	if p, err := parseSinglePlane("global"); err != nil || p != app.WorkspaceGlobal {
		t.Fatalf("parseSinglePlane(global) = %v, %v", p, err)
	}
	if _, err := parseSinglePlane("both"); err == nil {
		t.Fatal("parseSinglePlane(both) 应报错")
	}
}

// dec_delete 拒绝 plane=both（写操作单平面）。
func TestHandleDelete_RejectsBoth(t *testing.T) {
	s := New(Config{ProjectRoot: t.TempDir()})
	_, out, err := s.handleDelete(context.Background(), nil, deleteParams{Confirmed: true, Plane: "both"})
	if err != nil {
		t.Fatalf("handleDelete() err = %v", err)
	}
	resp, ok := out.(toolResponse)
	if !ok || resp.OK {
		t.Fatalf("plane=both 应失败: %#v", out)
	}
}

// dec_list_assets plane=both 聚合两平面结果为 planes[]。
func TestHandleListAssets_BothPlanes(t *testing.T) {
	decHome := t.TempDir()
	t.Setenv("DEC_HOME", decHome)
	startTestService(t)

	remote := setupRemoteRepo(t, map[string]string{
		"bundles/default/skills/helloworld/SKILL.md": "---\nname: helloworld\n---\n",
	})
	if err := repo.Connect(remote); err != nil {
		t.Fatalf("repo.Connect: %v", err)
	}

	s := New(Config{ProjectRoot: t.TempDir()})
	_, out, err := s.handleListAssets(context.Background(), nil, listAssetsParams{Plane: "both"})
	if err != nil {
		t.Fatalf("handleListAssets() err = %v", err)
	}
	resp, ok := out.(toolResponse)
	if !ok || !resp.OK {
		t.Fatalf("expected ok, got %#v", out)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T", resp.Data)
	}
	outcomes, ok := data["planes"].([]planeOutcome)
	if !ok {
		t.Fatalf("planes type = %T", data["planes"])
	}
	if len(outcomes) != 2 {
		t.Fatalf("planes len = %d, want 2", len(outcomes))
	}
	if outcomes[0].Plane != string(app.WorkspaceLocal) || outcomes[1].Plane != string(app.WorkspaceGlobal) {
		t.Fatalf("planes order = %q,%q", outcomes[0].Plane, outcomes[1].Plane)
	}
	for _, oc := range outcomes {
		if !oc.OK {
			t.Fatalf("plane %s 应成功: %s", oc.Plane, oc.Error)
		}
	}
}
