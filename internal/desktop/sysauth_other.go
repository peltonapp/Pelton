//go:build !darwin

package desktop

// Windows and Linux have no system prompt Pelton can reach without a new
// dependency, so the app lock uses a passphrase of its own there.
func newSystemAuth() systemAuth { return nil }
