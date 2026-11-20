---
name: goto-remote-command
description: Use this skill in the goto/goterm project when an agent needs to execute remote commands over SSH with the goto CLI, debug batch remote execution, preserve raw stdout/stderr/exit status, or update remote-command docs, tests, or behavior.
---

# Goto Remote Command

## Purpose

Use `goto` as the project-native SSH command runner. Prefer it over ad hoc `ssh`, `expect`, or custom shell glue when the task is about this repo's remote command behavior.

## Core Contract

- The command name is `goto`; `goterm` is legacy naming only.
- Batch mode is host-first: `goto <name|ip[:port]|expr|pattern> <cmd...>`.
- If no positional command and no `-cmd` are provided, batch mode can read the command from stdin.
- Batch mode must write only remote stdout to local stdout and remote stderr to local stderr.
- Batch mode must exit with the remote command's exit status.
- Local connection logs, host selection logs, and other agent narration must not mix into raw command output unless `-v` is explicitly requested.
- `initcmds` is interactive-only. Do not run it in batch mode.
- Interactive mode uses PTY + `sess.Shell()`. Batch mode uses `sess.Run(cmd)` without PTY.

## How To Execute Remote Commands

From this repository, build or run the CLI with normal Go tooling:

```bash
go run . <host> uptime
go run . -cmd='uname -a' <host>
echo 'df -h' | go run . <host>
go run . -j <jump-host> <host> uptime
go run . -cred=vm <host> uptime
go run . -setup-nopass -p=vm <host>
```

For an installed binary:

```bash
goto <host> uptime
goto -cmd='uname -a' <host>
echo 'df -h' | goto <host>
goto -j <jump-host> <host> uptime
goto -cred=vm <host> uptime
goto -setup-nopass -p=vm <host>
```

Use `-v` only when debugging the connection path, because it intentionally enables verbose local logs:

```bash
goto -v <host> uptime
```

## Production hosts (hhy / YG prod) — quick login

- Prod hosts (组件节点、Ceph 节点、计算节点) log in with the stored `hhy` credential: `goto -p hhy <host|ip> "<cmd>"` (user `hhy-ceph`). Add `-j jump` first when the host is only reachable through the bastion; retry direct if that fails. (2026-11-09 温正林提示)
- Default credential rule (2026-11-10 温正林定): for ANY host with a `qianxin-inc.cn` suffix, try `-p hhy` first when using goto, before other credentials or ad hoc auth.
- Example: `timeout 60 goto -p hhy osk12.zsy.bjat.qianxin-inc.cn "sudo dmesg | tail -20"`.
- Always use the full FQDN as the target. Bare short names (e.g. `osk01`) do not resolve in DNS — verified 2026-11-10: `goto -p hhy -cmd ... osk01` fails with `lookup osk01 ... no such host` while `goto -p hhy -cmd ... osk01.skill.zzzp.qianxin-inc.cn` works. Batch mode is host-first: the target must be a positional argument; `-f`/`-l` are only filters/list selectors — running with a filter but no positional target produces empty output and exit 0, which looks like "no such host" but is really a missing target.
- Read-only by default. Any write on a prod host still needs 温正林's explicit single-item confirmation (environment + action). Wrap every remote command in `timeout 60`.
- Global rule (2026-11-10 温正林定): when using goto on ANY host, execute read-only commands only — no writes, no state changes, nothing potentially damaging. Any write or destructive action (mount, delete, restart, config change, package install, file copy to the host) requires explicit human approval first, stated per action. Do not infer authorization from context; ask.

## Installation
```bash
wget -O ~/.local/bin/goto http://soft.yun.qianxin-inc.cn/xcloud/util/bin/goto
chmod +x ~/.local/bin/goto
```

For an installed binary:

```bash
goto <host> uptime
goto -cmd='uname -a' <host>
echo 'df -h' | goto <host>
goto -j <jump-host> <host> uptime
goto -cred=vm <host> uptime
goto -setup-nopass -p=vm <host>
```

Use `-v` only when debugging the connection path, because it intentionally enables verbose local logs:

```bash
goto -v <host> uptime
```

use goto -h for more info

## Pre-flight: Config Check

Before using `goto`, always check that the config file exists. Search order:

1. `~/.ssh/goto.yaml` (primary)
2. `~/.goterm/config.yaml` (legacy fallback)

If neither exists, **do not attempt the SSH command** (it will fail with auth errors). Instead, ask the user which variant they need (direct or jump), then create the file and retry.

