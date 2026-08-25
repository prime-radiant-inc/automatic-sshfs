// Package launchd generates the asshfs launchd agent plist and loads/unloads it.
package launchd

import (
	"fmt"
	"os"
	"os/exec"
)

const label = "io.asshfs.reconcile"

// Plist returns the launchd agent plist XML for a job that runs
// `binPath watch` as a long-running daemon. The watch command stays alive
// and polls every 15 seconds, which is more reliable than launchd's
// StartInterval (which doesn't fire for short-lived processes).
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
        <string>watch</string>
    </array>
    <key>KeepAlive</key>
    <true/>
    <key>RunAtLoad</key>
    <true/>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/local/bin:/opt/homebrew/bin:/usr/bin:/bin:/usr/sbin:/sbin</string>
        <key>HOME</key>
        <string>%s</string>
    </dict>
    <key>StandardOutPath</key>
    <string>/tmp/asshfs.out.log</string>
    <key>StandardErrorPath</key>
    <string>/tmp/asshfs.err.log</string>
</dict>
</plist>
`, label, binPath, homeDir())
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

// homeDir returns the user's home directory, or empty string on error.
func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return h
}
