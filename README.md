# goto

An ssh client terminal written in Go.

This project and command were renamed from `goterm` to `goto`.
Legacy `~/.goterm/config.yaml` configuration is still supported.

with replace for expect tool in minds.

## Problems

I used to use expect for machine jumps
It got tricky issue that expect can't properly handle 
terminal resize or other issues.

Therefore this tool, also for easier hosts management.

## Features

* Friendly with tmux(terminal resize)
* Replace expect tool
* Support password and key based auth
* Share credential for many hosts
* Config file for hosts and credentials
* One line ssh login(with password, if you like)

## usage

```bash
  -cmd string
    	command to run in batch mode
  -f string
    	regexp filter for host
  -initcmds string
    	init cmds after login
  -keypath string
    	private key path, e.g. ~/.ssh/id_rsa
  -l string
    	label filter for host
  -p string
    	password to auth; use base64:<value> to decode
  -port string
    	port to connect
  -user string
    	user to auth
  -v	verbose output
  -V	print version

Usage: goto <name>
       goto -V
       goto <name|ip[:port]|expr|pattern> <cmd...>
       echo 'uptime' | goto <name|ip[:port]|expr|pattern>
       goto [-cmd='uptime'] <name|ip[:port]|expr|pattern>
       goto [-user=root] [-p=password] <ip[:port]>
       goto [-port=2222] [-user=userfoo] [-initcmds='sudo su -\n'] <name|ip[:port]|expr|pattern>

Config example:
  $ cat ~/.ssh/goto.yaml
  creds:
  - name: vm
    user: root
    pass: password
    keypath: ~/.ssh/id_rsa  # optional private key path; omit to use ~/.ssh/id_rsa
  hosts:
  - name: 11
    host: 10.47.120.11
    cred: vm
    port: 22
    initcmds: "sudo su -\n"

keypath is a private key file, for example ~/.ssh/id_rsa, not id_rsa.pub.
If pass is empty, goto uses key-based auth with keypath.
Config is read from ~/.ssh/goto.yaml; legacy ~/.goterm/config.yaml still works.
initcmds is only for interactive mode because it writes commands into the opened shell after login. Batch mode ignores it.
Use -p to specify a password directly without reading credentials from config.
Use pass: base64:<value> or -p base64:<value> to decode a base64 password.
Batch command mode writes only remote stdout to stdout, remote stderr to stderr, and exits with the remote command status.
```

## Acknowledgements

Thanks to https://mritd.me/2018/11/09/go-interactive-shell/
