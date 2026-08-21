// Package launchd generates the asshfs launchd agent plist and loads/unloads it.
package launchd

import (
	"fmt"
	"os/exec"
)

const label = "io.asshfs.reconcile"

// Plist returns the launchd agent plist XML for a WatchPaths job that runs
// `binPath reconcile` whenever socketDir changes, and once at load.
func Plist(socketDir, binPath string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>reconcile</string>
    </array>
    <key>WatchPaths</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>/tmp/asshfs.out.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/asshfs.err.log</string>
</dict>
</plist>
`, label, binPath, socketDir)
}

// Load loads the plist with launchctl.
func Load(plistPath string) error {
	out, err := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl load: %v: %s", err, out)
	}
	return nil
}

// Unload unloads the plist with launchctl.
func Unload(plistPath string) error {
	out, err := exec.Command("launchctl", "unload", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl unload: %v: %s", err, out)
	}
	return nil
}
