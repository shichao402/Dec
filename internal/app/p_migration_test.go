package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shichao402/Dec/internal/secrets"
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

func TestBuildPMigrationPlan_MergesCaseOnlyNamesAndDropsUserRequires(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "projects/Dec.yaml", "name: Dec\nbundles: [tencent-cloud, pkv]\n")
	writeMigrationFile(t, root, "bundles/dec/bundle.yaml", "name: dec\nscope: project\nmembers: []\n")
	writeMigrationFile(t, root, "bundles/tencent-cloud/bundle.yaml", "name: tencent-cloud\nscope: user\nmembers: []\n")

	plan, err := BuildPMigrationPlan(root, PMigrationBWSnapshot{Folders: map[string]PMigrationBWFolder{
		"bundle/dec": {Notes: []string{"keys/dec-2026.private.pb"}},
		"Dec":        {Notes: []string{"keys/dec-2026.private.pb"}},
		"relkit":     {Notes: []string{".env/app.env"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HasBlockers() {
		t.Fatalf("unexpected blockers: %#v", plan.Issues)
	}
	codes := map[string]int{}
	for _, issue := range plan.Issues {
		codes[issue.Code]++
	}
	if codes["name_merged"] == 0 || codes["missing_reference"] == 0 || codes["invalid_user_reference"] == 0 || codes["orphan_bw_folder"] == 0 {
		t.Fatalf("issues = %#v", plan.Issues)
	}
	var dec *PMigrationManifest
	for i := range plan.Manifests {
		if plan.Manifests[i].Name == "dec" {
			dec = &plan.Manifests[i]
		}
	}
	if dec == nil || len(dec.Requires) != 0 {
		t.Fatalf("dec requires = %#v", dec)
	}
	var relkit bool
	for _, move := range plan.BWMoves {
		if move.TargetFolder == "relkit/private/project" {
			relkit = true
		}
	}
	if !relkit {
		t.Fatalf("orphan folder not mapped: %#v", plan.BWMoves)
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
	want := []string{"write-bw", "verify-bw", "delete-bw", "delete-git"}
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
		{SourceFolder: "bundle/My_App", TargetFolder: "my-app/private/local", Path: ".env/app.env", Kind: "note"},
		{SourceFolder: "bundle/My_App", TargetFolder: "my-app/private/local", Path: ".sshkey/deploy", Kind: "sshkey"},
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
	if len(client.NotesByFolder["my-app/private/local"]) != 1 || len(client.SSHKeysByFolder["my-app/private/local"]) != 1 {
		t.Fatalf("target missing: notes=%#v keys=%#v", client.NotesByFolder, client.SSHKeysByFolder)
	}
}

func TestBuildPMigrationPlan_TreatsExistingBWTargetAsAlreadyMigrated(t *testing.T) {
	root := t.TempDir()
	writeMigrationFile(t, root, "bundles/app/bundle.yaml", "name: app\nscope: user\nmembers: []\n")

	plan, err := BuildPMigrationPlan(root, PMigrationBWSnapshot{Folders: map[string]PMigrationBWFolder{
		"bundle/app":              {Notes: []string{".env/app.env"}},
		"app/private/user":        {Notes: []string{".env/app.env"}},
		"InvestM":                 {Notes: []string{"config.yaml"}},
		"investm/private/project": {Notes: []string{"config.yaml"}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if plan.HasBlockers() {
		t.Fatalf("already-migrated target must not block: %#v", plan.Issues)
	}
	var already int
	for _, issue := range plan.Issues {
		if issue.Code == "already_migrated_bw" {
			already++
		}
	}
	if already == 0 {
		t.Fatalf("expected already_migrated_bw, got %#v", plan.Issues)
	}
	found := false
	for _, folder := range plan.LegacyBWFolders {
		if folder == "InvestM" || folder == "bundle/app" {
			found = true
		}
	}
	if !found {
		t.Fatalf("legacy folders = %#v", plan.LegacyBWFolders)
	}
}

// 指纹必须只由远端内容决定：RunPMigration 会重做 preview 并比对指纹，
// 任何 map 迭代顺序泄漏都会让迁移永远无法通过自校验。
func TestBuildPMigrationPlan_FingerprintIsStableAcrossRuns(t *testing.T) {
	root := t.TempDir()
	snapshot := func() PMigrationBWSnapshot {
		return PMigrationBWSnapshot{Folders: map[string]PMigrationBWFolder{
			"bundle/dec":           {Notes: []string{"keys/dec.private.pb"}},
			"Dec":                  {Notes: []string{"note.md"}},
			"bundle/svnmergetool":  {Notes: []string{".env/tool.env"}},
			"SvnMergeTool":         {Notes: []string{"config.yaml"}},
			"bundle/tencent-cloud": {Notes: []string{".env/cloud.env"}},
			"relkit":               {Notes: []string{".env/relkit.env"}},
		}}
	}
	first, err := BuildPMigrationPlan(root, snapshot())
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		next, err := BuildPMigrationPlan(root, snapshot())
		if err != nil {
			t.Fatal(err)
		}
		if next.Fingerprint != first.Fingerprint {
			t.Fatalf("指纹不稳定: %q != %q", next.Fingerprint, first.Fingerprint)
		}
		if !reflect.DeepEqual(next.Manifests, first.Manifests) {
			t.Fatalf("manifests 漂移:\n%#v\n%#v", next.Manifests, first.Manifests)
		}
	}
}

func TestLivePMigrationBackend_DeletesEmptyLegacyFolders(t *testing.T) {
	client := &secrets.StubClient{
		NotesByFolder: map[string][]secrets.SecureNote{
			"Dec":              {},
			"bundle/dec":       {},
			"dec/private/user": {{RelativePath: "keys/dec.private.pb", Content: "x"}},
		},
		SecretBundleFolders: []string{"bundle/dec"},
	}
	plan := &PMigrationPlan{LegacyBWFolders: []string{"Dec", "bundle/dec"}}
	backend := &livePMigrationBackend{workspace: NewWorkspace(WorkspaceProject, t.TempDir()), client: client}
	if err := backend.DeleteLegacyBitwarden(context.Background(), plan); err != nil {
		t.Fatal(err)
	}
	if _, ok := client.NotesByFolder["Dec"]; ok {
		t.Fatalf("empty legacy folder not deleted: %#v", client.NotesByFolder)
	}
	if _, ok := client.NotesByFolder["dec/private/user"]; !ok {
		t.Fatal("P folder must stay")
	}
}
