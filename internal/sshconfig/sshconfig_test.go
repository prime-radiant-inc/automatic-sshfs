package sshconfig

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTemp(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestHostsBasic(t *testing.T) {
	cfg := writeTemp(t, "config", `Host web
    HostName 10.0.0.1
    User deploy
Host db
    HostName db.example.com
`)
	got, err := Hosts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web", "db"}
	if !equalSlices(got, want) {
		t.Errorf("Hosts = %v, want %v", got, want)
	}
}

func TestHostsMultiValue(t *testing.T) {
	cfg := writeTemp(t, "config", `Host web web2 alias
    HostName 10.0.0.1
`)
	got, err := Hosts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web", "web2", "alias"}
	if !equalSlices(got, want) {
		t.Errorf("Hosts = %v, want %v", got, want)
	}
}

func TestHostsSkipsWildcards(t *testing.T) {
	cfg := writeTemp(t, "config", `Host * !bad
    User default
Host web
    HostName 10.0.0.1
Host *.example
    User foo
`)
	got, err := Hosts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Only "web" is concrete; * and *.example are patterns.
	want := []string{"web"}
	if !equalSlices(got, want) {
		t.Errorf("Hosts = %v, want %v", got, want)
	}
}

func TestHostsInclude(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "config")
	incl := filepath.Join(dir, "extra")
	os.WriteFile(incl, []byte("Host included\n    HostName 1.2.3.4\n"), 0600)
	os.WriteFile(main, []byte("Host web\n    HostName 10.0.0.1\nInclude extra\n"), 0600)
	got, err := Hosts(main)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"web", "included"}
	if !equalSlices(got, want) {
		t.Errorf("Hosts = %v, want %v", got, want)
	}
}

func TestHostsIncludeCycle(t *testing.T) {
	// A config file that includes itself must not cause unbounded recursion.
	// Hosts should return the hosts declared before the cycle without crashing.
	dir := t.TempDir()
	main := filepath.Join(dir, "config")
	content := "Host web\n    HostName 10.0.0.1\nInclude config\n"
	if err := os.WriteFile(main, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := Hosts(main)
	if err != nil {
		t.Fatalf("Hosts returned error on include cycle: %v", err)
	}
	// "web" is declared before the self-include; the cycle is skipped.
	want := []string{"web"}
	if !equalSlices(got, want) {
		t.Errorf("Hosts = %v, want %v", got, want)
	}
}

func TestHostsMissingFile(t *testing.T) {
	_, err := Hosts("/nonexistent/path/config")
	if err == nil {
		t.Error("expected error for missing file")
	}
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
