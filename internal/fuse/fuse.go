// Package fuse builds sshfs mount and umount commands. It does not execute
// them itself (the cli package runs them) so the command construction is
// unit-testable without FUSE installed.
package fuse

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// MountArgs describes a mount operation.
type MountArgs struct {
	Host        string // ssh Host alias (used for the remotepath target label only)
	User        string // resolved user, may be empty
	HostName    string // resolved hostname
	Port        string // resolved port
	ControlPath string // master socket to reuse
	MountPoint  string // local mount path
}

// MountCmd returns the sshfs command that mounts the remote root (/) at the
// given mount point, reusing the existing ControlMaster socket so no new
// authentication is needed.
func MountCmd(a MountArgs) *exec.Cmd {
	target := fmt.Sprintf("%s:/", a.HostName)
	if a.User != "" {
		target = fmt.Sprintf("%s@%s:/", a.User, a.HostName)
	}
	// Options order is stable for testability.
	// Note: we do NOT pass -o reconnect. With sshfs 2.9 + FUSE-T, reconnect
	// causes a busy-spin in fuse_chan_recv on long-lived mounts when the
	// underlying connection has intermittent issues, eating 100% CPU.
	// Instead, if the connection drops, the mount fails and reconcile
	// remounts on the next socket event.
	opts := []string{
		"-o", "ControlPath=" + a.ControlPath,
		"-o", "ControlMaster=no",
		"-o", "port=" + a.Port,
	}
	args := append([]string{target, a.MountPoint}, opts...)
	return exec.Command("sshfs", args...)
}

// UnmountCmd returns the standard umount for a mount point.
func UnmountCmd(mountPoint string) *exec.Cmd {
	return exec.Command("umount", mountPoint)
}

// ForceUnmountCmd returns a forced umount (umount -f), used as a fallback when
// the mount is busy.
func ForceUnmountCmd(mountPoint string) *exec.Cmd {
	return exec.Command("umount", "-f", mountPoint)
}

// MkdirAll creates the mount point directory (and parents) with 0755.
func MkdirAll(mountPoint string) error {
	if err := os.MkdirAll(mountPoint, 0o755); err != nil {
		return fmt.Errorf("fuse: mkdir %s: %w", mountPoint, err)
	}
	return nil
}

// RemoveIfEmpty removes the mount point directory if it is empty, ignoring
// errors. Used after a successful unmount to keep ~/sshfs tidy.
func RemoveIfEmpty(mountPoint string) {
	_ = os.Remove(filepath.Clean(mountPoint))
}
