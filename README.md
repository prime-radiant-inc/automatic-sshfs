# asshfs — automatic SSHFS for macOS

`asshfs` automatically mounts the remote filesystem of any SSH host you connect
to, using SSHFS over FUSE-T. When you `ssh` to a host, its filesystem appears
at `~/sshfs/<host>/`. When the last session to that host closes, the mount
disappears. No wrapper command to remember — it hooks into OpenSSH's
`ControlMaster` multiplexing and watches for control-socket changes via
`launchd`.

## How it works

1. You add `ControlMaster auto` + `ControlPath` + `ControlPersist` to your
   `~/.ssh/config` (the `install` command shows you exactly what to add).
2. When you `ssh host`, OpenSSH creates a control socket in your
   `ControlPath` directory.
3. `launchd` fires `asshfs reconcile` via `WatchPaths` on that directory.
4. `reconcile` enumerates your `Host` entries, uses `ssh -G` to resolve each
   host's control path, stats it to see which sockets exist, diffs against
   existing FUSE mounts, and mounts/unmounts to converge.
5. The control socket's existence *is* the per-host refcount — mount once,
   share across concurrent sessions, unmount on last disconnect.

## Prerequisites

- **FUSE-T** (userspace FUSE, no kernel extension):
  ```sh
  brew install macos-fuse-t/homebrew-cask/fuse-t
  ```
- **SSHFS** (FUSE-T-compatible build, linked against FUSE-T's libraries):
  ```sh
  brew install macos-fuse-t/homebrew-cask/fuse-t-sshfs
  ```
- **macOS** (uses `launchd`, `umount`, and `syscall.Flock`)

> **Important:** Use the FUSE-T tap (`macos-fuse-t/homebrew-cask`) for both
> packages. The Homebrew core `sshfs`/`sshfs-mac` formulae are linked against
> macFUSE, which has a different code-signing Team ID and won't work with
> FUSE-T.

## Install

```sh
go install github.com/jesse/automatic-sshfs/cmd/asshfs@latest
asshfs install
```

The `install` command automatically:
1. Checks that FUSE-T and sshfs are installed (prints install commands if missing)
2. Creates the socket directory (`~/.ssh/cm/` or your existing ControlPath dir)
3. Creates the mount root (`~/sshfs/`)
4. Writes a managed SSH config snippet to `~/.ssh/asshfs.conf` with the
   `ControlMaster`/`ControlPath`/`ControlPersist` directives
5. Adds `Include asshfs.conf` to your `~/.ssh/config` (no manual editing needed)
6. Installs and loads a `launchd` agent that watches the socket directory

No manual config file editing required. `asshfs` manages its own snippet file
and cleans it up on uninstall.

## Usage

Just use `ssh` as normal:

```sh
ssh web1
# → ~/sshfs/web1/ now contains the remote filesystem (root /)
```

The mount appears within the `launchd` WatchPaths trigger latency (typically
under a second). Open another terminal and `ls ~/sshfs/web1/` to browse the
remote filesystem.

When the last SSH session to a host closes (and `ControlPersist` expires),
the mount is automatically unmounted.

### Commands

- `asshfs reconcile` — Run a reconcile cycle manually (also run automatically
  by `launchd` on socket-directory changes). Idempotent and safe to run
  anytime.
- `asshfs list` — Show per-host status: socket present, mounted, and resolved
  control path. Useful for debugging.
- `asshfs install` — Set up the `launchd` agent, create directories, and print
  the SSH config block.
- `asshfs uninstall` — Remove the `launchd` agent, unmount everything, and
  clean up.

## Troubleshooting

- **Mount not appearing:** Run `asshfs list` to check socket and mount status.
  Verify your `ControlPath` matches what `list` shows. Ensure `ControlMaster
  auto` is in your config.
- **Logs:** Check `~/Library/Logs/asshfs.log` for mount/unmount errors.
- **`ssh -G` is the oracle:** `asshfs` uses `ssh -G <host>` to resolve control
  paths exactly as SSH does. If `list` shows a wrong path, run
  `ssh -G <host> | grep controlpath` to verify.
- **FUSE-T not installed:** Mount will fail with an sshfs error. Install
  FUSE-T and sshfs (see Prerequisites).

## Limitations (v1)

- **No `CanonicalizeHostname`/`Match exec` resolution.** `asshfs` uses
  `ssh -G` which handles `HostName`/`Port`/`User` directives and `%C` token
  expansion, but does not resolve `CanonicalizeHostname` or `Match exec`
  blocks. This covers the overwhelming majority of SSH configs.
- **SSHFS ControlPath passthrough.** The mount command passes
  `ControlPath` and `ControlMaster=no` to sshfs so it reuses the
  already-authenticated master connection. If a particular sshfs build does
  not forward these options, sshfs will make its own connection (which may
  prompt for authentication). The mount still works either way.
- **Remote root mounted.** The remote filesystem root (`/`) is mounted to
  `~/sshfs/<host>/`, giving full access to the remote filesystem tree.

## Uninstall

```sh
asshfs uninstall
```

This unloads the `launchd` agent, removes the plist, unmounts all
`asshfs`-managed mounts, removes the `~/.ssh/asshfs.conf` snippet, and
removes the `Include` directive from `~/.ssh/config`. Your own SSH config
entries are untouched.
