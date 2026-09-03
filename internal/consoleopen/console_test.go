package consoleopen

import "testing"

func TestAvailableIsFalseUnderGoTest(t *testing.T) {
	if Available() {
		t.Fatal("test binaries must never launch Dec Console")
	}
}

func TestUnlockIntentContainsNoRequestData(t *testing.T) {
	if UnlockLocalURI != "dec://unlock/local" {
		t.Fatalf("UnlockLocalURI = %q", UnlockLocalURI)
	}
}
