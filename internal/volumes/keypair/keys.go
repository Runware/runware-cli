package keypair

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"net/url"
	"strings"
)

const productionHost = "api.serverless.runware.ai"

// verificationKeys maps a platform's serverless API host to the PEM public keys of its
// statement signing key, by key version (current and next).
var verificationKeys = map[string]map[uint32]string{
	productionHost: {},
}

// platformHost is the lower-cased host of baseURL, without a trailing root dot, or "" when it does not parse.
func platformHost(baseURL string) string {
	u, err := url.Parse(baseURL)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
}

// selectKeys returns the keys of the platform whose API host is the host of baseURL, or
// nil for any other host.
func selectKeys(table map[string]map[uint32]string, baseURL string) map[uint32]string {
	return table[platformHost(baseURL)]
}

func parseKeys(pems map[uint32]string) (map[uint32]*ecdsa.PublicKey, error) {
	keys := make(map[uint32]*ecdsa.PublicKey, len(pems))
	for version, text := range pems {
		block, _ := pem.Decode([]byte(text))
		if block == nil || block.Type != "PUBLIC KEY" {
			return nil, fmt.Errorf("verification key %d is not a PEM public key", version)
		}
		parsed, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			return nil, fmt.Errorf("verification key %d: %w", version, err)
		}
		key, ok := parsed.(*ecdsa.PublicKey)
		if !ok || key.Curve != elliptic.P256() {
			return nil, fmt.Errorf("verification key %d is not a P-256 key", version)
		}
		keys[version] = key
	}
	return keys, nil
}
