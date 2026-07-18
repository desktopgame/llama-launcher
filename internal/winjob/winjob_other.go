//go:build !windows

package winjob

// SetupKillOnClose is a no-op on non-Windows platforms, where orphaned
// children are handled well enough by process groups and signal handling.
func SetupKillOnClose() error {
	return nil
}
