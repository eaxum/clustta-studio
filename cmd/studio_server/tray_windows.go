//go:build windows

package main

import (
	_ "embed"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/getlantern/systray"
)

//go:embed icon.ico
var appIcon []byte

// initDesktop performs Windows-specific initialization:
// - Changes working directory to exe location (for Start Menu shortcuts)
// - Sets up file logging (since windowsgui hides console)
// Only runs in production builds (DesktopMode == "true"), not during dev.
func initDesktop() {
	if DesktopMode != "true" {
		return
	}

	// Change working directory to the executable's directory so studio_config.json is found
	if dir := exeDir(); dir != "." {
		os.Chdir(dir)
	}

	// Set up file logging so output is visible even when running as a windowsgui app
	logFile, err := os.OpenFile("studio_server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		log.SetOutput(logFile)
		// Note: we don't defer close here since this runs for the lifetime of the app
	}
}

// runWithTray starts the server and shows a system tray icon with Quit and Restart options.
func runWithTray(startServer func()) {
	systray.Run(func() {
		onTrayReady(startServer)
	}, func() {
		log.Println("System tray exited")
	})
}

// onTrayReady sets up tray menu items and starts the server.
func onTrayReady(startServer func()) {
	systray.SetIcon(appIcon)
	systray.SetTitle("Clustta Studio")
	systray.SetTooltip("Clustta Studio Server v" + Version)

	mShowLog := systray.AddMenuItem("Show Log", "Open the server log file")
	mRestart := systray.AddMenuItem("Restart", "Restart the server")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Stop the server and exit")

	go startServer()

	go func() {
		for {
			select {
			case <-mShowLog.ClickedCh:
				logPath := filepath.Join(exeDir(), "studio_server.log")
				exec.Command("cmd", "/c", "start", "", logPath).Start()

			case <-mRestart.ClickedCh:
				log.Println("Restarting server...")
				exe, err := os.Executable()
				if err != nil {
					log.Printf("Failed to get executable path: %v", err)
					continue
				}
				cmd := exec.Command(exe, os.Args[1:]...)
				cmd.Stdout = os.Stdout
				cmd.Stderr = os.Stderr
				cmd.Dir = exeDir()
				cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
				if err := cmd.Start(); err != nil {
					log.Printf("Failed to restart: %v", err)
					continue
				}
				systray.Quit()
				os.Exit(0)

			case <-mQuit.ClickedCh:
				log.Println("Quitting...")
				systray.Quit()
				os.Exit(0)
			}
		}
	}()
}
