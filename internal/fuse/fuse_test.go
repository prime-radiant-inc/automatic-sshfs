package fuse

import (
	"strings"
	"testing"
)

func TestMountCmd(t *testing.T) {
	args := MountArgs{
		Host: "web", User: "deploy", HostName: "10.0.0.1", Port: "2222",
		ControlPath: "/tmp/cm/abc123", MountPoint: "/Users/jesse/sshfs/web",
	}
	cmd := MountCmd(args)
	s := cmd.String()
	// sshfs [user@]host:remotepath mountpoint -o ...
	if !strings.Contains(s, "sshfs") {
		t.Errorf("cmd = %q, want sshfs", s)
	}
	if !strings.Contains(s, "deploy@10.0.0.1:/") {
		t.Errorf("cmd = %q, want deploy@10.0.0.1:/", s)
	}
	if !strings.Contains(s, "/Users/jesse/sshfs/web") {
		t.Errorf("cmd = %q, want mount point", s)
	}
	// Must reuse the master socket, not open a new connection.
	if !strings.Contains(s, "ControlPath=/tmp/cm/abc123") {
		t.Errorf("cmd = %q, want ControlPath=/tmp/cm/abc123", s)
	}
	if !strings.Contains(s, "ControlMaster=no") {
		t.Errorf("cmd = %q, want ControlMaster=no", s)
	}
	if !strings.Contains(s, "port=2222") {
		t.Errorf("cmd = %q, want port=2222", s)
	}
}

func TestMountCmdWithoutUser(t *testing.T) {
	args := MountArgs{
		Host: "web", HostName: "10.0.0.1", Port: "22",
		ControlPath: "/tmp/cm/x", MountPoint: "/mp/web",
	}
	cmd := MountCmd(args)
	s := cmd.String()
	// No user prefix when User is empty.
	if strings.Contains(s, "@10.0.0.1") {
		t.Errorf("cmd = %q, did not want user@ prefix", s)
	}
	if !strings.Contains(s, "10.0.0.1:/") {
		t.Errorf("cmd = %q, want 10.0.0.1:/", s)
	}
}

func TestUnmountCmd(t *testing.T) {
	cmd := UnmountCmd("/Users/jesse/sshfs/web")
	s := cmd.String()
	if !strings.Contains(s, "umount") {
		t.Errorf("cmd = %q, want umount", s)
	}
	if !strings.Contains(s, "/Users/jesse/sshfs/web") {
		t.Errorf("cmd = %q, want mount point", s)
	}
}

func TestForceUnmountCmd(t *testing.T) {
	cmd := ForceUnmountCmd("/Users/jesse/sshfs/web")
	s := cmd.String()
	if !strings.Contains(s, "umount -f") {
		t.Errorf("cmd = %q, want umount -f", s)
	}
}
