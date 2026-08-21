# automatic-sshfs — Design Spec

**Date:** 2026-08-21
**Status:** Approved
**Author:** Design via brainstorming session

## Summary

`asshfs` is a macOS tool that automatically mounts the remote filesystem (via
FUSE-T + SSHFS) to `~/sshfs/<host>/` whenever an SSH connection to a host
exists, and unmounts it when the last connection to that host closes. It is
fully transparent: no wrapper command to remember, and it never sits in the
SSH transport path.

## Mechanism

OpenSSH's `ControlMaster` creates a Unix control socket per host for the life
of any multiplexed session. `asshfs` watches the control-socket directory and
reconciles "sockets present" against "mounts present" — mount new, unmount
gone. The control socket's existence *is* the per-host refcount, so ref-count
behavior comes for free.

## Decisions (from brainstorming)

| Decision | Choice | Rationale |
|---|---|---|
| Mount technology | FUSE-T + SSHFS (real POSIX mount) | Userspace, no kext, future-proof; macFUSE requires reduced-security boot and is no longer open-source |
| SSH trigger | ControlMaster (not ProxyCommand) | Fully decoupled from ssh internals; multiplexed sessions don't re-trigger; no wrapper to remember |
| What gets mounted | Remote root (`/`) to `~/sshfs/<host>/` | Maximum flexibility — navigate anywhere on the remote |
| Lifecycle | Ref-count per host | Mount once, share across concurrent sessions, unmount on last disconnect. Free via control-socket existence. |
| Watcher | launchd WatchPaths on the socket dir | No always-running process; macOS-native; survives logout/reboot; idempotent/self-healing |

## Architecture

```
                 ~/.ssh/config                      ~/Library/LaunchAgents/
        ┌─────────────────────────────┐        ┌───────────────────────────────┐
        │ Host *                      │        │ io.asshfs.reconcile.plist     │
        │   ControlMaster auto        │        │   WatchPaths = ~/.ssh/cm/      │
        │   ControlPath ~/.ssh/cm/%C  │───────▶│        │                       │
        │   ControlPersist 30s        │        │        ▼ (dir change)          │
        │   (user adds these once)    │        │   asshfs reconcile             │
        └─────────────────────────────┘        └───────────────────────────────┘
                          │                                    │
                          ▼                                    ▼
        ssh user@host ──▶ creates ~/.ssh/cm/<hash>     asshfs reconcile:
                          socket (master)               1. enumerate Hosts in ~/.ssh/config
                                                        2. for each, stat its expected ControlPath
                                                        3. list active FUSE mounts at ~/sshfs/*
                                                        4. diff → mount new hosts, unmount gone
                                                        5. exit
```

### Key design decision: drive from ssh config, not socket filenames

A `%C` ControlPath hashes the host so the socket filename is opaque. The robust
fix: enumerate `Host` entries in `~/.ssh/config`, compute each host's *expected*
ControlPath by expanding ssh's tokens, and `stat()` it. We never parse socket
filenames. A socket that exists for a configured host → that host should be
mounted. This works for *any* ControlPath template.

### Riding the master socket (no double auth)

sshfs internally invokes `ssh`; we pass `-o ControlPath=<same path>
-o ControlMaster=no` so sshfs reuses the *already-authenticated* master
connection rather than opening a new one. The user authenticates once (their
normal `ssh`), and the mount piggybacks on that session with zero extra prompts.

**Spike risk:** verify sshfs forwards `ControlPath`/`ControlMaster` to its
internal ssh. If a given sshfs build doesn't, fall back to letting sshfs make
its own connection (still works, may prompt for auth/key).

## Components

**Language: Go.** Single static binary; `go` already on PATH.

Binary `asshfs` with subcommands:

1. `asshfs reconcile` — the core loop. Short-lived; launched by launchd or
   runnable by hand. Idempotent and self-healing.
