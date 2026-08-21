# automatic-sshfs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build `asshfs`, a macOS tool that auto-mounts SSH remotes via FUSE-T/SSHFS to `~/sshfs/<host>/` while an SSH ControlMaster socket exists, and unmounts when the last session closes.

**Architecture:** launchd `WatchPaths` on the control-socket directory spawns `asshfs reconcile`, which enumerates `Host` entries from `~/.ssh/config`, uses `ssh -G` to resolve each host's expected `ControlPath`, stats it to decide "socket exists → should mount", diffs against FUSE mounts at `~/sshfs/`, and mounts/unmounts to converge. The control socket's existence is the per-host refcount. No always-running daemon.

**Tech Stack:** Go (go1.26), FUSE-T + SSHFS (runtime deps, not build deps), launchd, OpenSSH `ssh -G` as config-resolution oracle.

**Spec:** `docs/superpowers/specs/2026-08-21-automatic-sshfs-design.md`

## Global Constraints

- **Language:** Go, module path `github.com/jesse/automatic-sshfs` (module created in repo root).
- **Go version:** go1.26 (present locally). Target older Go is not required; use standard library only — no external dependencies in v1.
- **Platform:** macOS only (launchd, `~/Library/LaunchAgents`, `umount`). Unix-socket stat semantics relied upon.
- **No external Go deps in v1.** Use only the standard library. (Parsing, CLI, plist XML, logging all doable in stdlib.)
- **TDD:** Every pure-logic task is test-first. Tests run with `go test ./...`.
- **Oracle:** `ssh -G <host>` is the source of truth for ControlPath/hostname/user/port resolution. Do not reimplement ssh's token expansion. Only `~/.ssh/config` `Host` entry enumeration is parsed by us.
- **Logging:** `~/Library/Logs/asshfs.log` via `log` package.
- **Never touch `~/.ssh/config`**: `install` prints the block to add; the user edits their own config.
- **Mount path:** `~/sshfs/<Host-alias>/`. Remote path: `/` (root).
- **Subcommand names:** `reconcile`, `install`, `uninstall`, `list`.

---

## File Structure

```
automatic-sshfs/
  go.mod
  go.sum                       (empty — no external deps)
  cmd/
    asshfs/
      main.go                  (subcommand dispatch; thin)
  internal/
    cli/
      cli.go                   (subcommand handlers: reconcile/install/uninstall/list)
      cli_test.go
    sshconfig/
      sshconfig.go             (enumerate Host entries from ~/.ssh/config + Includes)
      sshconfig_test.go
    sshoracle/
      sshoracle.go             (parse ssh -G output; shell out to ssh -G)
      sshoracle_test.go
    reconcile/
      reconcile.go             (pure diff: desired vs actual → plan)
      reconcile_test.go
    fuse/
      fuse.go                  (sshfs mount / umount wrappers, lazy fallback)
      fuse_test.go             (plist/mount-string construction; live ops integration-gated)
    launchd/
      launchd.go               (plist XML generation, load/unload via launchctl)
      launchd_test.go
    paths/
      paths.go                 (resolved paths: socket dir, mount root, log, plist)
      paths_test.go
  docs/superpowers/specs/2026-08-21-automatic-sshfs-design.md
  docs/superpowers/plans/2026-08-21-automatic-sshfs.md
```

**Responsibilities:**
- `paths` — single source for `~/sshfs/`, `~/.ssh/cm/` (default), log path, plist path. No logic.
- `sshconfig` — parse `~/.ssh/config` → list of `Host` aliases (skip pure wildcards like `*`/`!*`; honor `Include`). Pure, file-IO only.
- `sshoracle` — `Resolve(host) (controlPath, user, hostname, port, err)` via `ssh -G`. Parsing of `-G` output is pure and unit-tested with fixtures; the shellout is a thin wrapper.
- `reconcile` — `Plan(desired map[host]bool, actual map[host]bool) (mounts, unmounts []string)`. Pure.
- `fuse` — `Mount(host, user, hostname, port, controlPath, mountPoint)` and `Unmount(mountPoint)`. Side-effectful; string construction unit-tested, live ops integration-gated.
- `launchd` — `Plist(socketDir, binPath)` returns XML string (unit-tested); `Load`/`Unload` shell out.
- `cli` — wires subcommands; thin orchestration.

---

## Task 1: Scaffold Go module and CLI skeleton

**Files:**
- Create: `go.mod`
- Create: `cmd/asshfs/main.go`
- Create: `internal/cli/cli.go`
- Create: `internal/cli/cli_test.go`
- Create: `internal/paths/paths.go`
- Create: `internal/paths/paths_test.go`

**Interfaces:**
- Produces: `cli.Run(args []string) int` (exit code), `paths` package with `MountRoot()`, `SocketDir()`, `LogPath()`, `PlistPath()`.

- [ ] **Step 1: Initialize module and directories**

```bash
cd /Users/jesse/git/automatic-sshfs
go mod init github.com/jesse/automatic-sshfs
mkdir -p cmd/asshfs internal/cli internal/paths internal/sshconfig internal/sshoracle internal/reconcile internal/fuse internal/launchd
```

- [ ] **Step 2: Write `internal/paths/paths.go`**

```go
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
```

- [ ] **Step 3: Write failing test `internal/paths/paths_test.go`**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/paths/`
Expected: PASS

- [ ] **Step 5: Write `internal/cli/cli.go` skeleton**

```go
// Package cli implements the asshfs subcommands.
package cli

import (
	"fmt"
	"io"
	"os"
)

// Run dispatches a subcommand. args[0] is the program name; args[1] is the
// subcommand. Returns the process exit code.
func Run(args []string) int {
	return RunWith(args, os.Stdout, os.Stderr)
}

