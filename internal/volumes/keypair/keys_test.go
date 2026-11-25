package keypair

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"testing"

	"github.com/runware/runware-cli/internal/config"
)

type hostCase struct {
	name    string
	baseURL string
	want    string
}

// TestSelectKeysFollowsTheBaseURLHost: the key set is the one of the platform whose API host
// is exactly the host of the base URL the CLI targets, and any other URL selects none.
func TestSelectKeysFollowsTheBaseURLHost(t *testing.T) {
	table := map[string]map[uint32]string{
		"prod.example": {1: "prod"},
		"dev.example":  {1: "dev"},
	}
	cases := []hostCase{
		{
			name:    "production host",
			baseURL: "https://prod.example",
			want:    "prod",
		},
		{
			name:    "development host with a path",
			baseURL: "https://dev.example/v1",
			want:    "dev",
		},
		{
			name:    "host case and port",
			baseURL: "HTTPS://DEV.example:8443",
			want:    "dev",
		},
		{
			name:    "a trailing root dot",
			baseURL: "https://prod.example.",
			want:    "prod",
		},
		{
			name:    "a subdomain of a platform host",
			baseURL: "https://prod.example.evil.example",
		},
		{
			name:    "a platform host in the path",
			baseURL: "https://evil.example/prod.example",
		},
		{
			name:    "a platform host as the user",
			baseURL: "https://prod.example@evil.example",
		},
		{
			name:    "a platform host in the query",
			baseURL: "https://evil.example?host=prod.example",
		},
		{
			name:    "no scheme",
			baseURL: "prod.example",
		},
		{
			name:    "empty",
			baseURL: "",
		},
		{
			name:    "unparsable",
			baseURL: "http://[::1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := selectKeys(table, tc.baseURL)
			if got[1] != tc.want {
				t.Fatalf("selectKeys(%q) holds %q, want %q", tc.baseURL, got[1], tc.want)
			}
		})
	}
}

// TestShippedPlatformIsTheCLIsDefaultHost: the CLI's default serverless API has a key set in
// the table, so a change to the default cannot silently leave the production set unused.
func TestShippedPlatformIsTheCLIsDefaultHost(t *testing.T) {
	if selectKeys(verificationKeys, config.DefaultServerlessBaseURL) == nil {
		t.Errorf("no key set for %s", config.DefaultServerlessBaseURL)
	}
}

func parseShipped(table map[string]map[uint32]string) error {
	for host, pems := range table {
		if _, err := parseKeys(pems); err != nil {
			return fmt.Errorf("%s: %w", host, err)
		}
	}
	return nil
}

// TestShippedKeysParse: the production platform has a key set, and every PEM shipped parses.
func TestShippedKeysParse(t *testing.T) {
	if _, ok := verificationKeys[productionHost]; !ok {
		t.Fatalf("no key set for %s", productionHost)
	}
	if err := parseShipped(verificationKeys); err != nil {
		t.Fatal(err)
	}
}

// TestShippedKeysCheckFailsOnABadPEM: the check TestShippedKeysParse runs refuses a key that does not parse.
func TestShippedKeysCheckFailsOnABadPEM(t *testing.T) {
	bad := map[string]map[uint32]string{
		productionHost: {
			1: "not PEM",
		},
	}
	if err := parseShipped(bad); err == nil {
		t.Fatal("the check passed a PEM that does not parse")
	}
}

func pemBlock(blockType string, der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{
		Type:  blockType,
		Bytes: der,
	}))
}

func pemOf(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pemBlock("PUBLIC KEY", der)
}

