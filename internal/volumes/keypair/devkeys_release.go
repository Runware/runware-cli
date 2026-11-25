//go:build !volumesdevkeys

package keypair

// devKeysEnabled is false in a release build: StatementKeyEnv is never read,
// and every host gets only the keys the release ships.
const devKeysEnabled = false
