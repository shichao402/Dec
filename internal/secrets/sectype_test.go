package secrets

import (
	"context"
	"testing"
)

func TestParseTypePath(t *testing.T) {
	tp, ok, err := ParseTypePath(".gcm/cnb.yaml")
	if err != nil || !ok || tp.Type.ID != SecretTypeGCM || tp.Instance != "cnb" {
		t.Fatalf("gcm => %#v ok=%v err=%v", tp, ok, err)
	}
	tp, ok, err = ParseTypePath(".sshkey/deploy")
	if err != nil || !ok || tp.Type.ID != SecretTypeSSHKey || tp.Instance != "deploy" {
		t.Fatalf("sshkey => %#v ok=%v err=%v", tp, ok, err)
	}
	tp, ok, err = ParseTypePath(".env/app.env")
	if err != nil || !ok || tp.Type.ID != SecretTypeEnv {
		t.Fatalf("env => %#v ok=%v err=%v", tp, ok, err)
	}
	_, ok, err = ParseTypePath("config/x.json")
	if err != nil || ok {
		t.Fatalf("plain note should ok=false, got ok=%v err=%v", ok, err)
	}
	_, _, err = ParseTypePath(".unknown/x")
	if err == nil {
		t.Fatal("unknown dot dir should fail")
	}
}

func TestMigrateLegacyPaths(t *testing.T) {
	got, ok := MigrateLegacyGitGCMPath("cnb_gitgcm.yaml")
	if !ok || got != ".gcm/cnb.yaml" {
		t.Fatalf("gitgcm => %q ok=%v", got, ok)
	}
	got, ok = MigrateLegacyEnvPath("env/foo.env")
	if !ok || got != ".env/foo.env" {
		t.Fatalf("env => %q ok=%v", got, ok)
	}
}

func TestSSHKeyInstance(t *testing.T) {
	inst, err := SSHKeyInstance(".sshkey/deploy")
	if err != nil || inst != "deploy" {
		t.Fatalf("got %q err=%v", inst, err)
	}
	if _, err := SSHKeyInstance("deploy"); err == nil {
		t.Fatal("bare name should fail")
	}
}

func TestIsEnvNote_DotEnv(t *testing.T) {
	if !IsEnvNote(".env/app.env") {
		t.Fatal("want .env note")
	}
	if IsEnvNote("env/app.env") {
		t.Fatal("旧 env/ 不再视为 env note")
	}
}

func TestMigrateTypeDirNames(t *testing.T) {
	stub := &StubClient{
		NotesByFolder: map[string][]SecureNote{
			"bundle/cnb": {
				{RelativePath: "cnb_gitgcm.yaml", Content: "host: cnb.cool\nusername: cnb\npassword: x\n"},
				{RelativePath: "env/app.env", Content: "A=1\n"},
			},
		},
		SSHKeysByFolder: map[string][]SSHKeyItem{
			"bundle/cnb": {{Name: "deploy", PrivateKey: "-----BEGIN OPENSSH PRIVATE KEY-----\nx\n-----END OPENSSH PRIVATE KEY-----\n"}},
		},
	}
	target := SyncTarget{Kind: SyncKindBundle, Name: "cnb", Folder: "bundle/cnb", LocalRoot: "bundles/cnb", Plane: SyncPlaneMachine}
	res, err := MigrateTypeDirNames(context.Background(), stub, "", target)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.RenamedNotes) != 2 || len(res.RenamedSSH) != 1 {
		t.Fatalf("migrate result = %#v", res)
	}
	notes := stub.NotesByFolder["bundle/cnb"]
	paths := map[string]bool{}
	for _, n := range notes {
		paths[n.RelativePath] = true
	}
	if !paths[".gcm/cnb.yaml"] || !paths[".env/app.env"] {
		t.Fatalf("notes after migrate = %#v", notes)
	}
	if stub.SSHKeysByFolder["bundle/cnb"][0].Name != ".sshkey/deploy" {
		t.Fatalf("ssh name = %q", stub.SSHKeysByFolder["bundle/cnb"][0].Name)
	}
}
