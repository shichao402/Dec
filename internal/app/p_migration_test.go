package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/config"
	"github.com/shichao402/Dec/internal/secrets"
	"github.com/shichao402/Dec/internal/types"
)

func writeMigrationFile(t *testing.T, root, rel, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPMigrationPlan_NormalizesAndMapsFourQuadrants(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "projects/My_App.yaml", "name: My App\nbundles: [Shared_Tools, My_App]\nides: [cursor]\n")
	writeMigrationFile(t, root, "bundles/Shared_Tools/bundle.yaml", "name: Shared_Tools\nscope: project\nmembers: [rule/shared]\n")
	writeMigrationFile(t, root, "bundles/Shared_Tools/rules/shared.mdc", "shared")
	writeMigrationFile(t, root, "bundles/My_App/bundle.yaml", "name: My_App\nscope: project\nmembers: []\n")

	plan, err := BuildPMigrationPlan(root, PMigrationBWSnapshot{Folders: map[string]PMigrationBWFolder{
		"bundle/My_App": {Notes: []string{".env/app.env"}},
		"My_App":        {Notes: []string{"keys/signing.pem"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HasBlockers() {
		t.Fatalf("unexpected blockers: %#v", plan.Issues)
	}
	if !plan.LegacyDetected || plan.Fingerprint == "" {
		t.Fatalf("invalid plan: %#v", plan)
	}
	if got, want := plan.GitMoves[0].Target, "shared-tools/public/project/rules/shared.mdc"; got != want {
		t.Fatalf("Git target = %q, want %q", got, want)
	}
	var appManifest *PMigrationManifest
	for i := range plan.Manifests {
		if plan.Manifests[i].Name == "my-app" {
			appManifest = &plan.Manifests[i]
		}
	}
	if appManifest == nil || !reflect.DeepEqual(appManifest.Requires, []string{"shared-tools"}) {
		t.Fatalf("my-app manifest = %#v", appManifest)
	}
	gotTargets := []string{plan.BWMoves[0].TargetFolder, plan.BWMoves[1].TargetFolder}
	if !reflect.DeepEqual(gotTargets, []string{"my-app/private/project", "my-app/private/project"}) {
		t.Fatalf("BW targets = %#v", gotTargets)
	}
}

func TestBuildPMigrationPlan_ReportsMissingAndNormalizationCollision(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "projects/Foo_Bar.yaml", "name: Foo_Bar\nbundles: [missing]\n")
	writeMigrationFile(t, root, "bundles/foo-bar/bundle.yaml", "name: foo-bar\nscope: project\nmembers: []\n")

	plan, err := BuildPMigrationPlan(root, PMigrationBWSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	codes := map[string]bool{}
	for _, issue := range plan.Issues {
		codes[issue.Code] = true
	}
	if !codes["missing_reference"] || !codes["case_normalization_collision"] || !plan.HasBlockers() {
		t.Fatalf("issues = %#v", plan.Issues)
	}
}

func TestBuildPMigrationPlan_ReportsTargetAndGitBWConflict(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "projects/app.yaml", "name: app\nbundles: [app]\n")
	writeMigrationFile(t, root, "bundles/app/bundle.yaml", "name: app\nscope: project\nmembers: []\n")
	writeMigrationFile(t, root, "app/private/project/.env/app.env", "not-secret-metadata")

	plan, err := BuildPMigrationPlan(root, PMigrationBWSnapshot{Folders: map[string]PMigrationBWFolder{
		"bundle/app": {Notes: []string{".env/app.env"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, issue := range plan.Issues {
		if issue.Code == "git_bw_path_conflict" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected git_bw_path_conflict, got %#v", plan.Issues)
	}
}

func TestBuildPMigrationPlan_RejectsEscapingBWPath(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "bundles/app/bundle.yaml", "name: app\nscope: project\nmembers: []\n")

	plan, err := BuildPMigrationPlan(root, PMigrationBWSnapshot{Folders: map[string]PMigrationBWFolder{
		"bundle/app": {Notes: []string{"../../outside.env"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasBlockers() {
		t.Fatalf("逃逸路径必须阻断迁移: %#v", plan.Issues)
	}
	for _, move := range plan.BWMoves {
		if strings.Contains(move.Path, "..") {
			t.Fatalf("非法路径不得进入执行计划: %#v", move)
		}
	}
}

func TestBuildPMigrationPlan_RejectsNoteAndSSHAtSameLogicalPath(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "bundles/app/bundle.yaml", "name: app\nscope: project\nmembers: []\n")

	plan, err := BuildPMigrationPlan(root, PMigrationBWSnapshot{Folders: map[string]PMigrationBWFolder{
		"bundle/app": {
			Notes:   []string{".sshkey/deploy"},
			SSHKeys: []string{".sshkey/deploy"},
		},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if !plan.HasBlockers() {
		t.Fatalf("Note 与 SSH Key 同逻辑路径必须冲突: %#v", plan.Issues)
	}
}

type migrationBackendStub struct {
	calls  []string
	failAt string
}

func (b *migrationBackendStub) call(name string) error {
	b.calls = append(b.calls, name)
	if b.failAt == name {
		return errors.New("injected " + name)
	}
	return nil
}

func (b *migrationBackendStub) BackupLocal(context.Context, *PMigrationPlan) (string, error) {
	return "backup", b.call("backup")
}
func (b *migrationBackendStub) PrepareGit(context.Context, *PMigrationPlan) error {
	return b.call("prepare-git")
}
func (b *migrationBackendStub) VerifyGit(context.Context, *PMigrationPlan) error {
	return b.call("verify-git")
}
func (b *migrationBackendStub) WriteBitwarden(context.Context, *PMigrationPlan) error {
	return b.call("write-bw")
}
func (b *migrationBackendStub) VerifyBitwarden(context.Context, *PMigrationPlan) error {
	return b.call("verify-bw")
}
func (b *migrationBackendStub) SwitchLocal(context.Context, *PMigrationPlan, string) error {
	return b.call("switch-local")
}
func (b *migrationBackendStub) DeleteLegacyBitwarden(context.Context, *PMigrationPlan) error {
	return b.call("delete-bw")
}
func (b *migrationBackendStub) DeleteLegacyGit(context.Context, *PMigrationPlan) error {
	return b.call("delete-git")
}

func executableMigrationPlan(t *testing.T) *PMigrationPlan {
	t.Helper()
	root := t.TempDir()
	writeMigrationFile(t, root, "bundles/app/bundle.yaml", "name: app\nscope: user\nmembers: []\n")
	plan, err := BuildPMigrationPlan(root, PMigrationBWSnapshot{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HasBlockers() {
		t.Fatalf("plan blocked: %#v", plan.Issues)
	}
	return plan
}

func TestExecutePMigration_ResumesAfterFailureWithoutEarlyDelete(t *testing.T) {
	plan := executableMigrationPlan(t)
	journalPath := filepath.Join(t.TempDir(), "migration.json")
	first := &migrationBackendStub{failAt: "write-bw"}
	journal, err := ExecutePMigration(context.Background(), plan, journalPath, first, nil)
	if err == nil || journal.Phase != PMigrationGitPrepared {
		t.Fatalf("phase=%q err=%v", journal.Phase, err)
	}
	if strings.Contains(strings.Join(first.calls, ","), "delete-") {
		t.Fatalf("failure must not delete legacy nodes: %#v", first.calls)
	}

	second := &migrationBackendStub{}
	journal, err = ExecutePMigration(context.Background(), plan, journalPath, second, nil)
	if err != nil {
		t.Fatal(err)
	}
	if journal.Phase != PMigrationComplete {
		t.Fatalf("phase = %q", journal.Phase)
	}
	want := []string{"write-bw", "verify-bw", "switch-local", "delete-bw", "delete-git"}
	if !reflect.DeepEqual(second.calls, want) {
		t.Fatalf("resume calls = %#v, want %#v", second.calls, want)
	}
}

func TestExecutePMigration_IsReentrantAfterComplete(t *testing.T) {
	plan := executableMigrationPlan(t)
	journalPath := filepath.Join(t.TempDir(), "migration.json")
	backend := &migrationBackendStub{}
	if _, err := ExecutePMigration(context.Background(), plan, journalPath, backend, nil); err != nil {
		t.Fatal(err)
	}
	again := &migrationBackendStub{}
	journal, err := ExecutePMigration(context.Background(), plan, journalPath, again, nil)
	if err != nil || journal.Phase != PMigrationComplete || len(again.calls) != 0 {
		t.Fatalf("journal=%#v calls=%#v err=%v", journal, again.calls, err)
	}
}

func TestPreviewPMigration_ReturnsIncompleteJournalForProcessRestartRecovery(t *testing.T) {
	root := t.TempDir()
	plan := executableMigrationPlan(t)
	path := filepath.Join(root, ".dec", "migrations", "p-four-quadrant-v1.json")
	if err := savePMigrationJournal(path, &PMigrationJournal{
		Version: PMigrationJournalVersion, PlanFingerprint: plan.Fingerprint,
		Plan: plan, Phase: PMigrationGitPrepared,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := PreviewPMigration(context.Background(), NewWorkspace(WorkspaceProject, root), nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Fingerprint != plan.Fingerprint {
		t.Fatalf("recovered fingerprint=%q want=%q", got.Fingerprint, plan.Fingerprint)
	}
}

func TestLivePMigrationBackend_BitwardenUsesStubAndVerifiesBeforeDelete(t *testing.T) {
	client := &secrets.StubClient{
		NotesByFolder: map[string][]secrets.SecureNote{
			"bundle/My_App": {{RelativePath: ".env/app.env", Content: "TOKEN=test"}},
		},
		SSHKeysByFolder: map[string][]secrets.SSHKeyItem{
			"bundle/My_App": {{Name: ".sshkey/deploy", PrivateKey: "private", PublicKey: "public"}},
		},
	}
	plan := &PMigrationPlan{BWMoves: []PMigrationBWMove{
		{SourceFolder: "bundle/My_App", TargetFolder: "my-app/private/project", Path: ".env/app.env", Kind: "note"},
		{SourceFolder: "bundle/My_App", TargetFolder: "my-app/private/project", Path: ".sshkey/deploy", Kind: "sshkey"},
	}}
	backend := &livePMigrationBackend{workspace: NewWorkspace(WorkspaceProject, t.TempDir()), client: client}
	if err := backend.WriteBitwarden(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := backend.VerifyBitwarden(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if err := backend.DeleteLegacyBitwarden(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if len(client.NotesByFolder["bundle/My_App"]) != 0 || len(client.SSHKeysByFolder["bundle/My_App"]) != 0 {
		t.Fatalf("legacy source not deleted: notes=%#v keys=%#v", client.NotesByFolder, client.SSHKeysByFolder)
	}
	if len(client.NotesByFolder["my-app/private/project"]) != 1 || len(client.SSHKeysByFolder["my-app/private/project"]) != 1 {
		t.Fatalf("target missing: notes=%#v keys=%#v", client.NotesByFolder, client.SSHKeysByFolder)
	}
}

func TestLivePMigrationBackend_BackupAndSwitchLocalInTempWorkspace(t *testing.T) {
	decHome := t.TempDir()
	setEnvForProjectTest(t, "DEC_HOME", decHome)
	root := t.TempDir()
	mgr := config.NewProjectConfigManager(root)
	if err := mgr.SaveProjectConfig(&types.ProjectConfig{
		ProjectName: "My_App", EnabledBundles: []string{"My_App", "Shared_Tools"},
	}); err != nil {
		t.Fatal(err)
	}
	writeMigrationFile(t, root, ".dec/cache/My_App/rules/a.mdc", "rule")
	writeMigrationFile(t, root, ".secrets/project/.env/app.env", "A=1")
	writeMigrationFile(t, decHome, "cache/User_Tools/rules/u.mdc", "user rule")
	plan := &PMigrationPlan{
		GitMoves: []PMigrationGitMove{
			{Source: "bundles/My_App/rules/a.mdc", Target: "my-app/public/project/rules/a.mdc"},
			{Source: "bundles/User_Tools/rules/u.mdc", Target: "user-tools/public/user/rules/u.mdc"},
		},
		BWMoves: []PMigrationBWMove{{
			SourceFolder: "My_App", TargetFolder: "my-app/private/project", Path: ".env/app.env", Kind: "note",
		}},
	}
	backend := &livePMigrationBackend{workspace: NewWorkspace(WorkspaceProject, root)}
	backup, err := backend.BackupLocal(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(backup, "config.yaml")); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if err := backend.SwitchLocal(context.Background(), plan, backup); err != nil {
		t.Fatal(err)
	}
	cfg, err := mgr.LoadProjectConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.ProjectName != "my-app" || !reflect.DeepEqual(cfg.EnabledBundles, []string{"my-app", "shared-tools"}) {
		t.Fatalf("normalized config = %#v", cfg)
	}
	for _, rel := range []string{
		".dec/cache/my-app/public/project/rules/a.mdc",
		".secrets/my-app/.env/app.env",
	} {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel))); err != nil {
			t.Fatalf("switched file %s missing: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, ".dec", "cache", "user-tools", "public", "user", "rules", "u.mdc")); !os.IsNotExist(err) {
		t.Fatalf("user 象限不应迁入项目 cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(decHome, "cache", "user-tools", "public", "user", "rules", "u.mdc")); err != nil {
		t.Fatalf("用户 cache 未切换: %v", err)
	}
	if _, err := os.Stat(filepath.Join(decHome, "cache", "my-app", "public", "project", "rules", "a.mdc")); !os.IsNotExist(err) {
		t.Fatalf("project 象限不应迁入用户 cache: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".secrets/project")); !os.IsNotExist(err) {
		t.Fatalf("legacy local secrets should be removed after backup, err=%v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".dec/cache/My_App")); !os.IsNotExist(err) {
		t.Fatalf("legacy cache should be removed after backup, err=%v", err)
	}
}
