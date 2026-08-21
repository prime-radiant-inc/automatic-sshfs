# asshfs

Automatically mount remote filesystems over SSH. When you connect to a host,
its filesystem appears at `~/sshfs/<host>/`. When the last session closes, the
mount disappears. No wrapper command, no manual mounting.

> **Early build.** This software was developed by an AI agent and has not been
> audited. Use at your own risk.

## How it works

asshfs hooks into OpenSSH's `ControlMaster` connection multiplexing. When you
`ssh` to a host, OpenSSH creates a control socket. asshfs watches that socket
directory via `launchd` and mounts the remote filesystem using SSHFS over
FUSE-T. The control socket's existence acts as a per-host reference count:
mount once, share across concurrent sessions, unmount when the last session
closes.

```
ssh web1 ──▶ control socket created ──▶ launchd fires ──▶ asshfs mounts ~/sshfs/web1/
exit      ──▶ socket disappears       ──▶ launchd fires ──▶ asshfs unmounts
```

## Prerequisites

- **FUSE-T** (userspace FUSE, no kernel extension):
  ```sh
  brew install macos-fuse-t/homebrew-cask/fuse-t
  ```
- **SSHFS** (FUSE-T-compatible build):
  ```sh
  brew install macos-fuse-t/homebrew-cask/fuse-t-sshfs
  ```

Use the FUSE-T tap for both packages. The Homebrew core `sshfs` and
`sshfs-mac` formulae link against macFUSE, which has a different code-signing
identity and will not work with FUSE-T.

- **macOS** (uses `launchd`, `umount`, and OpenSSH)

## Install

```sh
go install github.com/obra/automatic-sshfs/cmd/asshfs@latest
asshfs install
```

The `install` command does everything for you:

1. Checks that FUSE-T and sshfs are installed
2. Creates the socket and mount directories
3. Writes `~/.ssh/asshfs.conf` with the ControlMaster directives
4. Adds `Include asshfs.conf` to your `~/.ssh/config`
5. Installs and loads a `launchd` agent

No manual config editing. Your existing SSH config entries are untouched.

## Usage

Use `ssh` as you normally would:

```sh
ssh web1
```

The remote filesystem appears at `~/sshfs/web1/` within a second. Browse it,
edit files in it, or use it from any application:

```sh
ls ~/sshfs/web1/
vim ~/sshfs/web1/etc/nginx/nginx.conf
cp ~/sshfs/web1/var/log/syslog ./
```

When the last SSH session to a host closes, the mount disappears.

### Commands

| Command | Description |
|---|---|
| `asshfs install` | Set up the launchd agent, write SSH config, create directories |
| `asshfs uninstall` | Remove the agent, unmount everything, clean up config |
| `asshfs reconcile` | Run a mount/unmount cycle manually (also runs automatically via launchd) |
| `asshfs list` | Show per-host status: socket present, mounted, control path |

## Troubleshooting

**Mount not appearing?** Run `asshfs list` to check socket and mount status.
Verify your `ControlPath` matches what `list` shows. Ensure `ControlMaster
auto` is in your config.

**Check logs:**
```sh
cat ~/Library/Logs/asshfs.log
```

**Verify ssh -G resolution:** asshfs uses `ssh -G <host>` to resolve control
paths exactly as SSH does. If `list` shows a wrong path, run:
```sh
ssh -G <host> | grep controlpath
```

**FUSE-T not installed?** Mount will fail with an sshfs error. Install
FUSE-T and sshfs from the FUSE-T tap (see Prerequisites above).

## Uninstall

```sh
asshfs uninstall
```

This unloads the launchd agent, removes the plist, unmounts all mounts,
removes `~/.ssh/asshfs.conf`, and removes the `Include` line from
`~/.ssh/config`. Your own SSH config entries are untouched.

## Limitations

- **No `CanonicalizeHostname` or `Match exec` resolution.** asshfs delegates
  token expansion to `ssh -G`, which handles `HostName`, `Port`, `User`, and
  `%C` but not these directives.
- **Remote root mounted.** The remote filesystem root (`/`) mounts to
  `~/sshfs/<host>/`, giving full access to the remote tree.
- **SSHFS ControlPath passthrough.** The mount command passes `ControlPath`
  and `ControlMaster=no` to sshfs so it reuses the authenticated master
  connection. If a particular sshfs build ignores these options, sshfs makes
  its own connection (which may prompt for authentication). The mount still
  works either way.

## License

MIT