If a config exists, inspect it before choosing the connection path:

1. If the target host has `jump: <name>`, use that configured jump path first.
2. If the config has `hosts:` with an entry `name: jump`, try `-j jump` first.
3. If the user supplied a jump host or the task context names one, use `-j <jump>` first.
4. If no jump path is configured or the jump attempt fails, retry direct without `-j`.

Prefer the configured jump-first flow for existing configs because many internal hosts are only reachable through a bastion, but keep the direct retry as the fallback path.

**Variant A: Direct**

```yaml
# ~/.ssh/goto.yaml
# demo: goto -user=hhy-ceph -p=hhy oska27.xcloud.bjat.qianxin-inc.cn "sudo dmesg | tail -10"
creds:
- name: hhy
  user: hhy-ceph
  pass: xxx           # or remove pass to use keypath only
  #keypath: ~/.ssh/id_rsa  # optional; omit to use ~/.ssh/id_rsa by default
hosts: []              # add hosts as needed
```

**Variant B: Jump (bastion/proxy)**

```yaml
# ~/.ssh/goto.yaml
# demo: goto -j jump -user=hhy-ceph -p=hhy oska27.xcloud.bjat.qianxin-inc.cn "sudo dmesg | tail -10"
creds:
- name: hhy
  user: hhy-ceph
  pass: xxx
- name: jump
  user: wenzhenglin              # key-only auth; pass ignored for jump
hosts:
- name: jump
  host: localhost
  cred: jump
  port: 22
```

After the user creates the config, retry the `goto` command.

## Hosts And Auth

Primary config path:

```text
~/.ssh/goto.yaml
```

Legacy fallback:

```text
~/.goterm/config.yaml
```

Config shape:

```yaml
creds:
- name: vm
  user: root
  pass: password
  keypath: ~/.ssh/id_rsa
hosts:
- name: vm1
  host: 10.47.120.11
  cred: vm
  port: 22
  initcmds: "sudo su -\n"
```

Auth rules:

- `-p` overrides the password for a single command.
- If `-p` matches a configured credential name, use that credential's password.
- If `-p` matches a configured credential name, reuse that credential's user, password, and keypath unless `-user` or `-keypath` explicitly overrides them.
- `-cred=<name>` selects a configured credential for auth and fails if it does not exist.
- `-show-cred=<name>` prints credential info and exits.
- If `-p` does not match a credential name, treat the value as a plain password.
- `-p base64:<value>` decodes a base64 password.
- `pass: base64:<value>` is also supported in config.
- If password is empty, `goto` uses key-based auth.
- `keypath` is a private key path, for example `~/.ssh/id_rsa`, not a `.pub` file.
- Quote `pass:` values in `goto.yaml` when they contain YAML special characters such as `%`.
- Inline targets can include user and port: `root@10.47.120.11:2222`.
- `-j` enables a jump host. The jump host is resolved from local config or inline `user@host:port`.
- Host config can set `jump: <name>` as the default jump host for that target.
- Explicit `-j` overrides the host config `jump`.
- Jump host auth is key-only; its config `pass` is ignored.
- Do not read config from the jump host. Target and jump host config both come from the local config file.

## Setup Nopass Login

Use setup mode to install the selected local public key on the target and create an OpenSSH config entry:

```bash
goto -setup-nopass -p=vm 10.47.120.11
goto -setup-nopass -force -p=vm 10.47.120.11
```

Known-good `hhy` example:

```text
$ goto -setup-nopass -p=hhy 10.248.26.16
nopass login configured: 10.248.26.16 -> hhy-ceph@10.248.26.16:22
```

Behavior:

- `-setup-nopass` appends the selected public key to remote `~/.ssh/authorized_keys` idempotently.
- The selected key is the credential `keypath`, explicit `-keypath`, or default `~/.ssh/id_rsa`.
- The main `~/.ssh/config` is given a top-level `Include config.d/*` line when missing.
- Generated host entries are written one file per host/IP under `~/.ssh/config.d/<host>`.
- If the exact `Host` entry already exists in the main config or another included file, setup stops.
- `-force` replaces only the generated per-host file for that exact host; it does not overwrite manual config elsewhere.

## File Transfer (goto put / goto get)

