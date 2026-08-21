// Package cli implements the asshfs subcommands.
package cli

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/jesse/automatic-sshfs/internal/fuse"
	"github.com/jesse/automatic-sshfs/internal/launchd"
	"github.com/jesse/automatic-sshfs/internal/paths"
	"github.com/jesse/automatic-sshfs/internal/reconcile"
	"github.com/jesse/automatic-sshfs/internal/sshconfig"
	"github.com/jesse/automatic-sshfs/internal/sshoracle"
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

// openLog opens the asshfs log file (~/Library/Logs/asshfs.log) for appending,
// creating its parent directory if needed. If anything fails it falls back to
// a logger writing to stderr so we never lose log lines.
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
	actual := listMounted(logger)

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

// listMounted returns the set of host names whose ~/sshfs/<host> is a mount
// point. We detect a mount point by comparing the device of the directory to
// its parent's device.
func listMounted(logger *log.Logger) map[string]bool {
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
		return uint64(s.Dev)
	}
	return 0
}

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
		// Busy? Try force unmount.
		fcmd := fuse.ForceUnmountCmd(mp)
		if fout, ferr := fcmd.CombinedOutput(); ferr != nil {
			return fmt.Errorf("umount: %v: %s | force: %v: %s", err, out, ferr, fout)
		}
	}
	fuse.RemoveIfEmpty(mp)
	logger.Printf("unmounted %s", host)
	return nil
}

// fuseInstallPaths are the filesystem locations that indicate a FUSE
// implementation is installed. FUSE-T installs as a framework (not a .fs);
// macFUSE uses the traditional /Library/Filesystems path.
var fuseInstallPaths = []string{
	"/Library/Frameworks/fuse_t.framework",
	"/Library/Filesystems/fuse-t.fs",
	"/Library/Filesystems/macfuse.fs",
}

// missingPrereqs checks for sshfs in PATH and a FUSE filesystem installation.
// It returns a list of user-facing messages describing what is missing; an
// empty list means all prerequisites are satisfied.
func missingPrereqs() []string {
	var missing []string
	if _, err := exec.LookPath("sshfs"); err != nil {
		missing = append(missing, "SSHFS is required but not found. Install it:\n    brew install macos-fuse-t/homebrew-cask/fuse-t-sshfs")
	}
	if !fuseInstalled() {
		missing = append(missing, "FUSE-T is required but not found. Install it:\n    brew install macos-fuse-t/homebrew-cask/fuse-t")
	}
	return missing
}

func fuseInstalled() bool {
	for _, p := range fuseInstallPaths {
		if _, err := os.Stat(p); err == nil {
			return true
		}
	}
	return false
}

// ensureInclude adds an Include directive to the ssh config file if it's not
// already present. Creates the file if it doesn't exist.
func ensureInclude(configPath, includeLine string) error {
	// Read existing config (may not exist yet).
	data, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := string(data)
	// Check if the Include line is already present (whole-line match).
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) == includeLine {
			return nil // already present
		}
	}
	// Append the Include line. Ensure it ends with a newline first.
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += includeLine + "\n"
	return os.WriteFile(configPath, []byte(content), 0o644)
}

// removeInclude removes an Include directive from the ssh config file.
func removeInclude(configPath, includeLine string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == includeLine {
			continue // skip the Include line
		}
		lines = append(lines, line)
	}
	// Rejoin, trimming trailing empty lines.
	result := strings.Join(lines, "\n")
	result = strings.TrimRight(result, "\n")
	if result != "" {
		result += "\n"
	}
	return os.WriteFile(configPath, []byte(result), 0o644)
}

// detectControlPathDir reads the user's ~/.ssh/config and looks for a
// ControlPath directive. If found, it extracts the directory portion (the
// value with any trailing % token and last path component stripped). If not
// found or on error, it returns "".
func detectControlPathDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	configPath := filepath.Join(home, ".ssh", "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		keyword, rest := splitKeyword(line)
		if strings.ToLower(keyword) != "controlpath" {
			continue
		}
		val := strings.TrimSpace(rest)
		if val == "" || val == "none" {
			continue
		}
		// Expand a leading ~ to the home directory.
		if val == "~" {
			val = home
		} else if strings.HasPrefix(val, "~/") {
			val = filepath.Join(home, val[2:])
		}
		// If the value contains a % token (e.g. %C), the directory is the
		// path before it: /Users/jesse/.ssh/s/%C → /Users/jesse/.ssh/s.
		// Otherwise the value is a literal socket path; take its parent dir.
		if idx := strings.IndexByte(val, '%'); idx >= 0 {
			val = strings.TrimRight(val[:idx], "/")
		} else {
			val = filepath.Dir(val)
		}
		if val == "" || val == "." {
			return ""
		}
		return val
	}
	return ""
}

