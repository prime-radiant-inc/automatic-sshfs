// Package launchd generates the asshfs launchd agent plist and loads/unloads it.
package launchd

import (
	"fmt"
	"os"
	"os/exec"
)

const label = "io.asshfs.reconcile"

// Plist returns the launchd agent plist XML for a job that runs
// `binPath reconcile` every 15 seconds, and once at load.
// We use StartInterval instead of WatchPaths because launchd WatchPaths
// does not reliably fire on Unix socket file creation/removal — it only
// fires on regular file vnode changes. A 15-second poll is cheap (reconcile
// is idempotent and fast) and reliably catches new/closed SSH sessions.
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
    <key>StartInterval</key>
    <integer>15</integer>
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