`goto put [-r] [-v] <local> <remote>` uploads one local path; `goto get [-r]
[-v] <remote> <local>` downloads one remote path to the local machine. A
remote side parses as `[user@]host:path` (scp semantics: split at the first
`:`, only when the text before it contains no `/`). One source, one
destination; when the destination is an existing directory or ends with `/`,
the source is copied into it under its base name, otherwise the destination is
the exact target path with missing parents created. `-r` is required for
directories; symlinks in a recursive tree are rejected with the offending path.
The remote side resolves through the same target resolution as login
(goto.yaml, `~/.ssh/config`, `-cred/-p/-user/-port/-j`), and the remote spec
has no port field — use `-port` or config.

```bash
goto put file.txt 11:/root/            # upload
goto put -r ./dir 11:/root/dst         # upload a directory recursively
goto get 11:/root/file.txt ./          # download
goto get -r -v 11:/root/dir /tmp/back  # download a directory, print each path
```

**Copying INTO a host is a write operation on that host.** Under this skill's
read-only rule, any `goto put` upload needs explicit human approval for the
environment and action, exactly like other writes. Downloads (`goto get`)
only read from the remote host.

## Remote Forward (-R)

`-R [bind_address:]port:host:hostport` opens a listener on the **remote** host that forwards each
connection back to the **local** machine, where `host:hostport` is dialed. Repeat the flag for
several forwards. goto keeps the listeners for the whole session.

The usual reason to need it: a remote host with no egress, where the working proxy lives on the
operator's laptop.

```bash
goto -R 40000:127.0.0.1:40000 -p xc 10.59.72.36                          # same port both sides, usual case
goto -R 14000:127.0.0.1:40000 -p xc 10.59.72.36                          # remote 14000 -> local 40000
goto -R 14000:127.0.0.1:40000 -R 14001:127.0.0.1:40000 -p xc 10.59.72.36  # repeatable
```

Then inside the remote shell: `export http_proxy=http://127.0.0.1:14000 https_proxy=$http_proxy`.
`-initcmds` can do that at login.

Facts worth remembering (verified 2026-11-20 against 10.59.72.36):

- Without a bind address the listener binds remote `localhost`, so a network port scan of the
  remote host does not see it, and only processes on that host can use it. This is the safe form.
- `0.0.0.0` or `*` needs `GatewayPorts yes` on the remote sshd; most hosts run `gatewayports no`,
  so keep the loopback form instead of asking the owner to widen it.
- Check the remote side supports it first: `sshd -T | grep -i allowtcpforwarding` must say `yes`.
- Env vars only help the shell you exported them in. Package managers and daemons need their own
  config (yum.conf `proxy=`, docker `~/.docker/config.json` or daemon proxies).
- The forward dies with the goto session. It is a debugging and access bridge, not a replacement
  for real egress on the remote network.

The implementation lives in `ssh/ssh.go`: `ParseRemoteForward` parses the spec,
`startRemoteForwards` opens `client.Listen` right after `dialTarget`, `forward` copies bytes, and
`Close` tears the listeners down.

## File Transfer Fallback: -R Tunnel Plus curl (verified 2026-11-20)

`goto put` / `goto get` above are the normal way to move files. Reach for the `-R` tunnel below when
they cannot serve the case: the remote sshd has no `sftp` subsystem, the transfer must resume or run
into the tens of gigabytes, or the remote host can only reach out through the tunnel.

Two shortcuts that look like they should transfer something but do not:

- **stdin is not piped to the remote command.** `main()` in `main.go` reads stdin as *the command*
  when no positional command is given (see `readCommandFromStdin`), and batch mode's
  `SSHTerminal.Run` sets only `Stdout`/`Stderr` on the session. The session's `StdinPipe` is taken
  only in `interactiveSession`; the other `StdinPipe` in that file belongs to the local
  `ProxyCommand` process, not to the remote command. Measured on 10.59.72.36:
  `cat hello.bin | goto -p xc <host> 'cat > /tmp/x.bin'` exits **0** and writes a **0-byte** file.
- **base64 in the command string** works, but only for small files; it hits the remote argument limit.

Working path — hold a session with `-R` and serve locally:

```bash
# operator host: serve the directory, hold the tunnel in the background
(cd /path/to/dir && python3 -m http.server 18080 --bind 127.0.0.1 &)
goto -R 18080:127.0.0.1:18080 -p xc 10.59.72.36 'sleep 7200' &

# remote host: pull (large files, resumable, checksum it)
curl -O http://127.0.0.1:18080/pkg.tar.gz && sha256sum pkg.tar.gz
```

