//go:build volumesdevkeys

package keypair

import (
	"fmt"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/config"
)

// These tests cover the StatementKeyEnv override, which only a local
// development build (the volumesdevkeys tag) compiles in.

// TestVerifierKeysUsesTheOverrideOnAnotherHost: another host gets exactly the override's keys
// in place of any shipped for it, and the shipped ones, if any, without an override.
func TestVerifierKeysUsesTheOverrideOnAnotherHost(t *testing.T) {
	shipped, injected := newReleaseKey(t), newReleaseKey(t)
	table := map[string]map[uint32]string{
		productionHost: {},
		testOtherHost: {
			testVersion: pemOf(t, &shipped.PublicKey),
		},
	}
	override := fmt.Sprintf("%d:%s", testNextVersion, derOf(t, &injected.PublicKey))
	for _, base := range []string{
		"https://" + testHost,
		"https://" + testOtherHost,
		"http://localhost:8080",
		"http://[::1]:8080",
		"http://some_host:8080",
		"https://" + productionHost + ".evil.example",
		"https://" + productionHost + "@evil.example",
	} {
		t.Run(base, func(t *testing.T) {
			keys, err := verifierKeys(table, base, envOf(override))
			if err != nil {
				t.Fatal(err)
			}
			if len(keys) != 1 || !keys[testNextVersion].Equal(&injected.PublicKey) {
				t.Fatal("the host did not get exactly the override's key")
			}
		})
	}
	keys, err := verifierKeys(table, "https://"+testOtherHost, envOf(""))
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 1 || !keys[testVersion].Equal(&shipped.PublicKey) {
		t.Fatal("without an override the host did not get its shipped key")
	}
	if keys, err := verifierKeys(table, "https://"+testHost, envOf("")); err != nil || len(keys) != 0 {
		t.Fatalf("an unknown host without an override got %d keys, error %v", len(keys), err)
	}
}

// TestVerifierKeysRefusesAMalformedOverrideOnAnotherHost: a malformed value is an error, not
// an empty key set.
func TestVerifierKeysRefusesAMalformedOverrideOnAnotherHost(t *testing.T) {
	_, err := verifierKeys(verificationKeys, "https://"+testHost, envOf("1:nope"))
	if err == nil || !strings.Contains(err.Error(), StatementKeyEnv) {
		t.Fatalf("error = %v, want one naming %s", err, StatementKeyEnv)
	}
}

// TestNewVerifierReadsTheStatementKeyEnv: NewVerifier takes the override from the environment
// for another host, ignores it for production, and refuses a malformed one.
func TestNewVerifierReadsTheStatementKeyEnv(t *testing.T) {
	signer := newReleaseKey(t)
	t.Setenv(StatementKeyEnv, fmt.Sprintf("%d:%s", testVersion, derOf(t, &signer.PublicKey)))
	other, err := NewVerifier("https://"+testHost, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if len(other.keys) != 1 || !other.keys[testVersion].Equal(&signer.PublicKey) {
		t.Fatal("another host did not take the key from the environment")
	}
	production, err := NewVerifier(config.DefaultServerlessBaseURL, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range production.keys {
		if key.Equal(&signer.PublicKey) {
			t.Fatal("the production host took the key from the environment")
		}
	}
	t.Setenv(StatementKeyEnv, "garbage")
	if _, err := NewVerifier("https://"+testHost, t.TempDir()); err == nil {
		t.Fatal("NewVerifier accepted a malformed value")
	}
	if _, err := NewVerifier(config.DefaultServerlessBaseURL, t.TempDir()); err != nil {
		t.Fatalf("the production host read the malformed value: %v", err)
	}
}

// TestNoKeysNamesTheVariableOffProduction: with no keys, a host other than production is told
// which variable gives them; production is not.
func TestNoKeysNamesTheVariableOffProduction(t *testing.T) {
	t.Setenv(StatementKeyEnv, "")
	original := verificationKeys
	t.Cleanup(func() { verificationKeys = original })
	verificationKeys = map[string]map[uint32]string{
		productionHost: {},
	}
	for base, wantNamed := range map[string]bool{
		"https://" + testHost:           true,
		config.DefaultServerlessBaseURL: false,
	} {
		v, err := NewVerifier(base, t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		_, err = v.Verify([]byte("statement"), []byte("signature"), [16]byte{})
		if err == nil {
			t.Fatalf("%s: a verifier with no keys accepted a statement", base)
		}
		if named := strings.Contains(err.Error(), StatementKeyEnv); named != wantNamed {
			t.Errorf("%s: error %q names %s: %v, want %v", base, err, StatementKeyEnv, named, wantNamed)
		}
	}
}
