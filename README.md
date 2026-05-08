# GoTerm

A ssh client terminal written in go

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
Usage: goterm <name>
       goterm -v
       goterm <name|ip|expr|pattern> [default]
       goterm <name|ip|expr|pattern> [vm]
       goterm [-port=2222] [-user=userfoo] [-initcmds='sudo su -\n'] <name|ip|expr|pattern> [vm] 
```

## Config

config example

```
$ cat ~/.goterm/config.yaml 
creds:
- name: vm
  user: root
  pass: password
  keypath: 
hosts:
- name: 11
  host: 10.47.120.11
  cred: vm
  port: 22
```

## Acknowledgements

Thanks to https://mritd.me/2018/11/09/go-interactive-shell/