Verified both directions on 2026-11-20 with `sha256sum` equality: download (`curl -O`, 949 MB image
at ~15 MB/s) and upload (`curl -T file http://127.0.0.1:18080/` against a tiny python PUT handler).
No inbound reachability to the operator host is needed, because the SSH connection dials outward.

Gotchas when driving this from an agent shell:

- Hold the tunnel with an explicit long command (`'sleep 7200'`), detached (`setsid nohup ... &`).
  Without a command goto goes interactive; when the hold command ends, the tunnel and its listeners
  die with the session.
- A second `goto -R` on a port the first tunnel already bound fails with
  `remote forward ... request denied by peer` and the **first** tunnel keeps working, so a stale
  tunnel hides a broken new one. Check `ss -lntp | grep <port>` on the remote before trusting it.
- Never use `pkill -f <pattern>` where the pattern also appears in your own command line: it kills
  your own shell (exit 143), and any `&&` cleanup after it never runs. Match on process name
  (`pgrep -x goto`, `pgrep -x docker`) or on a pid you already resolved from `ss`.

## Editing The Implementation

Important files:

- `main.go`: CLI flags, host-first argument parsing, stdin command fallback, password decoding, exit-status propagation, and the shared `resolveTarget` used by both login and `goto put`/`goto get`.
- `putget.go`: `goto put`/`goto get` file transfer (scp-style side parsing, sftp upload/download, recursive plans, mode propagation).
- `nopass.go`: `-setup-nopass` public-key installation and generated OpenSSH config entries.
- `ssh/ssh.go`: SSH session setup, jump host tunneling, interactive shell, batch `Run`, stdout/stderr wiring, key auth, remote forwards.
- `config/config.go`: config search order and host/credential lookup.
- `README.md`: user-facing examples should match `goto -h`.

When changing batch execution, keep the interactive and batch paths separate:

```text
interactive: Start -> connect -> RequestPty -> Shell -> initcmds
batch:       Run   -> connect -> sess.Run(cmd)
```

Do not add wrappers that capture, reformat, prefix, trim, or summarize remote stdout/stderr unless the user explicitly asks for a new output mode.



## usage

