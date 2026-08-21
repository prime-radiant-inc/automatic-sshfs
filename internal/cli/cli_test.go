package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jesse/automatic-sshfs/internal/launchd"
)

func TestNoArgsReturns2(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs"}, &out, &errOut)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "usage") {
		t.Errorf("stderr = %q, want usage", errOut.String())
	}
}

func TestUnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "bogus"}, &out, &errOut)
	if code != 2 {
		t.Errorf("code = %d, want 2", code)
	}
	if !strings.Contains(errOut.String(), "unknown subcommand") {
		t.Errorf("stderr = %q", errOut.String())
	}
}

func TestHelpReturns0(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "--help"}, &out, &errOut)
	if code != 0 {
		t.Errorf("code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "reconcile") {
		t.Errorf("stdout = %q", out.String())
	}
}

// Reconcile with an empty/missing ssh config should succeed (no-op) with exit 0,
// not crash. This is the smoke test for the wiring.
func TestReconcileNoConfig(t *testing.T) {
	// Point HOME at a temp dir with no ~/.ssh/config so the reconcile is a no-op.
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "reconcile"}, &out, &errOut)
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, errOut.String())
	}
}

func TestListNoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "list"}, &out, &errOut)
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, errOut.String())
	}
}

func TestInstallSmoke(t *testing.T) {
	// Use a temp HOME so we don't touch the real LaunchAgents dir.
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "install"}, &out, &errOut)
	// If prerequisites (sshfs/FUSE) are missing, install returns 1 with a
	// helpful message — that's the expected path on machines without sshfs.
	if code == 1 {
		if errOut.String() == "" {
			t.Errorf("stderr empty on install failure")
		}
		return
	}
	// install will try to launchctl load, which may fail in a sandbox/CI; that's
	// okay as long as the plist was written and the managed config was created.
	if !strings.Contains(out.String(), "asshfs.conf") {
		t.Errorf("stdout = %q, want asshfs.conf mention", out.String())
	}
	// Plist file should exist.
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", "io.asshfs.reconcile.plist")
	if _, err := os.Stat(plist); err != nil {
		t.Errorf("plist not written: %v", err)
	}
}

func TestInstallMissingPrereqs(t *testing.T) {
	// Use a temp HOME so we don't touch the real LaunchAgents dir.
	t.Setenv("HOME", t.TempDir())
	// Ensure sshfs is not found in PATH.
	t.Setenv("PATH", "/usr/bin:/bin")
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "install"}, &out, &errOut)
	if code != 1 {
		t.Errorf("code = %d, want 1; stderr=%q", code, errOut.String())
	}
	// The missing-sshfs message should be printed to stderr.
	if !strings.Contains(errOut.String(), "SSHFS is required") {
		t.Errorf("stderr = %q, want SSHFS missing message", errOut.String())
	}
}

func TestInstallDetectsControlPath(t *testing.T) {
	// Set up a temp HOME with a custom ControlPath in ~/.ssh/config.
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(sshDir, 0o700); err != nil {
		t.Fatal(err)
	}
	customDir := filepath.Join(home, ".ssh", "s")
	configContent := "Host *\n    ControlMaster auto\n    ControlPath " + customDir + "/%C\n"
	if err := os.WriteFile(filepath.Join(sshDir, "config"), []byte(configContent), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)

	// detectControlPathDir should extract the directory from the config.
	got := detectControlPathDir()
	if got != customDir {
		t.Errorf("detectControlPathDir = %q, want %q", got, customDir)
	}

	// The plist's WatchPaths should reference the detected directory.
	plist := launchd.Plist(got, "/usr/local/bin/asshfs")
	if !strings.Contains(plist, customDir) {
		t.Errorf("plist does not contain custom ControlPath dir %q:\n%s", customDir, plist)
	}
}

func TestInstallDetectsControlPathFallback(t *testing.T) {
	// No ControlPath in config → detectControlPathDir returns "".
	t.Setenv("HOME", t.TempDir())
	got := detectControlPathDir()
	if got != "" {
		t.Errorf("detectControlPathDir = %q, want empty (no ControlPath)", got)
	}
}

func TestInstallDetectsControlPathTilde(t *testing.T) {
	// ControlPath with ~/ should expand to the home directory.
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	os.MkdirAll(sshDir, 0o700)
	configContent := "Host *\n    ControlPath ~/.ssh/cm/%C\n"
	os.WriteFile(filepath.Join(sshDir, "config"), []byte(configContent), 0o600)
	t.Setenv("HOME", home)
	got := detectControlPathDir()
	want := filepath.Join(home, ".ssh", "cm")
	if got != want {
		t.Errorf("detectControlPathDir = %q, want %q (tilde not expanded)", got, want)
	}
}

