// Package paths resolves the filesystem locations asshfs uses.
package paths

import (
	"os"
	"path/filepath"
)

// MountRoot is the parent of all per-host mount points, e.g. ~/sshfs.
func MountRoot() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "sshfs"), nil
}

// MountPoint returns the per-host mount path, e.g. ~/sshfs/<host>.
func MountPoint(host string) (string, error) {
	root, err := MountRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, host), nil
}

// SocketDir is the control-socket directory, default ~/.ssh/cm.
// (The actual dir is whatever ControlPath resolves to; this default is used
// for WatchPaths when the user has not customized ControlPath.)
func SocketDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ssh", "cm"), nil
}

// LogPath returns ~/Library/Logs/asshfs.log.
func LogPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Logs", "asshfs.log"), nil
}

// PlistPath returns the launchd agent plist path.
func PlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "io.asshfs.reconcile.plist"), nil
}
