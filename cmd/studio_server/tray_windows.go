//go:build windows

package main

import (
	_ "embed"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"

	"github.com/getlantern/systray"
)

//go:embed icon.ico
var appIcon []byte

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	user32               = syscall.NewLazyDLL("user32.dll")
	procAllocConsole     = kernel32.NewProc("AllocConsole")
	procGetConsoleWindow = kernel32.NewProc("GetConsoleWindow")
	procShowWindow       = user32.NewProc("ShowWindow")
	procGetSystemMenu    = user32.NewProc("GetSystemMenu")
	procDeleteMenu       = user32.NewProc("DeleteMenu")
)

const (
	SW_HIDE      = 0
	SW_SHOW      = 5
	SC_CLOSE     = 0xF060
	MF_BYCOMMAND = 0x0000
)

var consoleHwnd uintptr
var consoleVisible = true

func allocConsole() {
	procAllocConsole.Call()

	// Get console window handle
	hwnd, _, _ := procGetConsoleWindow.Call()
	consoleHwnd = hwnd

	// Remove the close button from the console window
	// This prevents accidental closing - use tray menu to quit
	if consoleHwnd != 0 {
		hMenu, _, _ := procGetSystemMenu.Call(consoleHwnd, 0)
		if hMenu != 0 {
			procDeleteMenu.Call(hMenu, SC_CLOSE, MF_BYCOMMAND)
		}
	}

	// Open CONOUT$ directly - GetStdHandle doesn't work reliably after AllocConsole
	// when the process was started as a GUI app without a console
	conout, err := os.OpenFile("CONOUT$", os.O_WRONLY, 0)
	if err == nil {
		os.Stdout = conout
		os.Stderr = conout
		log.SetOutput(conout)
	}
}

func showConsole() {
	if consoleHwnd != 0 {
		procShowWindow.Call(consoleHwnd, SW_SHOW)
		consoleVisible = true
	}
}

func hideConsole() {
	if consoleHwnd != 0 {
		procShowWindow.Call(consoleHwnd, SW_HIDE)
		consoleVisible = false
	}
}

// initDesktop performs Windows-specific initialization:
// - Changes working directory to exe location (for Start Menu shortcuts)
// - Creates a console window for log output
// Only runs in production builds (DesktopMode == "true"), not during dev.
func initDesktop() {
	if DesktopMode != "true" {
		return
	}

	// Change working directory to the executable's directory so studio_config.json is found
	if dir := exeDir(); dir != "." {
		os.Chdir(dir)
	}

	// Create a console window for output
	allocConsole()

	// Also log to file for persistence
	logFile, err := os.OpenFile("studio_server.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err == nil {
		// Write to both console and file
		log.SetOutput(io.MultiWriter(os.Stdout, logFile))
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

	mToggleConsole := systray.AddMenuItem("Toggle Console", "Show/hide the console window")
	mOpenLog := systray.AddMenuItem("Open Log File", "Open the log file in Notepad")
	mRestart := systray.AddMenuItem("Restart", "Restart the server")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Stop the server and exit")

	go startServer()

	go func() {
		for {
			select {
			case <-mToggleConsole.ClickedCh:
				if consoleVisible {
					hideConsole()
				} else {
					showConsole()
				}

			case <-mOpenLog.ClickedCh:
				logPath := filepath.Join(exeDir(), "studio_server.log")
				cmd := exec.Command("notepad.exe", logPath)
				if err := cmd.Start(); err != nil {
					log.Printf("Failed to open log file: %v", err)
				}

			case <-mRestart.ClickedCh:
				log.Println("Restarting server...")
				exe, err := os.Executable()
				if err != nil {
					log.Printf("Failed to get executable path: %v", err)
					continue
				}
				cmd := exec.Command(exe, os.Args[1:]...)
				cmd.Dir = exeDir()
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