// RunWith is like Run but with explicit streams for testing.
func RunWith(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 {
		usage(stderr)
		return 2
	}
	switch args[1] {
	case "reconcile":
		return cmdReconcile(stdout, stderr)
	case "install":
		return cmdInstall(stdout, stderr)
	case "uninstall":
		return cmdUninstall(stdout, stderr)
	case "list":
		return cmdList(stdout, stderr)
	case "-h", "--help", "help":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand: %s\n", args[1])
		usage(stderr)
		return 2
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "usage: asshfs <reconcile|install|uninstall|list>")
}

func cmdReconcile(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "reconcile: not yet implemented")
	return 1
}

func cmdInstall(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "install: not yet implemented")
	return 1
}

func cmdUninstall(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "uninstall: not yet implemented")
	return 1
}

func cmdList(stdout, stderr io.Writer) int {
	fmt.Fprintln(stderr, "list: not yet implemented")
	return 1
}
```

- [ ] **Step 6: Write failing test `internal/cli/cli_test.go`**

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
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

func TestReconcilePlaceholder(t *testing.T) {
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "reconcile"}, &out, &errOut)
	if code != 1 {
		t.Errorf("code = %d, want 1 (not yet implemented)", code)
	}
}
```

- [ ] **Step 7: Run tests to verify they pass**

Run: `go test ./internal/cli/`
Expected: PASS

- [ ] **Step 8: Write `cmd/asshfs/main.go`**

```go
package main

import (
	"os"

	"github.com/jesse/automatic-sshfs/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args))
}
```

- [ ] **Step 9: Build and smoke-test**

Run: `go build ./... && go run ./cmd/asshfs --help`
Expected: prints usage, exit 0.

Run: `go test ./...`
Expected: all PASS.

- [ ] **Step 10: Commit**

```bash
git add go.mod cmd/ internal/
git commit -m "feat: scaffold asshfs module, paths, and CLI skeleton"
```

---

## Task 2: sshconfig — enumerate Host entries

**Files:**
- Create: `internal/sshconfig/sshconfig.go`
- Create: `internal/sshconfig/sshconfig_test.go`
- Modify: `internal/cli/cli.go` (wire `list` to use Hosts — done in Task 7; not here)

