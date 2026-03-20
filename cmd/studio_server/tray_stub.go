//go:build !windows

package main

// initDesktop is a no-op on non-Windows platforms.
// Linux/Docker doesn't need chdir (no Start Menu shortcuts) or file logging (logs go to stdout).
func initDesktop() {}

// runWithTray on non-Windows platforms just runs the server directly (no tray).
func runWithTray(startServer func()) {
	startServer()
}
