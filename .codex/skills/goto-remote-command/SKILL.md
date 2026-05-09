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
```

For an installed binary:

```bash
goto <host> uptime
goto -cmd='uname -a' <host>
echo 'df -h' | goto <host>
goto -j <jump-host> <host> uptime
```

Use `-v` only when debugging the connection path, because it intentionally enables verbose local logs:

```bash
goto -v <host> uptime
```

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
- If `-p` does not match a credential name, treat the value as a plain password.
- `-p base64:<value>` decodes a base64 password.
- `pass: base64:<value>` is also supported in config.
- If password is empty, `goto` uses key-based auth.
- `keypath` is a private key path, for example `~/.ssh/id_rsa`, not a `.pub` file.
- Inline targets can include user and port: `root@10.47.120.11:2222`.
- `-j` enables a jump host. The jump host is resolved from local config or inline `user@host:port`.
- Host config can set `jump: <name>` as the default jump host for that target.
- Explicit `-j` overrides the host config `jump`.
- Jump host auth is key-only; its config `pass` is ignored.
- Do not read config from the jump host. Target and jump host config both come from the local config file.

## Editing The Implementation

Important files:

- `main.go`: CLI flags, host-first argument parsing, stdin command fallback, password decoding, exit-status propagation.
- `ssh/ssh.go`: SSH session setup, jump host tunneling, interactive shell, batch `Run`, stdout/stderr wiring, key auth.
- `config/config.go`: config search order and host/credential lookup.
- `README.md`: user-facing examples should match `goto -h`.

When changing batch execution, keep the interactive and batch paths separate:

```text
interactive: Start -> connect -> RequestPty -> Shell -> initcmds
batch:       Run   -> connect -> sess.Run(cmd)
```

Do not add wrappers that capture, reformat, prefix, trim, or summarize remote stdout/stderr unless the user explicitly asks for a new output mode.

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
