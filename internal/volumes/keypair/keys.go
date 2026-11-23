package keypair

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/idna"
)

const productionHost = "api.serverless.runware.ai"

// StatementKeyEnv names the environment variable that gives the trusted statement
// verification keys for a non-production API host. Its value is a comma-separated list of
// <version>:<key> entries, where version is the decimal signing key version and key is the
// standard base64 of the DER (SubjectPublicKeyInfo) encoding of a P-256 public key; two
// entries carry the current and the next version during a rollover. Only a local
// development build, made with the volumesdevkeys build tag (make build), reads it; a
// release build never does, so the environment cannot change what a released CLI trusts.
// Even a development build ignores it when the serverless base URL is the production host.
const StatementKeyEnv = "RUNWARE_VOLUMES_STATEMENT_KEY"

// verificationKeys maps a platform's serverless API host to the PEM public keys of its
// statement signing key, by key version (current and next).
var verificationKeys = map[string]map[uint32]string{
	productionHost: {},
}

// platformHost is the host of baseURL as the HTTP client dials it, lower-cased and without
// trailing root dots. Like net/http, it maps a host that is not ASCII through IDNA, so a
// full-width or otherwise mapped spelling of a host is that host; an ASCII host (a name, an
// IPv4 or IPv6 literal) is kept as it is. It is "" when baseURL does not parse or a
// non-ASCII host does not map.
func platformHost(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if !isASCII(host) {
		if host, err = idna.Lookup.ToASCII(host); err != nil {
			return ""
		}
	}
	return strings.TrimRight(strings.ToLower(host), ".")
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

// selectKeys returns the keys of the platform whose API host is the host of baseURL, or
// nil for any other host.
func selectKeys(table map[string]map[uint32]string, baseURL string) map[uint32]string {
	return table[platformHost(baseURL)]
}

// verifierKeys returns the keys that verify statements for the platform at baseURL. A
// release build gives every host the shipped keys for that host, if any, and never calls
// getenv. In a development build the production host still gets only the shipped keys; any
// other host that parses gets the keys in StatementKeyEnv when it is set.
func verifierKeys(table map[string]map[uint32]string, baseURL string, getenv func(string) string) (map[uint32]*ecdsa.PublicKey, error) {
	host := platformHost(baseURL)
	if !devKeysEnabled || host == productionHost || host == "" {
		return parseKeys(selectKeys(table, baseURL))
	}
	if override := getenv(StatementKeyEnv); override != "" {
		return parseKeyList(override)
	}
	return parseKeys(selectKeys(table, baseURL))
}

// parseKeyList parses a StatementKeyEnv value. It refuses an empty entry, a version that is
// not a decimal uint32, a key that is not base64 of a P-256 public key, and a repeated version.
func parseKeyList(value string) (map[uint32]*ecdsa.PublicKey, error) {
	entries := strings.Split(value, ",")
	keys := make(map[uint32]*ecdsa.PublicKey, len(entries))
	for i, entry := range entries {
		versionText, keyText, ok := strings.Cut(strings.TrimSpace(entry), ":")
		if !ok || versionText == "" || keyText == "" {
			return nil, keyListError(i, errors.New("want <version>:<base64 public key>"))
		}
		version, err := strconv.ParseUint(versionText, 10, 32)
		if err != nil {
			return nil, keyListError(i, errors.New("the version is not a decimal number below 2^32"))
		}
		der, err := base64.StdEncoding.DecodeString(keyText)
		if err != nil {
			return nil, keyListError(i, errors.New("the key is not standard base64"))
		}
		if _, dup := keys[uint32(version)]; dup {
			return nil, keyListError(i, fmt.Errorf("version %d is given twice", version))
		}
		key, err := parsePublicKey(uint32(version), der)
		if err != nil {
			return nil, keyListError(i, err)
		}
		keys[uint32(version)] = key
	}
	return keys, nil
}

func keyListError(index int, err error) error {
	return fmt.Errorf("%s: entry %d: %w", StatementKeyEnv, index+1, err)
}

func parseKeys(pems map[uint32]string) (map[uint32]*ecdsa.PublicKey, error) {
	keys := make(map[uint32]*ecdsa.PublicKey, len(pems))
	for version, text := range pems {
		block, _ := pem.Decode([]byte(text))
		if block == nil || block.Type != "PUBLIC KEY" {
			return nil, fmt.Errorf("verification key %d is not a PEM public key", version)
		}
		key, err := parsePublicKey(version, block.Bytes)
		if err != nil {
			return nil, err
		}
		keys[version] = key
	}
	return keys, nil
}

// parsePublicKey parses der as the SubjectPublicKeyInfo of a P-256 key.
func parsePublicKey(version uint32, der []byte) (*ecdsa.PublicKey, error) {
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, fmt.Errorf("verification key %d: %w", version, err)
	}
	key, ok := parsed.(*ecdsa.PublicKey)
	if !ok || key.Curve != elliptic.P256() {
		return nil, fmt.Errorf("verification key %d is not a P-256 key", version)
	}
	return key, nil
}