func TestParseKeysAcceptsP256(t *testing.T) {
	first, second := newReleaseKey(t), newReleaseKey(t)
	keys, err := parseKeys(map[uint32]string{
		1: pemOf(t, &first.PublicKey),
		2: pemOf(t, &second.PublicKey),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || !keys[1].Equal(&first.PublicKey) || !keys[2].Equal(&second.PublicKey) {
		t.Fatalf("parsed %d keys that differ from the PEMs", len(keys))
	}
}

func TestParseKeysRefuses(t *testing.T) {
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	good := newReleaseKey(t)
	for name, text := range map[string]string{
		"not PEM":           "nope",
		"not a public key":  pemBlock("PUBLIC KEY", []byte("x")),
		"another curve":     pemOf(t, &p384.PublicKey),
		"another algorithm": pemOf(t, edPublic),
		"empty":             "",
		"truncated PEM":     pemOf(t, &good.PublicKey)[:40],
	} {
		if _, err := parseKeys(map[uint32]string{1: text}); err == nil {
			t.Errorf("%s: parseKeys accepted it", name)
		}
	}
}

// TestParseKeysChecksThePEMType: a valid P-256 key in a block of another type is refused.
func TestParseKeysChecksThePEMType(t *testing.T) {
	key := newReleaseKey(t)
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseKeys(map[uint32]string{1: pemBlock("CERTIFICATE", der)}); err == nil {
		t.Fatal("parseKeys accepted a P-256 key in a CERTIFICATE block")
	}
}

// envOf is a getenv that answers value for StatementKeyEnv and "" for any other name.
func envOf(value string) func(string) string {
	return func(name string) string {
		if name == StatementKeyEnv {
			return value
		}
		return ""
	}
}

// derOf returns the standard base64 of key's SubjectPublicKeyInfo, as StatementKeyEnv carries it.
func derOf(t *testing.T, key any) string {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(der)
}

// TestParseKeyListAcceptsCurrentAndNext: two entries carry a rollover's current and next
// version, with spaces around an entry allowed.
func TestParseKeyListAcceptsCurrentAndNext(t *testing.T) {
	current, next := newReleaseKey(t), newReleaseKey(t)
	value := fmt.Sprintf("%d:%s, %d:%s ", testVersion, derOf(t, &current.PublicKey), testNextVersion, derOf(t, &next.PublicKey))
	keys, err := parseKeyList(value)
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 || !keys[testVersion].Equal(&current.PublicKey) || !keys[testNextVersion].Equal(&next.PublicKey) {
		t.Fatalf("parsed %d keys that differ from the value", len(keys))
	}
}

// TestParseKeyListRefusesAMalformedValue: every malformed value is refused with an error that
// names the variable and the entry.
func TestParseKeyListRefusesAMalformedValue(t *testing.T) {
	p384, err := ecdsa.GenerateKey(elliptic.P384(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	edPublic, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	good := derOf(t, &newReleaseKey(t).PublicKey)
	other := derOf(t, &newReleaseKey(t).PublicKey)
	for name, value := range map[string]string{
		"no separator":         good,
		"no version":           ":" + good,
		"no key":               "1:",
		"version not a number": "one:" + good,
		"negative version":     "-1:" + good,
		"version above uint32": "4294967296:" + good,
		"key not base64":       "1:not base64!",
		"key PEM, not DER":     "1:" + pemOf(t, &newReleaseKey(t).PublicKey),
		"key not a public key": "1:" + base64.StdEncoding.EncodeToString([]byte("x")),
		"another curve":        "1:" + derOf(t, &p384.PublicKey),
		"another algorithm":    "1:" + derOf(t, edPublic),
		"repeated version":     "1:" + good + ",1:" + other,
		"empty entry":          "1:" + good + ",",
		"only a comma":         ",",
		"blank":                " ",
	} {
		t.Run(name, func(t *testing.T) {
			_, err := parseKeyList(value)
			if err == nil {
				t.Fatal("parseKeyList accepted it")
			}
			if !strings.HasPrefix(err.Error(), StatementKeyEnv+": entry ") {
				t.Fatalf("error = %q, want it to name %s and the entry", err, StatementKeyEnv)
			}
		})
	}
}

// TestVerifierKeysIgnoresTheOverrideOnProduction: the production host always gets the shipped
// keys, however its URL is spelled, and even a malformed override is not read there.
func TestVerifierKeysIgnoresTheOverrideOnProduction(t *testing.T) {
	shipped, injected := newReleaseKey(t), newReleaseKey(t)
	table := map[string]map[uint32]string{
		productionHost: {
			testVersion: pemOf(t, &shipped.PublicKey),
		},
	}
	for _, base := range []string{
		config.DefaultServerlessBaseURL,
		"https://API.SERVERLESS.RUNWARE.AI",
		"https://" + productionHost + ".",
		"https://" + productionHost + "..",
		"https://" + productionHost + ":443/v1",
		"https://user@" + productionHost,
		// Spellings net/http maps to the production host before it dials.
		"https://\uff21\uff30\uff29.serverless.runware.ai",
		"https://api\u3002serverless\u3002runware\u3002ai\u3002",
		"https://api.serverless.runw\u200bare.ai",
		"https://api.serverless.runware.ai\u00ad",
	} {
		for name, override := range map[string]string{
			"valid":     fmt.Sprintf("%d:%s", testVersion, derOf(t, &injected.PublicKey)),
			"malformed": "garbage",
		} {
			t.Run(base+" "+name, func(t *testing.T) {
				getenv := func(string) string {
					t.Errorf("the production host read %s", StatementKeyEnv)
					return override
				}
				keys, err := verifierKeys(table, base, getenv)
				if err != nil {
					t.Fatal(err)
				}
				if len(keys) != 1 || !keys[testVersion].Equal(&shipped.PublicKey) {
					t.Fatal("the production host did not get exactly the shipped key")
				}
			})
		}
	}
}

// TestVerifierKeysIgnoresTheOverrideWithoutAHost: a base URL with no host the CLI could reach
// does not take the override.
func TestVerifierKeysIgnoresTheOverrideWithoutAHost(t *testing.T) {
	override := fmt.Sprintf("%d:%s", testVersion, derOf(t, &newReleaseKey(t).PublicKey))
	for _, base := range []string{
		"",
		productionHost,
		"http://[::1",
	} {
		keys, err := verifierKeys(verificationKeys, base, envOf(override))
		if err != nil || len(keys) != 0 {
			t.Fatalf("%q: got %d keys, error %v", base, len(keys), err)
		}
	}
}

// TestPlatformHostRejectsAHostIDNACannotMap: a non-ASCII host IDNA refuses to map has no
// platform host, so it never takes the override and gets no shipped keys.
func TestPlatformHostRejectsAHostIDNACannotMap(t *testing.T) {
	if got := platformHost("https://⒈.com"); got != "" {
		t.Fatalf("platformHost = %q, want \"\"", got)
	}
}
