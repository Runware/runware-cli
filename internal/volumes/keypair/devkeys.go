//go:build volumesdevkeys

package keypair

// devKeysEnabled is true in a local development build (make build), which
// reads StatementKeyEnv for a host other than production.
const devKeysEnabled = true
