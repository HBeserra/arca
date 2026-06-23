// Package desktop launches the host operating system to open a file in its default
// application or reveal it in the system file manager. It works on macOS, Windows
// and Linux by shelling out to the platform's standard launcher — there is no
// portable Go API for this, so each OS gets its documented command.
package desktop

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// Open opens path in the OS default application for its file type.
func Open(path string) error {
	if err := checkExists(path); err != nil {
		return err
	}
	name, args := openArgs(runtime.GOOS, path)
	return run(name, args, false)
}

// Reveal shows path in the system file manager, selecting the file where the
// platform supports it (Finder on macOS, Explorer on Windows). On Linux it asks
// the file manager to highlight the file via the FileManager1 D-Bus interface and
// falls back to opening the containing folder.
func Reveal(path string) error {
	if err := checkExists(path); err != nil {
		return err
	}
	switch runtime.GOOS {
	case "darwin":
		return run("open", []string{"-R", path}, false)
	case "windows":
		// explorer returns exit code 1 even when it succeeds, so don't treat a
		// non-zero exit as a failure here.
		return run("explorer", []string{"/select," + path}, true)
	default:
		// Most modern Linux file managers implement org.freedesktop.FileManager1,
		// whose ShowItems selects the file. Fall back to opening the folder.
		if run("dbus-send", dbusShowItemsArgs(path), false) == nil {
			return nil
		}
		return run("xdg-open", []string{filepath.Dir(path)}, false)
	}
}

// openArgs is the per-OS command to open a file with its default application.
// Split out as a pure function so the platform mapping is unit-testable.
func openArgs(goos, path string) (name string, args []string) {
	switch goos {
	case "darwin":
		return "open", []string{path}
	case "windows":
		// `cmd /c start "" <path>`: the empty "" is start's window-title argument,
		// which keeps a quoted path from being consumed as the title.
		return "cmd", []string{"/c", "start", "", path}
	default: // linux, *bsd
		return "xdg-open", []string{path}
	}
}

// dbusShowItemsArgs builds the dbus-send call that asks the file manager to reveal
// (and select) path.
func dbusShowItemsArgs(path string) []string {
	return []string{
		"--session",
		"--dest=org.freedesktop.FileManager1",
		"--type=method_call",
		"/org/freedesktop/FileManager1",
		"org.freedesktop.FileManager1.ShowItems",
		"array:string:file://" + path,
		"string:",
	}
}

// run launches name with args and waits for it to start/finish. When ignoreExit is
// set, a non-zero exit code from a process that did run is treated as success
// (Windows Explorer's /select quirk); a missing binary is still reported.
func run(name string, args []string, ignoreExit bool) error {
	if err := exec.Command(name, args...).Run(); err != nil {
		var exitErr *exec.ExitError
		if ignoreExit && errors.As(err, &exitErr) {
			return nil // the process ran; only its exit code was non-zero
		}
		return fmt.Errorf("desktop: %s: %w", name, err)
	}
	return nil
}

func checkExists(path string) error {
	if path == "" {
		return fmt.Errorf("desktop: empty path")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("desktop: file unavailable: %w", err)
	}
	return nil
}