func TestUninstallSmoke(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	_ = RunWith([]string{"asshfs", "uninstall"}, &out, &errOut)
	// Should not crash; plist (if any) removed.
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", "io.asshfs.reconcile.plist")
	if _, err := os.Stat(plist); err == nil {
		// It's fine if it never existed; only fail if it still exists.
		t.Errorf("plist still present after uninstall")
	}
}

func TestEnsureIncludeCreatesFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	err := ensureInclude(cfgPath, "Include asshfs.conf")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfgPath)
	if !strings.Contains(string(data), "Include asshfs.conf") {
		t.Errorf("config = %q, want Include line", string(data))
	}
}

func TestEnsureIncludeAppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	os.WriteFile(cfgPath, []byte("Host web\n    HostName 10.0.0.1\n"), 0o644)
	err := ensureInclude(cfgPath, "Include asshfs.conf")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfgPath)
	s := string(data)
	if !strings.Contains(s, "Host web") {
		t.Errorf("existing config lost: %q", s)
	}
	if !strings.Contains(s, "Include asshfs.conf") {
		t.Errorf("Include line not added: %q", s)
	}
}

func TestEnsureIncludeIdempotent(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	// Add twice — should only appear once.
	ensureInclude(cfgPath, "Include asshfs.conf")
	ensureInclude(cfgPath, "Include asshfs.conf")
	data, _ := os.ReadFile(cfgPath)
	s := string(data)
	if strings.Count(s, "Include asshfs.conf") != 1 {
		t.Errorf("Include line appears %d times, want 1: %q", strings.Count(s, "Include asshfs.conf"), s)
	}
}

func TestRemoveInclude(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	content := "Host web\n    HostName 10.0.0.1\nInclude asshfs.conf\nHost db\n    HostName db.example.com\n"
	os.WriteFile(cfgPath, []byte(content), 0o644)
	err := removeInclude(cfgPath, "Include asshfs.conf")
	if err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(cfgPath)
	s := string(data)
	if strings.Contains(s, "Include asshfs.conf") {
		t.Errorf("Include line still present: %q", s)
	}
	if !strings.Contains(s, "Host web") || !strings.Contains(s, "Host db") {
		t.Errorf("other config lost: %q", s)
	}
}

func TestRemoveIncludeMissing(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config")
	os.WriteFile(cfgPath, []byte("Host web\n    HostName 10.0.0.1\n"), 0o644)
	// Should not error even if Include line not present.
	err := removeInclude(cfgPath, "Include asshfs.conf")
	if err != nil {
		t.Errorf("removeInclude on missing line should not error: %v", err)
	}
}

func TestInstallCreatesManagedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "install"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("code = %d, want 0; stderr=%q", code, errOut.String())
	}
	// asshfs.conf should exist with the ControlMaster directives.
	asshfsConf := filepath.Join(home, ".ssh", "asshfs.conf")
	data, err := os.ReadFile(asshfsConf)
	if err != nil {
		t.Fatalf("asshfs.conf not written: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, "ControlMaster auto") {
		t.Errorf("asshfs.conf missing ControlMaster: %q", s)
	}
	if !strings.Contains(s, "ControlPath") {
		t.Errorf("asshfs.conf missing ControlPath: %q", s)
	}
	// ~/.ssh/config should contain the Include line.
	sshConfig := filepath.Join(home, ".ssh", "config")
	cfgData, err := os.ReadFile(sshConfig)
	if err != nil {
		t.Fatalf("ssh config not written: %v", err)
	}
	if !strings.Contains(string(cfgData), "Include asshfs.conf") {
		t.Errorf("ssh config missing Include: %q", string(cfgData))
	}
}

func TestUninstallRemovesManagedConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Install first, then uninstall.
	RunWith([]string{"asshfs", "install"}, &bytes.Buffer{}, &bytes.Buffer{})
	RunWith([]string{"asshfs", "uninstall"}, &bytes.Buffer{}, &bytes.Buffer{})
	// asshfs.conf should be gone.
	asshfsConf := filepath.Join(home, ".ssh", "asshfs.conf")
	if _, err := os.Stat(asshfsConf); err == nil {
		t.Errorf("asshfs.conf still exists after uninstall")
	}
	// ssh config should not contain the Include line.
	sshConfig := filepath.Join(home, ".ssh", "config")
	if data, err := os.ReadFile(sshConfig); err == nil {
		if strings.Contains(string(data), "Include asshfs.conf") {
			t.Errorf("Include line still in ssh config after uninstall: %q", string(data))
		}
	}
}
