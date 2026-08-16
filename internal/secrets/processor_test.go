package secrets

import "testing"

func TestRegisteredProcessors_PeerTypes(t *testing.T) {
	procs := RegisteredProcessors()
	if len(procs) < 4 {
		t.Fatalf("want >=4 processors, got %d", len(procs))
	}
	seen := map[SecretTypeID]Processor{}
	for _, p := range procs {
		seen[p.ID] = p
	}
	for _, id := range []SecretTypeID{SecretTypePlain, SecretTypeGCM, SecretTypeEnv, SecretTypeSSHKey} {
		if _, ok := seen[id]; !ok {
			t.Fatalf("missing processor %s", id)
		}
	}
	if !seen[SecretTypeSSHKey].WritesSSHItem() {
		t.Fatal(".sshkey should write SSH item")
	}
	if !seen[SecretTypeGCM].WritesSecureNote() {
		t.Fatal(".gcm should write secure note")
	}
	if !seen[SecretTypeSSHKey].HasSourceMode(SourceGenerate) {
		t.Fatal(".sshkey should offer generate")
	}
	if !seen[SecretTypePlain].HasSourceMode(SourceTemp) {
		t.Fatal("note should offer temp")
	}
}

func TestProcessorNormalizeName(t *testing.T) {
	gcm, _ := LookupProcessor("gcm")
	got, err := gcm.NormalizeName("cnb")
	if err != nil || got != ".gcm/cnb.yaml" {
		t.Fatalf("gcm instance => %q err=%v", got, err)
	}
	ssh, _ := LookupProcessor("sshkey")
	got, err = ssh.NormalizeName("deploy")
	if err != nil || got != ".sshkey/deploy" {
		t.Fatalf("ssh instance => %q err=%v", got, err)
	}
	note, _ := LookupProcessor("note")
	if _, err := note.NormalizeName(".gcm/x.yaml"); err == nil {
		t.Fatal("plain note should reject typed path")
	}
}
