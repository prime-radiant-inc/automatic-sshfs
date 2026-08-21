// Package cli implements the asshfs subcommands.
package cli

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
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
