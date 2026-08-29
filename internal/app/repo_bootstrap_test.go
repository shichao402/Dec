package app

import (
	"context"
	"testing"

	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/secrets/handler"
)

func TestPrepareRepoGCMBootstrapFindsMatchingHostWithoutExposingPassword(t *testing.T) {
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"cnb/private/user": {
			{RelativePath: ".gcm/cnb.yaml", Content: "host: cnb.cool\nusername: firo\npassword: top-secret\n"},
			{RelativePath: ".env/app.env", Content: "TOKEN=also-secret\n"},
		},
		"github/private/user": {
			{RelativePath: ".gcm/github.yaml", Content: "host: github.com\nusername: firo\npassword: gh-secret\n"},
		},
	}}
	oldFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = oldFactory })

	result, err := PrepareRepoGCMBootstrap(context.Background(), "https://cnb.cool/shichao402/private.git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 {
		t.Fatalf("Candidates = %#v", result.Candidates)
	}
	got := result.Candidates[0]
	if got.Address != "cnb/private/user" || got.NotePath != ".gcm/cnb.yaml" || got.Host != "cnb.cool" || got.Username != "firo" {
		t.Fatalf("candidate = %#v", got)
	}
	if got.Unmanaged {
		t.Fatalf("项目地址不应标 Unmanaged: %#v", got)
	}
}

func TestPrepareRepoGCMBootstrapMarksBareFolderUnmanaged(t *testing.T) {
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"Dec": {
			{RelativePath: ".gcm/cnb.yaml", Content: "host: cnb.cool\nusername: firo\npassword: top-secret\n"},
		},
	}}
	oldFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = oldFactory })

	result, err := PrepareRepoGCMBootstrap(context.Background(), "https://cnb.cool/example/private.git", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Candidates) != 1 || !result.Candidates[0].Unmanaged {
		t.Fatalf("裸 folder 候选应标记 Unmanaged: %#v", result.Candidates)
	}
}

type captureGCMHandler struct {
	item handler.Item
}

func (h *captureGCMHandler) Kind() string                               { return "gcm" }
func (h *captureGCMHandler) Source() handler.SourceKind                 { return handler.SourceNote }
func (h *captureGCMHandler) Match(name string) bool                     { return handler.MatchGCMPath(name) }
func (h *captureGCMHandler) Revoke(context.Context, handler.Item) error { return nil }
func (h *captureGCMHandler) Apply(_ context.Context, item handler.Item) error {
	h.item = item
	return nil
}

func TestApplyRepoGCMBootstrapReusesGCMHandlerAndProbesRepo(t *testing.T) {
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"cnb/private/user": {
			{RelativePath: ".gcm/cnb.yaml", Content: "host: cnb.cool\nusername: firo\npassword: top-secret\n"},
		},
	}}
	oldFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = oldFactory })

	capture := &captureGCMHandler{}
	reg := handler.NewRegistry()
	reg.Register(capture)
	restoreRegistry := handler.SetDefault(reg)
	t.Cleanup(restoreRegistry)

	oldProbe := probeRepoForBootstrap
	probed := ""
	probeRepoForBootstrap = func(repoURL string) error {
		probed = repoURL
		return nil
	}
	t.Cleanup(func() { probeRepoForBootstrap = oldProbe })

	input := ApplyRepoGCMBootstrapInput{
		RepoURL: "https://cnb.cool/shichao402/private.git", Address: "cnb/private/user", NotePath: ".gcm/cnb.yaml",
	}
	result, err := ApplyRepoGCMBootstrap(context.Background(), input, nil)
	if err != nil {
		t.Fatal(err)
	}
	if probed != input.RepoURL {
		t.Fatalf("probe = %q", probed)
	}
	if capture.item.Name != ".gcm/cnb.yaml" || capture.item.NoteContent == "" {
		t.Fatalf("handler item = %#v", capture.item)
	}
	if result.Candidate.Username != "firo" {
		t.Fatalf("result = %#v", result)
	}
}

func TestApplyRepoGCMBootstrapRejectsHostMismatchBeforeApply(t *testing.T) {
	secrets.SetSession("test-session")
	t.Cleanup(secrets.ClearSession)
	stub := &secrets.StubClient{NotesByFolder: map[string][]secrets.SecureNote{
		"github/private/user": {
			{RelativePath: ".gcm/github.yaml", Content: "host: github.com\nusername: firo\npassword: secret\n"},
		},
	}}
	oldFactory := secretsClientFactory
	secretsClientFactory = func() secrets.Client { return stub }
	t.Cleanup(func() { secretsClientFactory = oldFactory })

	_, err := ApplyRepoGCMBootstrap(context.Background(), ApplyRepoGCMBootstrapInput{
		RepoURL: "https://cnb.cool/shichao402/private.git", Address: "github/private/user", NotePath: ".gcm/github.yaml",
	}, nil)
	if err == nil {
		t.Fatal("host 不匹配应失败")
	}
}