2. `asshfs install` — writes the launchd plist, creates `~/.ssh/cm/` and
   `~/sshfs/`, `launchctl load`s the plist, prints the `~/.ssh/config` block
   to add. Refuses to touch `~/.ssh/config` itself.
3. `asshfs uninstall` — `launchctl unload`, removes the plist, unmounts
   everything, removes empty mount dirs. Leaves `~/.ssh/config` alone.
4. `asshfs list` — shows current sockets ↔ mounts state (debugging/inspection).

Internal packages:
- `internal/sshconfig` — parser + token expander
- `internal/fuse` — mount/unmount wrappers
- `internal/reconcile` — the diff logic

Each independently testable.

## Token expansion scope (v1)

Support: `%h` (HostName or Host alias), `%r` (User or `$USER`), `%p` (Port or
22), `%C` (hash of ssh-defined concatenation), `%l` (local hostname), `~` and
`$ENV` expansion. Honor `HostName`/`Port`/`User`/`Include` directives and
wildcard `Host` matching precedence.

Defer full `CanonicalizeHostname`/`Match exec` resolution to a later version.
Unknown tokens → leave literal. Document the limitation.

## Data flow & state

- **No persistent state of our own.** Ground truth: (a) which control sockets
  exist in the socket dir, (b) which FUSE mounts exist at `~/sshfs/`. Reconcile
  recomputes both each run and converges. A crash or missed WatchPaths event is
  self-healing.
- **Mount directory convention:** `~/sshfs/<Host-alias>/`. Create on mount,
  remove on unmount if empty.
- **ControlPersist:** installer recommends `ControlPersist 30s` to prevent
  thrash on quick reconnect cycles.
- **WatchPaths target:** `~/.ssh/cm/` (or whatever dir the user's ControlPath
  resolves to — `install` detects it from their config).

## Error handling & robustness

- **Mount failure** (host unreachable, sshfs missing, FUSE-T not installed):
  log to `~/Library/Logs/asshfs.log`, leave unmounted, exit cleanly. The
  user's `ssh` session still works normally. Next reconcile retries.
- **Unmount failure** (mount busy): retry with lazy/force unmount; if still
  busy, log and leave it mounted. Never lose data to an impatient unmount.
- **Concurrent reconcile runs:** `flock` on a state file; second instance
  exits immediately. Serializes bursts.
- **sshfs crash / orphaned mount:** reconcile detects and converges.
- **FUSE-T not installed:** `install` checks for `sshfs` and FUSE-T; if absent,
  prints the brew commands and stops. Doesn't auto-install system software
  without consent.
- **sshfs doesn't forward ControlPath:** degrade to sshfs making its own
  connection. Documented behavior, not a crash.

## Testing

- **Unit (TDD, no network, no FUSE):**
  - `internal/sshconfig`: parse fixture `~/.ssh/config`, expand tokens, honor
    directives, wildcard matching precedence.
  - `internal/reconcile`: given fake socket-dir listing + fake mount list,
    the diff produces the correct mount/unmount plan.
  - Token expansion including `%C` hash correctness (validated against
    `ssh -G <host>` output as a fixture).
- **Integration (gated on prereqs):** against a real sshd + FUSE-T. Build tag
  `integration`. Skipped if prereqs absent.
- **`ssh -G` oracle:** validate our config parser against real ssh during
  development.

## Research findings (existing solutions)

| Existing | Approach | Why it doesn't fit |
|---|---|---|
| `robgmills/sshfs-automount` | launchd + macOS `automount` daemon, static host | Not SSH-triggered; static config; Yosemite-era; 0 stars; dormant |
| Super User jkhadka 2017 | Wrapper script: `sshfs` → `ssh` → `umount` | Naive wrapper, single hardcoded host, no concurrency, no ProxyCommand |
| VSCode-SSHFS, Cyberduck | GUI mounters | Not triggered by `ssh`; not transparent to terminal |

No ControlMaster-triggered auto-mount tool exists. Building new.
