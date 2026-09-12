package mcp

import (
	"testing"

	"github.com/shichao402/Dec/internal/app"
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
