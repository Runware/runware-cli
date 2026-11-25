//go:build !volumesdevkeys

package keypair

import (
	"fmt"
	"strings"
	"testing"
)

// TestReleaseBuildIgnoresTheStatementKeyEnv: a build without the volumesdevkeys tag never
// reads StatementKeyEnv, so a host other than production gets only its shipped keys, and a
// host with none refuses every statement without naming the variable.
func TestReleaseBuildIgnoresTheStatementKeyEnv(t *testing.T) {
	shipped, injected := newReleaseKey(t), newReleaseKey(t)
	table := map[string]map[uint32]string{
		productionHost: {},
		testOtherHost: {
			testVersion: pemOf(t, &shipped.PublicKey),
		},
	}
	getenv := func(string) string {
		t.Errorf("a release build read %s", StatementKeyEnv)
		return fmt.Sprintf("%d:%s", testNextVersion, derOf(t, &injected.PublicKey))
	}
	keys, err := verifierKeys(table, "https://"+testOtherHost, getenv)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || !keys[testVersion].Equal(&shipped.PublicKey) {
		t.Fatal("the host did not get exactly its shipped key")
	}
	for _, base := range []string{
		"https://" + testHost,
		"http://localhost:8080",
	} {
		if keys, err := verifierKeys(table, base, getenv); err != nil || len(keys) != 0 {
			t.Fatalf("%s: got %d keys, error %v", base, len(keys), err)
		}
	}

	t.Setenv(StatementKeyEnv, fmt.Sprintf("%d:%s", testVersion, derOf(t, &injected.PublicKey)))
	v, err := NewVerifier("https://"+testHost, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(v.keys) != 0 {
		t.Fatal("a release build took a key from the environment")
	}
	_, err = v.Verify([]byte("statement"), []byte("signature"), [16]byte{})
	if err == nil || strings.Contains(err.Error(), StatementKeyEnv) {
		t.Fatalf("error = %v, want a refusal that does not name %s", err, StatementKeyEnv)
	}
}
