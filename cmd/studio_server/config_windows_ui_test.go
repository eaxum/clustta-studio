package main

import "testing"

func TestValidWindowsUIMode(t *testing.T) {
	tests := map[string]bool{
		WindowsUIModeConsole:  true,
		WindowsUIModeTray:     true,
		WindowsUIModeHeadless: true,
		"invalid":             false,
		"":                    false,
	}

	for mode, expected := range tests {
		if actual := validWindowsUIMode(mode); actual != expected {
			t.Fatalf("validWindowsUIMode(%q) = %t, want %t", mode, actual, expected)
		}
	}
}

func TestLoadWindowsUIModeFromEnvironment(t *testing.T) {
	t.Setenv("WINDOWS_UI_MODE", " TRAY ")

	if mode := loadWindowsUIMode(); mode != WindowsUIModeTray {
		t.Fatalf("loadWindowsUIMode() = %q, want %q", mode, WindowsUIModeTray)
	}
}
