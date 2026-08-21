package paths

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMountPoint(t *testing.T) {
	mp, err := MountPoint("web")
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, "sshfs", "web")
	if mp != want {
		t.Errorf("MountPoint = %q, want %q", mp, want)
	}
}

func TestSocketDir(t *testing.T) {
	sd, err := SocketDir()
	if err != nil {
		t.Fatal(err)
	}
	home, _ := os.UserHomeDir()
	want := filepath.Join(home, ".ssh", "cm")
	if sd != want {
		t.Errorf("SocketDir = %q, want %q", sd, want)
	}
}

func TestLogPath(t *testing.T) {
	lp, err := LogPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(lp, "Library/Logs/asshfs.log") {
		t.Errorf("LogPath = %q, want suffix Library/Logs/asshfs.log", lp)
	}
}

func TestPlistPath(t *testing.T) {
	pp, err := PlistPath()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(pp, "Library/LaunchAgents/io.asshfs.reconcile.plist") {
		t.Errorf("PlistPath = %q", pp)
	}
}