```bash
  -R value
    	remote port forward, repeatable: [bind_address:]port:host:hostport
  -V	print version
  -cmd string
    	command to run in batch mode
  -cred string
    	credential name for auth
  -f string
    	regexp filter for host
  -force
    	replace existing exact Host entry when used with -setup-nopass
  -initcmds string
    	init cmds after login
  -j string
    	jump host name or user@host:port; key auth only
  -keypath string
    	private key path, e.g. ~/.ssh/id_rsa (default "/home/wenzhenglin/.ssh/id_rsa")
  -l string
    	label filter for host
  -p string
    	credential name or plain password; use base64:<value> to decode
  -port string
    	port to connect
  -setup-nopass
    	install public key on target and save ~/.ssh/config.d/<host> entry
  -show-cred string
    	show credential info by name and exit
  -user string
    	user to auth
  -v	verbose output

Usage: goto <name>
       goto -V
       goto -show-cred=<name>
       goto <name|ip[:port]|expr|pattern> <cmd...>
       echo 'uptime' | goto <name|ip[:port]|expr|pattern>
       goto [-cmd='uptime'] <name|ip[:port]|expr|pattern>
       goto [-cred=<name>] <ip[:port]>
       goto [-p=password] <ip[:port]>
       goto -setup-nopass [-force] <name|ip[:port]>
       goto [-j=<jump>] <name|ip[:port]|expr|pattern> <cmd...>
       goto [-port=2222] [-initcmds='sudo su -\n'] <name|ip[:port]|expr|pattern>
       goto -R [bind_address:]port:host:hostport <name|ip[:port]|expr|pattern>

Examples:
  goto 11                                      # interactive login using config host 11
  goto root@10.47.120.11:2222                 # interactive login with inline user and port
  goto -cred=vm 10.47.120.11                  # use user+password+keypath from configured credential vm
  goto -p=vm 10.47.120.11                     # use user+password+keypath from configured credential vm
  goto -user=root -p=vm 10.47.120.11          # override user, use password from credential vm
  goto -p=password 10.47.120.11               # direct plain password login
  goto -user=root -p=base64:cGFzc3dvcmQ= 10.47.120.11
  goto -keypath=~/.ssh/id_rsa root@10.47.120.11
  goto -setup-nopass -p=vm 10.47.120.11       # install local public key and save ~/.ssh/config.d/<host> entry
  goto -setup-nopass -force -p=vm 10.47.120.11 # replace existing exact Host entry
  goto 11 uptime                              # batch command; uses host jump config when jump is set
  goto -j=bastion 11 uptime                   # override with explicit jump host bastion
  goto internal-vm uptime                     # can resolve HostName/User/Port/IdentityFile/ProxyCommand from ~/.ssh/config
  goto -cmd='uname -a' 11                     # batch command from -cmd
  echo 'df -h' | goto 11                      # batch command from stdin
  goto -v 11 uptime                           # verbose batch execution
  goto -l prod                                # list hosts by label
  goto -f '10\.47\.120'                       # list hosts by regexp
  goto -initcmds='sudo su -\n' 11             # interactive login, then run sudo su -
  goto -R 40000:127.0.0.1:40000 11           # remote 127.0.0.1:40000 -> local :40000
  goto -R 0.0.0.0:8080:10.0.0.5:80 11        # repeatable; needs GatewayPorts on the remote sshd

Config example:
  $ cat ~/.ssh/goto.yaml
  creds:
  - name: vm
    user: root
    pass: password
    keypath: ~/.ssh/id_rsa  # optional private key path; omit to use ~/.ssh/id_rsa
  - name: jump
    user: root
    keypath: ~/.ssh/id_rsa
  hosts:
  - name: 11
    host: 10.47.120.11
    cred: vm
    port: 22
    jump: bastion
    initcmds: "sudo su -\n"
  - name: bastion
    host: 10.47.120.10
    cred: jump
    port: 22

keypath is a private key file, for example ~/.ssh/id_rsa, not id_rsa.pub.
If pass is empty, goto uses key-based auth with keypath.
Config is read from ~/.ssh/goto.yaml; legacy ~/.goterm/config.yaml still works.
initcmds is only for interactive mode because it writes commands into the opened shell after login. Batch mode ignores it.
Use -p with a credential name to reuse its user, password and keypath; or with any other value as a plain password.
Use -cred=<name> to select a configured credential for login.
Use -show-cred=<name> to show credential info (user, pass, keypath) and exit.
Use pass: base64:<value> or -p base64:<value> to decode a base64 password.
Quote pass values in goto.yaml when they contain YAML special characters such as %.
Use -setup-nopass to append the selected public key to the remote authorized_keys and save a local ~/.ssh/config.d/<host> entry.
Use -force with -setup-nopass to replace an existing exact Host entry in that generated per-host file.
Use host jump: <name> to set a default jump host. Use -j with a configured host name or inline user@host:port to override it.
Jump host config is resolved locally and uses key auth.
goto also reads ~/.ssh/config for HostName, User, Port, IdentityFile, and ProxyCommand.
Batch command mode writes only remote stdout to stdout, remote stderr to stderr, and exits with the remote command status.
Use -R to open a remote forward on the remote side; repeat the flag for several forwards.
The listener is bound where the remote sshd allows it, so 0.0.0.0 needs GatewayPorts yes on the remote.
The -R target host:hostport is dialed from the local machine, and forwards stay open until goto exits.
```

## Validation

Run focused local checks after editing:

```bash
go test ./...
go run . -h
go run . -V
```

For output-contract checks, use a real reachable host from the user's config when available:

```bash
goto <host> 'printf out; printf err >&2; exit 7'
echo $?
```

Expected behavior:

- stdout contains exactly `out`.
- stderr contains exactly `err`.
- local exit code is `7`.

If no reachable host is available, state that remote validation was not run and report the local tests that did run.

## Safety

- Do not invent hostnames, passwords, or private key paths.
- Do not print secrets from config or command flags.
- Ask before running destructive remote commands such as `rm`, disk formatting, service restarts, or package upgrades unless the user has clearly requested that action.
- Prefer read-only probes for diagnosis: `hostname`, `uptime`, `id`, `pwd`, `df -h`, `uname -a`, or targeted `systemctl status`.
- Leave nothing behind on prod hosts (2026-11-10 温正林定): any artifact a diagnosis creates — mounts, loop devices, temp files, copied images — must be cleaned up in the same task once the evidence is collected, and the cleanup noted in the report. Bad example: a loop-mounted forensic image left on a Ceph node (`/mnt/forensic_tfc` on osk01) with no owner, no record, 89% full — colleagues should never inherit unexplained state on prod.
- Unknown pre-existing leftovers are colleagues' work, not yours to judge: don't unmount, delete, or "clean up" objects you didn't create and can't attribute. Record what you found, who it likely belongs to, and report to 温正林 — the owner decides.