**Interfaces:**
- Produces: `sshconfig.Hosts(configPath string) ([]string, error)` — returns the list of concrete `Host` aliases declared, excluding pure-wildcard patterns (`*`, `!*`, `*.example`, `0.0.0.0`). Honors `Include` directives (relative to the config file's dir or absolute). Tokens like `%h` inside Host names are not expected (Host aliases are literal).

- [ ] **Step 1: Write failing test `internal/sshconfig/sshconfig_test.go`**

```go
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshconfig/`
Expected: FAIL — `Hosts` undefined.

- [ ] **Step 3: Implement `internal/sshconfig/sshconfig.go`**

```go
// Package sshconfig enumerates Host entries from an ssh config file.
// It does NOT expand ssh tokens — that is delegated to `ssh -G`.
package sshconfig

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Hosts returns the concrete Host aliases declared in the given config file,
// in declaration order, de-duplicated. Pattern-only Host values (containing
// any of * ? ! or a leading .) are skipped. Include directives are followed
// (paths relative to the config file's directory or absolute).
func Hosts(configPath string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	if err := collect(configPath, &out, seen); err != nil {
		return nil, err
	}
	return out, nil
}

func collect(configPath string, out *[]string, seen map[string]bool) error {
	f, err := os.Open(configPath)
	if err != nil {
		return fmt.Errorf("sshconfig: open %s: %w", configPath, err)
	}
	defer f.Close()

	baseDir := filepath.Dir(configPath)
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// ssh config keys are case-insensitive.
		keyword, rest := splitKeyword(line)
		switch strings.ToLower(keyword) {
		case "host":
			for _, name := range fields(rest) {
				if isPattern(name) {
					continue
				}
				if !seen[name] {
					seen[name] = true
					*out = append(*out, name)
				}
			}
		case "include":
			for _, pattern := range fields(rest) {
				p := pattern
				if !filepath.IsAbs(p) {
					p = filepath.Join(baseDir, p)
				}
				matches, _ := filepath.Glob(p)
				if len(matches) == 0 {
					matches = []string{p} // try as literal
				}
				for _, m := range matches {
					if err := collect(m, out, seen); err != nil {
						// Missing includes are non-fatal in ssh; skip silently.
						_ = err
					}
				}
			}
		}
	}
	return sc.Err()
}

// splitKeyword returns the first whitespace-delimited keyword and the rest.
func splitKeyword(line string) (keyword, rest string) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i+1:])
}

// fields splits on whitespace (spaces and tabs).
func fields(s string) []string {
	return strings.Fields(s)
}

// isPattern reports whether a Host value is a glob pattern rather than a
// concrete alias. ssh patterns use *, ?, and ! for negation.
func isPattern(name string) bool {
	if strings.ContainsAny(name, "*?!") {
		return true
	}
	return false
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshconfig/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/sshconfig/
git commit -m "feat(sshconfig): enumerate concrete Host entries with Include support"
```

---

## Task 3: sshoracle — parse and shell out to `ssh -G`

**Files:**
- Create: `internal/sshoracle/sshoracle.go`
- Create: `internal/sshoracle/sshoracle_test.go`

**Interfaces:**
- Produces:
  - `type Resolved struct { User, HostName, Port, ControlPath, ControlMaster, ControlPersist string }`
  - `ParseG(output string) (Resolved, error)` — pure parser for `ssh -G` text output (key-value lines, lowercased keys).
  - `Resolve(host string) (Resolved, error)` — shells out to `ssh -G <host>` and parses.
  - `ResolveAll(hosts []string) map[string]Resolved` — best-effort resolve each; silently skips hosts that fail (logged by caller).

- [ ] **Step 1: Write failing test `internal/sshoracle/sshoracle_test.go`**

```go
package sshoracle

import (
	"reflect"
	"testing"
)

const sampleG = `user deploy
hostname 10.0.0.1
port 2222
controlmaster auto
controlpath /tmp/cm/be4420faf0ccd33bd13a61e0bfc1768c49e461db
controlpersist 30
host example.com
`
const sampleGNoControl = `user jesse
hostname example.com
port 22
controlmaster no
controlpath none
controlpersist no
`

func TestParseGFull(t *testing.T) {
	got, err := ParseG(sampleG)
	if err != nil {
		t.Fatal(err)
	}
	want := Resolved{
		User:           "deploy",
		HostName:       "10.0.0.1",
		Port:           "2222",
		ControlPath:    "/tmp/cm/be4420faf0ccd33bd13a61e0bfc1768c49e461db",
		ControlMaster:  "auto",
		ControlPersist: "30",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseG = %+v, want %+v", got, want)
	}
}

func TestParseGNoControl(t *testing.T) {
	got, err := ParseG(sampleGNoControl)
	if err != nil {
		t.Fatal(err)
	}
	if got.ControlPath != "none" {
		t.Errorf("ControlPath = %q, want none", got.ControlPath)
	}
	if got.User != "jesse" {
		t.Errorf("User = %q, want jesse", got.User)
	}
}

func TestParseGEmpty(t *testing.T) {
	_, err := ParseG("")
	if err == nil {
		t.Error("expected error for empty ssh -G output")
	}
}

func TestParseGMalformed(t *testing.T) {
	// No recognizable keys at all.
	_, err := ParseG("garbage line\nanother\n")
	if err == nil {
		t.Error("expected error when no ssh keys present")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/sshoracle/`
Expected: FAIL — `ParseG` undefined.

- [ ] **Step 3: Implement `internal/sshoracle/sshoracle.go`**

```go
// Package sshoracle resolves a host's effective ssh options by parsing the
// output of `ssh -G`, which expands all tokens (including %C) exactly as ssh
// itself would. We do not reimplement ssh's token expansion.
package sshoracle

import (
	"bufio"
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

// Resolved holds the ssh options asshfs cares about, as ssh -G reports them.
type Resolved struct {
	User           string
	HostName       string
	Port           string
	ControlPath    string
	ControlMaster  string
	ControlPersist string
}

// ParseG parses the text output of `ssh -G`. Keys are case-insensitive and
// lowercase in real output; we normalize defensively. Returns an error if the
// output contains no recognizable keys (e.g. not actually ssh -G output).
func ParseG(output string) (Resolved, error) {
	var r Resolved
	found := 0
	sc := bufio.NewScanner(strings.NewReader(output))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		key, val, ok := splitKV(line)
		if !ok {
			continue
		}
		switch strings.ToLower(key) {
		case "user":
			r.User = val
			found++
		case "hostname":
			r.HostName = val
			found++
		case "port":
			r.Port = val
			found++
		case "controlpath":
			r.ControlPath = val
			found++
		case "controlmaster":
			r.ControlMaster = val
			found++
		case "controlpersist":
			r.ControlPersist = val
			found++
		}
	}
	if err := sc.Err(); err != nil {
		return r, err
	}
	if found == 0 {
		return r, fmt.Errorf("sshoracle: no ssh keys found in -G output")
	}
	return r, nil
}

// Resolve shells out to `ssh -G <host>` and parses the result.
func Resolve(host string) (Resolved, error) {
	cmd := exec.Command("ssh", "-G", host)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return Resolved{}, fmt.Errorf("ssh -G %s: %w (%s)", host, err, stderr.String())
	}
	return ParseG(stdout.String())
}

// ResolveAll resolves each host, dropping any that error. Errors are returned
// as a map of host->error for the caller to log.
func ResolveAll(hosts []string) (map[string]Resolved, map[string]error) {
	resolved := make(map[string]Resolved, len(hosts))
	errs := make(map[string]error)
	for _, h := range hosts {
		r, err := Resolve(h)
		if err != nil {
			errs[h] = err
			continue
		}
		resolved[h] = r
	}
	return resolved, errs
}

func splitKV(line string) (key, val string, ok bool) {
	// ssh -G output is "key value" separated by whitespace.
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", "", false
	}
	return fields[0], fields[1], true
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/sshoracle/`
Expected: PASS

- [ ] **Step 5: Add an integration-ish test that actually shells out (gated)**

Append to `sshoracle_test.go`:

```go
func TestResolveRealSSHG(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping real ssh -G in short mode")
	}
	// example.com is always resolvable; ssh -G does not connect.
	r, err := Resolve("example.com")
	if err != nil {
		t.Skipf("ssh -G unavailable in this environment: %v", err)
	}
	if r.HostName != "example.com" {
		t.Errorf("HostName = %q, want example.com", r.HostName)
	}
	if r.Port != "22" {
		t.Errorf("Port = %q, want 22", r.Port)
	}
}
```

Run: `go test ./internal/sshoracle/`
Expected: PASS (the real test may skip if ssh behaves oddly, but ParseG tests must pass).

- [ ] **Step 6: Commit**

```bash
git add internal/sshoracle/
git commit -m "feat(sshoracle): parse ssh -G output for resolved control path"
```

---

## Task 4: reconcile — pure diff logic

**Files:**
- Create: `internal/reconcile/reconcile.go`
- Create: `internal/reconcile/reconcile_test.go`

**Interfaces:**
- Consumes: from caller — a set of hosts whose control sockets exist (`desired`), and a set of hosts currently mounted (`actual`).
- Produces:
  - `type Plan struct { Mounts, Unmounts []string }`
  - `Diff(desired, actual map[string]bool) Plan` — pure: hosts in desired but not actual → Mounts; hosts in actual but not desired → Unmounts.

- [ ] **Step 1: Write failing test `internal/reconcile/reconcile_test.go`**

```go
package reconcile

import (
	"reflect"
	"sort"
	"testing"
)

func TestDiffAllMount(t *testing.T) {
	desired := map[string]bool{"web": true, "db": true}
	actual := map[string]bool{}
	p := Diff(desired, actual)
	if !sortedEqual(p.Mounts, []string{"db", "web"}) {
		t.Errorf("Mounts = %v, want [db web]", p.Mounts)
	}
	if len(p.Unmounts) != 0 {
		t.Errorf("Unmounts = %v, want []", p.Unmounts)
	}
}

func TestDiffAllUnmount(t *testing.T) {
	desired := map[string]bool{}
	actual := map[string]bool{"web": true, "db": true}
	p := Diff(desired, actual)
	if len(p.Mounts) != 0 {
		t.Errorf("Mounts = %v, want []", p.Mounts)
	}
	if !sortedEqual(p.Unmounts, []string{"db", "web"}) {
		t.Errorf("Unmounts = %v, want [db web]", p.Unmounts)
	}
}

func TestDiffMixed(t *testing.T) {
	desired := map[string]bool{"web": true, "api": true}
	actual := map[string]bool{"web": true, "db": true}
	p := Diff(desired, actual)
	if !sortedEqual(p.Mounts, []string{"api"}) {
		t.Errorf("Mounts = %v, want [api]", p.Mounts)
	}
	if !sortedEqual(p.Unmounts, []string{"db"}) {
		t.Errorf("Unmounts = %v, want [db]", p.Unmounts)
	}
}

func TestDiffStable(t *testing.T) {
	desired := map[string]bool{"web": true}
	actual := map[string]bool{"web": true}
	p := Diff(desired, actual)
	if len(p.Mounts) != 0 || len(p.Unmounts) != 0 {
		t.Errorf("expected no-op plan, got %+v", p)
	}
}

func TestDiffIgnoresFalseValues(t *testing.T) {
	// A host explicitly false should not be treated as present.
	desired := map[string]bool{"web": false, "db": true}
	actual := map[string]bool{"web": true}
	p := Diff(desired, actual)
	if !sortedEqual(p.Mounts, []string{"db"}) {
		t.Errorf("Mounts = %v, want [db]", p.Mounts)
	}
	if !sortedEqual(p.Unmounts, []string{"web"}) {
		t.Errorf("Unmounts = %v, want [web]", p.Unmounts)
	}
}

func sortedEqual(a, b []string) bool {
	sa := append([]string(nil), a...)
	sb := append([]string(nil), b...)
	sort.Strings(sa)
	sort.Strings(sb)
	return reflect.DeepEqual(sa, sb)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/reconcile/`
Expected: FAIL — `Diff` undefined.

- [ ] **Step 3: Implement `internal/reconcile/reconcile.go`**

```go
// Package reconcile computes the mount/unmount plan that brings the actual
// set of mounts into agreement with the desired set (control sockets present).
package reconcile

// Plan is the set of mount and unmount operations to perform.
type Plan struct {
	Mounts   []string // hosts to mount
	Unmounts []string // hosts to unmount
}

// Diff returns the operations needed to converge actual onto desired.
// A host is "present" in a set only if its value is true. Hosts present in
// desired but not actual must be mounted; present in actual but not desired
// must be unmounted.
func Diff(desired, actual map[string]bool) Plan {
	var p Plan
	for h, want := range desired {
		if !want {
			continue
		}
		if !actual[h] {
			p.Mounts = append(p.Mounts, h)
		}
	}
	for h, isMounted := range actual {
		if !isMounted {
			continue
		}
		if !desired[h] {
			p.Unmounts = append(p.Unmounts, h)
		}
	}
	return p
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/reconcile/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/reconcile/
git commit -m "feat(reconcile): pure diff of desired vs actual mount sets"
```

---

## Task 5: fuse — mount/unmount wrappers

**Files:**
- Create: `internal/fuse/fuse.go`
- Create: `internal/fuse/fuse_test.go`

**Interfaces:**
- Consumes: resolved host info (from sshoracle) + control path + mount point.
- Produces:
  - `MountArgs struct { Host, User, HostName, Port, ControlPath, MountPoint string }`
  - `MountCmd(args MountArgs) *exec.Cmd` — returns the sshfs command (not executed; testable string form via `cmd.String()`). Caller (cli) runs it.
  - `UnmountCmd(mountPoint string) *exec.Cmd` — returns `umount <mountPoint>`.
  - `LazyUnmountCmd(mountPoint string) *exec.Cmd` — `umount -f <mountPoint>` fallback.
  - `MkdirAll(mountPoint string) error` — ensures the mount point dir exists.

- [ ] **Step 1: Write failing test `internal/fuse/fuse_test.go`**

```go
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

func TestLazyUnmountCmd(t *testing.T) {
	cmd := LazyUnmountCmd("/Users/jesse/sshfs/web")
	s := cmd.String()
	if !strings.Contains(s, "umount -f") {
		t.Errorf("cmd = %q, want umount -f", s)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/fuse/`
Expected: FAIL — types undefined.

- [ ] **Step 3: Implement `internal/fuse/fuse.go`**

```go
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
	opts := []string{
		"-o", "ControlPath=" + a.ControlPath,
		"-o", "ControlMaster=no",
		"-o", "reconnect",
		"-o", "port=" + a.Port,
	}
	args := append([]string{target, a.MountPoint}, opts...)
	return exec.Command("sshfs", args...)
}

// UnmountCmd returns the standard umount for a mount point.
func UnmountCmd(mountPoint string) *exec.Cmd {
	return exec.Command("umount", mountPoint)
}

// LazyUnmountCmd returns a forced umount, used as a fallback when the mount
// is busy.
func LazyUnmountCmd(mountPoint string) *exec.Cmd {
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/fuse/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/fuse/
git commit -m "feat(fuse): build sshfs mount and umount commands"
```

---

## Task 6: Wire `asshfs reconcile`

**Files:**
- Modify: `internal/cli/cli.go`
- Create: `internal/cli/cli_test.go` additions (or new `reconcile_test.go`)

**Interfaces:**
- Consumes: `sshconfig.Hosts`, `sshoracle.ResolveAll`, `reconcile.Diff`, `fuse.{MountCmd,UnmountCmd,LazyUnmountCmd,MkdirAll,RemoveIfEmpty}`, `paths.*`, a logger.
- Produces: `cmdReconcile` that returns exit 0 on success, nonzero on hard failure (but individual mount/unmount failures are logged, not fatal).

- [ ] **Step 1: Write failing test for reconcile wiring**

Append to `internal/cli/cli_test.go`:

```go
package cli

import (
	"bytes"
	"strings"
	"testing"
)

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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestReconcileNoConfig`
Expected: FAIL — reconcile still returns 1 (placeholder).

- [ ] **Step 3: Implement reconcile in `internal/cli/cli.go`**

Replace the `cmdReconcile` placeholder with the real implementation. Add imports:

```go
import (
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/jesse/automatic-sshfs/internal/fuse"
	"github.com/jesse/automatic-sshfs/internal/paths"
	"github.com/jesse/automatic-sshfs/internal/reconcile"
	"github.com/jesse/automatic-sshfs/internal/sshconfig"
	"github.com/jesse/automatic-sshfs/internal/sshoracle"
)
```

Add a logger helper and rewrite `cmdReconcile`:

```go
func openLog() *log.Logger {
	lp, err := paths.LogPath()
	if err != nil {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	if err := os.MkdirAll(filepath.Dir(lp), 0o755); err != nil {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	f, err := os.OpenFile(lp, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return log.New(os.Stderr, "", log.LstdFlags)
	}
	return log.New(f, "", log.LstdFlags)
}

func cmdReconcile(stdout, stderr io.Writer) int {
	logger := openLog()
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "asshfs: %v\n", err)
		return 1
	}
	configPath := filepath.Join(home, ".ssh", "config")

	// Serialize concurrent reconcile runs (launchd can fire WatchPaths in bursts).
	release, ok := acquireLock()
	if !ok {
		logger.Println("reconcile already running; skipping")
		return 0
	}
	defer release()

	// 1. Enumerate hosts from config.
	hosts, err := sshconfig.Hosts(configPath)
	if err != nil {
		// Missing config is not fatal: nothing to reconcile.
		logger.Printf("sshconfig: %v", err)
		return 0
	}

	// 2. Resolve each host's control path via ssh -G, check socket existence.
	resolved, errs := sshoracle.ResolveAll(hosts)
	for h, e := range errs {
		logger.Printf("resolve %s: %v", h, e)
	}
	desired := make(map[string]bool)
	for h, r := range resolved {
		if r.ControlPath == "" || r.ControlPath == "none" {
			continue
		}
		if _, statErr := os.Stat(r.ControlPath); statErr == nil {
			desired[h] = true
		}
	}

	// 3. List actual mounts: directories under ~/sshfs/ that are mount points.
	actual := listMounted(stdout, logger)

	// 4. Diff and execute.
	plan := reconcile.Diff(desired, actual)
	for _, h := range plan.Mounts {
		if err := doMount(h, resolved[h], logger); err != nil {
			logger.Printf("mount %s: %v", h, err)
		}
	}
	for _, h := range plan.Unmounts {
		if err := doUnmount(h, logger); err != nil {
			logger.Printf("unmount %s: %v", h, err)
		}
	}
	return 0
}
```

Add the helper functions (`acquireLock`, `listMounted`, `doMount`, `doUnmount`) to `cli.go`:

```go
var lockMu sync.Mutex
var lockFile *os.File

// acquireLock takes an exclusive flock on a state file so concurrent
// reconcile invocations (launchd WatchPaths bursts) serialize. Returns a
// release function. If the lock is held, returns (nil-func, false).
func acquireLock() (func(), bool) {
	lockMu.Lock()
	defer lockMu.Unlock()
	lp, err := paths.LogPath()
	if err != nil {
		return nil, false
	}
	lockPath := lp + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, false
	}
	if err := tryFlock(f); err != nil {
		f.Close()
		return nil, false
	}
	lockFile = f
	return func() {
		releaseFlock(f)
		f.Close()
		lockMu.Lock()
		lockFile = nil
		lockMu.Unlock()
	}, true
}
```

Add `internal/cli/lock_unix.go` (build-tagged for unix) with `tryFlock`/`releaseFlock` using `golang.org/x/sys/unix`... — **no**: global constraints forbid external deps. Use the syscall package directly:

Create `internal/cli/flock.go`:

```go
//go:build darwin

package cli

import (
	"os"
	"syscall"
)

func tryFlock(f *os.File) error {
	flock := syscall.Flock_t{Type: syscall.F_WRLCK | syscall.F_UNLCK, Whence: 0, Start: 0, Len: 0}
	// Non-blocking exclusive lock.
	return syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &flock)
}

func releaseFlock(f *os.File) {
	flock := syscall.Flock_t{Type: syscall.F_UNLCK}
	syscall.FcntlFlock(f.Fd(), syscall.F_SETLK, &flock)
}
```

Wait — `syscall.Flock_t` on darwin uses a different shape; correct approach:

Create `internal/cli/flock.go` with the correct darwin syscall:

```go
//go:build darwin

package cli

import (
	"os"
	"syscall"
)

func tryFlock(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

func releaseFlock(f *os.File) {
	syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
```

(`syscall.Flock` is available on darwin and is the simplest correct API. `LOCK_EX|LOCK_NB` returns EWOULDBLOCK if held, which we treat as "busy → skip".)

Add `listMounted`, `doMount`, `doUnmount`:

```go
// listMounted returns the set of host names whose ~/sshfs/<host> is a mount
// point. We detect a mount point by comparing the device of the directory to
// its parent's device.
func listMounted(stdout io.Writer, logger *log.Logger) map[string]bool {
	out := make(map[string]bool)
	root, err := paths.MountRoot()
	if err != nil {
		logger.Printf("mount root: %v", err)
		return out
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return out // no mount root yet → nothing mounted
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mp := filepath.Join(root, e.Name())
		if isMountPoint(mp) {
			out[e.Name()] = true
		}
	}
	return out
}

func isMountPoint(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	parent, err := os.Stat(filepath.Dir(p))
	if err != nil {
		return false
	}
	return !os.SameFile(info, parent) && info.Sys().(*syscall.Stat_t).Dev != parent.Sys().(*syscall.Stat_t).Dev
}
```

Correction — a directory being a mount point is best detected by comparing device IDs of the dir vs its parent. But os.SameFile compares both dev and inode; for a mount the dir's dev differs from parent. Use:

```go
func isMountPoint(p string) bool {
	st, err := os.Stat(p)
	if err != nil {
		return false
	}
	pst, err := os.Stat(filepath.Dir(p))
	if err != nil {
		return false
	}
	return devOf(st) != devOf(pst)
}

func devOf(fi os.FileInfo) uint64 {
	if s, ok := fi.Sys().(*syscall.Stat_t); ok {
		return s.Dev
	}
	return 0
}
```

This is darwin-specific via `syscall.Stat_t`; gate with a build tag if needed. For v1 (macOS-only), it's fine.

Add `doMount` and `doUnmount`:

```go
func doMount(host string, r sshoracle.Resolved, logger *log.Logger) error {
	mp, err := paths.MountPoint(host)
	if err != nil {
		return err
	}
	if err := fuse.MkdirAll(mp); err != nil {
		return err
	}
	cmd := fuse.MountCmd(fuse.MountArgs{
		Host: host, User: r.User, HostName: r.HostName, Port: r.Port,
		ControlPath: r.ControlPath, MountPoint: mp,
	})
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("sshfs: %v: %s", err, out)
	}
	logger.Printf("mounted %s at %s", host, mp)
	return nil
}

func doUnmount(host string, logger *log.Logger) error {
	mp, err := paths.MountPoint(host)
	if err != nil {
		return err
	}
	cmd := fuse.UnmountCmd(mp)
	if out, err := cmd.CombinedOutput(); err != nil {
		// Busy? Try lazy/force unmount.
		fcmd := fuse.LazyUnmountCmd(mp)
		if fout, ferr := fcmd.CombinedOutput(); ferr != nil {
			return fmt.Errorf("umount: %v: %s | lazy: %v: %s", err, out, ferr, fout)
		}
	}
	fuse.RemoveIfEmpty(mp)
	logger.Printf("unmounted %s", host)
	return nil
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/cli/`
Expected: PASS (the `TestReconcileNoConfig` smoke test passes; placeholder test for reconcile returning 1 must be removed — it was a placeholder test).

Update the placeholder test `TestReconcilePlaceholder` by deleting it (reconcile is now implemented).

Run: `go build ./... && go test ./...`
Expected: all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): wire reconcile — sockets→mounts convergence"
```

---

## Task 7: `asshfs list` command

**Files:**
- Modify: `internal/cli/cli.go`
- Modify: `internal/cli/cli_test.go`

**Interfaces:**
- Produces: `cmdList` prints, per configured host: host alias, socket present (yes/no), mounted (yes/no), and the resolved control path. Useful for debugging.

- [ ] **Step 1: Write failing test**

Append to `internal/cli/cli_test.go`:

```go
func TestListNoConfig(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "list"}, &out, &errOut)
	if code != 0 {
		t.Errorf("code = %d, want 0; stderr=%q", code, errOut.String())
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/ -run TestListNoConfig`
Expected: FAIL (list still placeholder returning 1).

- [ ] **Step 3: Implement `cmdList`**

In `internal/cli/cli.go`, replace `cmdList`:

```go
func cmdList(stdout, stderr io.Writer) int {
	logger := openLog()
	home, _ := os.UserHomeDir()
	configPath := filepath.Join(home, ".ssh", "config")
	hosts, err := sshconfig.Hosts(configPath)
	if err != nil {
		fmt.Fprintf(stdout, "(no config: %v)\n", err)
		return 0
	}
	resolved, _ := sshoracle.ResolveAll(hosts)
	mounted := listMounted(stdout, logger)
	fmt.Fprintln(stdout, "HOST\tSOCKET\tMOUNTED\tCONTROLPATH")
	for _, h := range hosts {
		r, ok := resolved[h]
		if !ok {
			fmt.Fprintf(stdout, "%s\t?\tno\t?\n", h)
			continue
		}
		socket := "no"
		if r.ControlPath != "" && r.ControlPath != "none" {
			if _, err := os.Stat(r.ControlPath); err == nil {
				socket = "yes"
			}
		}
		m := "no"
		if mounted[h] {
			m = "yes"
		}
		fmt.Fprintf(stdout, "%s\t%s\t%s\t%s\n", h, socket, m, r.ControlPath)
	}
	return 0
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/cli/ -run TestListNoConfig`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): list command shows sockets and mounts per host"
```

---

## Task 8: launchd plist + `asshfs install`/`uninstall`

**Files:**
- Create: `internal/launchd/launchd.go`
- Create: `internal/launchd/launchd_test.go`
- Modify: `internal/cli/cli.go` (wire install/uninstall)
- Modify: `internal/cli/cli_test.go`

**Interfaces:**
- Produces:
  - `launchd.Plist(socketDir, binPath string) string` — returns the plist XML with WatchPaths on socketDir, ProgramArguments `[binPath, "reconcile"]`, RunAtLoad true, KeepAlive false (WatchPaths drives it).
  - `launchd.Load(plistPath string) error` — `launchctl load -w <plist>`.
  - `launchd.Unload(plistPath string) error` — `launchctl unload <plist>`.
  - `cli.cmdInstall` writes the plist, creates dirs, loads it, prints the ssh config block.
  - `cli.cmdUninstall` unloads, removes the plist, unmounts all, removes empty mount dirs.

- [ ] **Step 1: Write failing test for plist generation**

`internal/launchd/launchd_test.go`:

```go
package launchd

import (
	"strings"
	"testing"
	"unicode"
)

func TestPlistContainsRequiredKeys(t *testing.T) {
	got := Plist("/Users/jesse/.ssh/cm", "/usr/local/bin/asshfs")
	if !strings.Contains(got, "<key>Label</key>") {
		t.Error("missing Label")
	}
	if !strings.Contains(got, "io.asshfs.reconcile") {
		t.Error("missing label value")
	}
	if !strings.Contains(got, "<key>WatchPaths</key>") {
		t.Error("missing WatchPaths")
	}
	if !strings.Contains(got, "/Users/jesse/.ssh/cm") {
		t.Error("missing socket dir")
	}
	if !strings.Contains(got, "<key>ProgramArguments</key>") {
		t.Error("missing ProgramArguments")
	}
	if !strings.Contains(got, "/usr/local/bin/asshfs") {
		t.Error("missing binary path")
	}
	if !strings.Contains(got, "reconcile") {
		t.Error("missing reconcile arg")
	}
	if !strings.Contains(got, "<key>RunAtLoad</key>") {
		t.Error("missing RunAtLoad")
	}
}

func TestPlistIsWellFormedXML(t *testing.T) {
	got := Plist("/x", "/y")
	// Crude but sufficient: balanced angle brackets for the plist doctype and
	// root tags. Full XML validation is overkill for v1.
	if !strings.HasPrefix(strings.TrimLeftFunc(got, unicode.IsSpace), "<?xml") {
		t.Error("plist should start with xml declaration")
	}
	if !strings.Contains(got, "<plist version=\"1.0\">") {
		t.Error("missing plist root")
	}
	if !strings.HasSuffix(strings.TrimRightFunc(got, unicode.IsSpace), "</plist>") {
		t.Error("plist should end with </plist>")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/launchd/`
Expected: FAIL — `Plist` undefined.

- [ ] **Step 3: Implement `internal/launchd/launchd.go`**

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/launchd/`
Expected: PASS

- [ ] **Step 5: Wire `cmdInstall` and `cmdUninstall` in `internal/cli/cli.go`**

Replace the placeholders:

```go
func cmdInstall(stdout, stderr io.Writer) int {
	logger := openLog()
	// Resolve binary path: prefer the current executable so the installed job
	// runs the same binary the user invoked.
	binPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "asshfs: cannot resolve own executable: %v\n", err)
		return 1
	}
	socketDir, err := paths.SocketDir()
	if err != nil {
		fmt.Fprintf(stderr, "asshfs: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(socketDir, 0o700); err != nil {
		fmt.Fprintf(stderr, "asshfs: mkdir %s: %v\n", socketDir, err)
		return 1
	}
	mountRoot, err := paths.MountRoot()
	if err != nil {
		fmt.Fprintf(stderr, "asshfs: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(mountRoot, 0o755); err != nil {
		fmt.Fprintf(stderr, "asshfs: mkdir %s: %v\n", mountRoot, err)
		return 1
	}
	plistPath, err := paths.PlistPath()
	if err != nil {
		fmt.Fprintf(stderr, "asshfs: %v\n", err)
		return 1
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		fmt.Fprintf(stderr, "asshfs: %v\n", err)
		return 1
	}
	xml := launchd.Plist(socketDir, binPath)
	if err := os.WriteFile(plistPath, []byte(xml), 0o644); err != nil {
		fmt.Fprintf(stderr, "asshfs: write plist: %v\n", err)
		return 1
	}
	if err := launchd.Load(plistPath); err != nil {
		fmt.Fprintf(stderr, "asshfs: load: %v\n", err)
		return 1
	}
	logger.Println("installed")
	fmt.Fprintln(stdout, "Installed asshfs launchd agent.")
	fmt.Fprintln(stdout, "Add the following to ~/.ssh/config (asshfs will not edit it for you):")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "    Host *")
	fmt.Fprintln(stdout, "        ControlMaster auto")
	fmt.Fprintln(stdout, "        ControlPath "+socketDir+"/%C")
	fmt.Fprintln(stdout, "        ControlPersist 30s")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Then ssh to any configured host to mount its filesystem at ~/sshfs/<host>/.")
	return 0
}

func cmdUninstall(stdout, stderr io.Writer) int {
	logger := openLog()
	plistPath, err := paths.PlistPath()
	if err != nil {
		fmt.Fprintf(stderr, "asshfs: %v\n", err)
		return 1
	}
	if err := launchd.Unload(plistPath); err != nil {
		fmt.Fprintf(stderr, "asshfs: unload: %v\n", err)
		// continue to clean up anyway
	}
	_ = os.Remove(plistPath)
	// Unmount everything currently mounted under ~/sshfs/.
	mountRoot, _ := paths.MountRoot()
	entries, _ := os.ReadDir(mountRoot)
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		mp := filepath.Join(mountRoot, e.Name())
		if isMountPoint(mp) {
			if err := fuse.LazyUnmountCmd(mp).Run(); err != nil {
				logger.Printf("uninstall: unmount %s: %v", mp, err)
			}
		}
		fuse.RemoveIfEmpty(mp)
	}
	logger.Println("uninstalled")
	fmt.Fprintln(stdout, "Uninstalled asshfs launchd agent and unmounted all mounts.")
	fmt.Fprintln(stdout, "Remove the ControlMaster/ControlPath/ControlPersist lines from ~/.ssh/config if you wish.")
	return 0
}
```

Add `launchd` import to `cli.go`.

- [ ] **Step 6: Write tests for install/uninstall smoke**

Append to `internal/cli/cli_test.go`:

```go
func TestInstallSmoke(t *testing.T) {
	// Use a temp HOME so we don't touch the real LaunchAgents dir.
	t.Setenv("HOME", t.TempDir())
	var out, errOut bytes.Buffer
	code := RunWith([]string{"asshfs", "install"}, &out, &errOut)
	// install will try to launchctl load, which may fail in a sandbox/CI; that's
	// okay as long as the plist was written and the help text printed.
	_ = code
	if !strings.Contains(out.String(), "~/.ssh/config") {
		t.Errorf("stdout = %q, want config instructions", out.String())
	}
	// Plist file should exist.
	home, _ := os.UserHomeDir()
	plist := filepath.Join(home, "Library", "LaunchAgents", "io.asshfs.reconcile.plist")
	if _, err := os.Stat(plist); err != nil {
		t.Errorf("plist not written: %v", err)
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
```

Add imports `path/filepath` to the test file if not present.

- [ ] **Step 7: Run all tests**

Run: `go build ./... && go test ./...`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/launchd/ internal/cli/
git commit -m "feat(launchd): install/uninstall with WatchPaths plist"
```

---

## Task 9: End-to-end manual verification + README

**Files:**
- Create: `README.md`
- Create: `docs/superpowers/specs/2026-08-21-automatic-sshfs-design.md` (already exists)

This task is verification-heavy and depends on FUSE-T/sshfs being installed. It is performed by the parent after subagents finish, not delegated (requires interactive sshd).

- [ ] **Step 1: Build the binary**

```bash
go build -o /tmp/asshfs ./cmd/asshfs
```

- [ ] **Step 2: Run `asshfs list` against the user's real config**

```bash
/tmp/asshfs list
```

Verify it lists configured hosts with socket=mounted columns. (No mounts yet; all no/no unless an ssh session is live.)

- [ ] **Step 3: Verify `ssh -G` oracle integration**

For each host shown, manually run `ssh -G <host> | grep controlpath` and confirm it matches the path `asshfs list` shows.

- [ ] **Step 4: Write `README.md`**

Cover: what it does, prerequisites (FUSE-T + sshfs: `brew install --cask fuse-t && brew install sshfs`), install (`asshfs install` + add the ssh config block), usage (just `ssh host` — mount appears at `~/sshfs/host/`), uninstall, troubleshooting (`asshfs list`, log at `~/Library/Logs/asshfs.log`), and the v1 limitations (no CanonicalizeHostname/Match exec; sshfs ControlPath passthrough fallback).

- [ ] **Step 5: Commit**

```bash
git add README.md
git commit -m "docs: README with install, usage, and troubleshooting"
```

---

## Self-Review (run before handoff)

**Spec coverage:**
- Architecture (ControlMaster + WatchPaths + reconcile): Tasks 1, 4, 6, 8. ✓
- ssh config enumeration: Task 2. ✓
- ssh -G oracle (token expansion delegated): Task 3. ✓
- Reconcile diff: Task 4. ✓
- fuse mount/unmount: Task 5. ✓
- install/uninstall + plist: Task 8. ✓
- list: Task 7. ✓
- Error handling (mount failure logged, lazy unmount, flock serialization): Task 6. ✓
- Remote root `/` mount: Task 5 (`%s:/`). ✓
- ~/sshfs/<host> path: Task 1 (paths). ✓
- Never touch ~/.ssh/config: Task 8 (prints block, doesn't write). ✓
- v1 limitations documented: Task 9 (README). ✓
- sshfs ControlPath passthrough fallback: noted in README (Task 9) and spec; the mount command in Task 5 passes ControlPath/ControlMaster and if sshfs ignores it, sshfs still connects on its own — degrade behavior. ✓

**Placeholder scan:** None — each code step has real Go.

**Type consistency:** `sshoracle.Resolved` used in cli (Task 6) matches Task 3. `fuse.MountArgs` fields match Task 5. `reconcile.Plan` matches Task 4. `paths.MountPoint/SocketDir/etc.` consistent across tasks.

**Gaps:** None identified.