// splitKeyword returns the first whitespace-delimited keyword and the rest.
func splitKeyword(line string) (keyword, rest string) {
	i := strings.IndexAny(line, " \t")
	if i < 0 {
		return line, ""
	}
	return line[:i], strings.TrimSpace(line[i+1:])
}

func cmdInstall(stdout, stderr io.Writer) int {
	logger := openLog()
	// Prerequisite check: sshfs and a FUSE implementation must be installed
	// before we can set up mounts.
	if missing := missingPrereqs(); len(missing) > 0 {
		for _, m := range missing {
			fmt.Fprintln(stderr, m)
		}
		return 1
	}
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintf(stderr, "asshfs: %v\n", err)
		return 1
	}
	// Resolve binary path: prefer the current executable so the installed job
	// runs the same binary the user invoked.
	binPath, err := os.Executable()
	if err != nil {
		fmt.Fprintf(stderr, "asshfs: cannot resolve own executable: %v\n", err)
		return 1
	}
	// Detect the ControlPath directory from the user's ssh config; fall back to
	// the default ~/.ssh/cm. WatchPaths must point at the directory holding the
	// control sockets so launchd fires reconcile when sockets appear/change.
	socketDir := detectControlPathDir()
	if socketDir == "" {
		socketDir, err = paths.SocketDir()
		if err != nil {
			fmt.Fprintf(stderr, "asshfs: %v\n", err)
			return 1
		}
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

	// Write the managed SSH config snippet and wire it into ~/.ssh/config
	// via an Include directive so the user doesn't have to edit anything.
	asshfsConf := filepath.Join(home, ".ssh", "asshfs.conf")
	confContent := "Host *\n" +
		"    ControlMaster auto\n" +
		"    ControlPath " + socketDir + "/%C\n" +
		"    ControlPersist 30s\n"
	if err := os.WriteFile(asshfsConf, []byte(confContent), 0o600); err != nil {
		fmt.Fprintf(stderr, "asshfs: write %s: %v\n", asshfsConf, err)
		// Fall back to printing the block.
		fmt.Fprintln(stdout, "Add the following to ~/.ssh/config:")
		fmt.Fprintln(stdout, "")
		fmt.Fprintln(stdout, "    Host *")
		fmt.Fprintln(stdout, "        ControlMaster auto")
		fmt.Fprintln(stdout, "        ControlPath "+socketDir+"/%C")
		fmt.Fprintln(stdout, "        ControlPersist 30s")
		return 0
	}
	fmt.Fprintf(stdout, "Wrote SSH config to %s\n", asshfsConf)

	// Add Include directive to ~/.ssh/config if not already present.
	sshConfig := filepath.Join(home, ".ssh", "config")
	includeLine := "Include asshfs.conf"
	if err := ensureInclude(sshConfig, includeLine); err != nil {
		fmt.Fprintf(stderr, "asshfs: could not add Include to %s: %v\n", sshConfig, err)
		fmt.Fprintln(stdout, "Add this line to ~/.ssh/config:")
		fmt.Fprintln(stdout, "    "+includeLine)
	} else {
		fmt.Fprintf(stdout, "Added '%s' to %s\n", includeLine, sshConfig)
	}
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Done! ssh to any configured host to mount its filesystem at ~/sshfs/<host>/.")
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
			if err := fuse.ForceUnmountCmd(mp).Run(); err != nil {
				logger.Printf("uninstall: unmount %s: %v", mp, err)
			}
		}
		fuse.RemoveIfEmpty(mp)
	}
	logger.Println("uninstalled")
	fmt.Fprintln(stdout, "Uninstalled asshfs launchd agent and unmounted all mounts.")
	// Remove the managed SSH config snippet and its Include directive.
	home, _ := os.UserHomeDir()
	asshfsConf := filepath.Join(home, ".ssh", "asshfs.conf")
	sshConfig := filepath.Join(home, ".ssh", "config")
	_ = removeInclude(sshConfig, "Include asshfs.conf")
	_ = os.Remove(asshfsConf)
	fmt.Fprintf(stdout, "Removed %s and its Include directive from ~/.ssh/config.\n", asshfsConf)
	return 0
}

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
	mounted := listMounted(logger)
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
