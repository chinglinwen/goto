package main

import (
	"encoding/base64"
	"flag"
	"fmt"
	"io"
	"os"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/chinglinwen/goto/config"
	"github.com/chinglinwen/goto/ssh"
	"k8s.io/klog/v2"
)

const version = "v1.0.11"

func helpfunc() {
	flag.CommandLine.SetOutput(os.Stdout)
	flag.PrintDefaults()
	fmt.Print(`
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
       goto put [-r] [-v] <local> <remote>
       goto get [-r] [-v] <remote> <local>

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
  goto put file.txt 11:/root/                 # upload to host 11 from config
  goto put -r ./dir 11:/root/dst              # upload a directory recursively
  goto get 11:/root/file.txt ./               # download from host 11
  goto get -r -v 11:/root/dir /tmp/back       # download a directory, print each path
  goto put file.txt root@10.47.120.11:/a/b    # inline user@host:path also works

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
Use goto put/get to copy files over ssh; remote sides use [user@]host:path and resolve like login targets.
put uploads one local path, get downloads one remote path; -r is required for directories; symlinks are rejected.
When the destination is an existing directory or ends with /, the source is copied into it under its base name.
The remote side has no port field; use -port or config. put/get are subcommands, so a host literally named put or get needs the inline form user@put:path.
`)
}
func main() {
	var (
		port      string
		user      string
		pass      string
		keyPath   string
		jump      string
		cmds      string
		cmd       string
		label     string
		filter    string
		cred      string
		force     bool
		verbose   bool
		showVer   bool
		showCred  string
		nopass    bool
		recursive bool
		forwards  forwardList
	)
	flag.StringVar(&port, "port", "", "port to connect")
	flag.StringVar(&user, "user", "", "user to auth")
	flag.StringVar(&pass, "p", "", "credential name or plain password; use base64:<value> to decode")
	flag.StringVar(&keyPath, "keypath", defaultKeyPath(), "private key path, e.g. ~/.ssh/id_rsa")
	flag.StringVar(&jump, "j", "", "jump host name or user@host:port; key auth only")
	flag.StringVar(&cmds, "initcmds", "", "init cmds after login")
	flag.StringVar(&cmd, "cmd", "", "command to run in batch mode")
	flag.StringVar(&label, "l", "", "label filter for host")
	flag.StringVar(&filter, "f", "", "regexp filter for host")
	flag.StringVar(&cred, "cred", "", "credential name for auth")
	flag.StringVar(&showCred, "show-cred", "", "show credential info by name and exit")
	flag.BoolVar(&nopass, "setup-nopass", false, "install public key on target and save ~/.ssh/config.d/<host> entry")
	flag.BoolVar(&force, "force", false, "replace existing exact Host entry when used with -setup-nopass")
	flag.BoolVar(&verbose, "v", false, "verbose output")
	flag.BoolVar(&recursive, "r", false, "recursive copy for goto put/get")
	flag.Var(&forwards, "R", "remote port forward, repeatable: [bind_address:]port:host:hostport")
	flag.BoolVar(&showVer, "V", false, "print version")
	flag.Usage = helpfunc
	klogFlags := flag.NewFlagSet("klog", flag.ContinueOnError)
	klog.InitFlags(klogFlags)

	flag.Parse()
	if showVer {
		fmt.Println(version)
		return
	}
	if verbose {
		_ = klogFlags.Set("v", "2")
	}

	for _, spec := range forwards {
		if _, _, err := ssh.ParseRemoteForward(spec); err != nil {
			exiterr("parse -R", err)
		}
	}

	c, configErr := config.ParseConfig()
	if configErr != nil {
		exiterr("parse config ", configErr)
	}

	if len(showCred) != 0 {
		cred := c.GetCredByName(showCred)
		if cred == nil {
			exit("credential '" + showCred + "' not found")
		}
		fmt.Printf("name: %v\nuser: %v\npass: %v\nkeypath: %v\n", cred.Name, cred.User, cred.Pass, cred.Keypath)
		return
	}
	keyPathSet := false
	userSet := false
	portSet := false
	flag.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "keypath":
			keyPathSet = true
		case "user":
			userSet = true
		case "port":
			portSet = true
		}
	})
	klog.V(2).Info("debug info...")

	if len(label) != 0 {
		for _, v := range c.Hosts {
			if v.Label == label {
				fmt.Printf("name: %v, host: %v\n", v.Name, v.Host)
			}
		}
		return
	}
	if len(filter) != 0 {
		f := regexp.MustCompile(filter)
		for _, v := range c.Hosts {
			if f.MatchString(v.Label) || f.MatchString(v.Name) || f.MatchString(v.Host) {
				fmt.Printf("name: %v, host: %v\n", v.Name, v.Host)
			}
		}
		return
	}

	// if ip not provided, get it from config
	args := flag.Args()
	if len(args) > 0 && (args[0] == "put" || args[0] == "get") {
		// The global flag package stops parsing at the subcommand, so -r/-v
		// after put/get need their own FlagSet. The global flags still work.
		sub := args[0]
		subFlags := flag.NewFlagSet(sub, flag.ExitOnError)
		subRecursive := subFlags.Bool("r", false, "recursive copy")
		subVerbose := subFlags.Bool("v", false, "verbose output")
		if err := subFlags.Parse(args[1:]); err != nil {
			exit(fmt.Sprintf("usage: goto %s [-r] [-v] <src> <dst>", sub))
		}
		runTransfer(c, sub, subFlags.Args(), recursive || *subRecursive, verbose || *subVerbose, resolveFlags{
			port:       port,
			user:       user,
			pass:       pass,
			keyPath:    keyPath,
			cred:       cred,
			jump:       jump,
			keyPathSet: keyPathSet,
			userSet:    userSet,
			portSet:    portSet,
		})
		return
	}
	if len(args) == 0 {
		exit("no name or ip to connect")
	}

	klog.V(2).Infof("keypath: %v", keyPath)

	klog.V(2).Info("args: ", args)
	ipStr := args[0]
	if nopass && len(args) > 1 {
		exit("-setup-nopass does not accept remote command arguments")
	}
	if nopass && len(cmd) != 0 {
		exit("-setup-nopass does not accept -cmd")
	}
	if !nopass && len(cmd) == 0 {
		cmd = strings.Join(args[1:], " ")
	}
	if !nopass && len(cmd) == 0 {
		var err error
		cmd, err = readCommandFromStdin()
		if err != nil {
			exiterr("read command from stdin", err)
		}
	}

	klog.V(2).Infof("get hosts: %v", ipStr)
	t, err := resolveTarget(c, ipStr, resolveFlags{
		port:       port,
		user:       user,
		pass:       pass,
		keyPath:    keyPath,
		cred:       cred,
		cmds:       cmds,
		jump:       jump,
		keyPathSet: keyPathSet,
		userSet:    userSet,
		portSet:    portSet,
	})
	if err != nil {
		exiterr("resolve target", err)
	}

	if nopass {
		alias := nopassAlias(ipStr)
		if err := setupNopass(t.host, t.user, t.port, t.pass, t.keypath, alias, force, t.jumpHost, t.proxyCommand); err != nil {
			exiterr("setup nopass", err)
		}
		fmt.Printf("nopass login configured: %s -> %s@%s:%s\n", alias, t.user, t.host, t.port)
		return
	}
	// klog.Infof("connecting to %v ..., with user: %v", expr, cuser)
	startssh(t.host, t.user, t.port, t.pass, t.keypath, t.cmds, cmd, verbose, t.jumpHost, t.proxyCommand, forwards)
}

// resolveFlags carries the global flags that override target resolution.
type resolveFlags struct {
	port       string
	user       string
	pass       string
	keyPath    string
	cred       string
	cmds       string
	jump       string
	keyPathSet bool
	userSet    bool
	portSet    bool
}

// target is the fully resolved connection description for one remote host.
type target struct {
	host         string
	user         string
	port         string
	pass         string
	keypath      string
	cmds         string
	jumpHost     *ssh.JumpHost
	proxyCommand string
}

// resolveTarget resolves a host expression to a concrete connection target.
// It goes through goto.yaml hosts, ~/.ssh/config, credentials, flag overrides,
// jump host resolution and password decoding. Both login and `goto put`/`goto get` use it.
func resolveTarget(c *config.Config, hostExpr string, flags resolveFlags) (target, error) {
	chost, cport, ccred, ccmds, cjump := c.GetHost(hostExpr)
	klog.V(2).Infof("chost: %v, cport: %v, ccred: %v, ccmds: %v, cjump: %v", chost, cport, ccred, ccmds, cjump)
	_, sshConfigName, _ := parseHost(hostExpr, "", "")
	sshConfigHost := config.GetSSHConfigHost(sshConfigName)
	if sshConfigHost.Found {
		if sshConfigHost.HostName != "" {
			chost = sshConfigHost.HostName
		}
		if !flags.portSet && sshConfigHost.Port != "" {
			cport = sshConfigHost.Port
		}
	}
	if len(chost) == 0 {
		// exit("there's no config for " + expr)
		klog.V(2).Info("using best effort to guess target host with default creds")
	}

	klog.V(2).Info("get cred: ", ccred)
	cuser, cpass, ckeypath := c.GetCred(ccred)
	klog.V(2).Infof("cuser: %v,cpass: ***, ckeypath: %v", cuser, ckeypath)
	if len(flags.port) != 0 {
		cport = flags.port
	}
	if len(flags.user) != 0 {
		cuser = flags.user
	}
	if !flags.userSet && sshConfigHost.User != "" {
		cuser = sshConfigHost.User
	}
	if len(flags.cred) != 0 {
		var found bool
		cuser, cpass, ckeypath, found = applyCredential(c, flags.cred, cuser, cpass, ckeypath, flags.userSet, flags.keyPathSet)
		if !found {
			return target{}, fmt.Errorf("credential '%v' not found", flags.cred)
		}
	}
	if len(flags.pass) != 0 {
		var found bool
		cuser, cpass, ckeypath, found = applyCredential(c, flags.pass, cuser, cpass, ckeypath, flags.userSet, flags.keyPathSet)
		if !found {
			cpass = flags.pass
		}
	}
	if !flags.keyPathSet && sshConfigHost.IdentityFile != "" {
		ckeypath = sshConfigHost.IdentityFile
	}
	if flags.keyPathSet {
		ckeypath = flags.keyPath
	}
	if len(flags.cmds) != 0 {
		ccmds = flags.cmds
	}
	if len(flags.jump) != 0 {
		cjump = flags.jump
	}
	if len(cpass) == 0 {
		klog.V(2).Info("no password from cred config")
	}
	cpass, decodeErr := decodePassword(cpass)
	if decodeErr != nil {
		return target{}, fmt.Errorf("decode password: %w", decodeErr)
	}

	if len(cuser) == 0 {
		cuser = "root"
	}
	if len(cport) == 0 {
		cport = "22"
	}

	var jumpHost *ssh.JumpHost
	if len(cjump) != 0 {
		var err error
		jumpHost, err = resolveJumpHost(c, cjump)
		if err != nil {
			return target{}, fmt.Errorf("resolve jump host: %w", err)
		}
	}

	klog.V(2).Infof("chost: %v, cport: %v, cuser: %v, cpass: ***, ckeypath: %v, ccmds: %v", chost, cport, cuser, ckeypath, ccmds)
	return target{
		host:         chost,
		user:         cuser,
		port:         cport,
		pass:         cpass,
		keypath:      ckeypath,
		cmds:         ccmds,
		jumpHost:     jumpHost,
		proxyCommand: sshConfigHost.ProxyCommand,
	}, nil
}

// forwardList collects repeatable -R forward specs.
type forwardList []string

func (f *forwardList) String() string {
	return strings.Join(*f, ",")
}

func (f *forwardList) Set(v string) error {
	*f = append(*f, v)
	return nil
}

func readCommandFromStdin() (string, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	if stat.Mode()&os.ModeCharDevice != 0 {
		return "", nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func decodePassword(pass string) (string, error) {
	const prefix = "base64:"
	if !strings.HasPrefix(pass, prefix) {
		return pass, nil
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(pass, prefix))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func resolvePassword(c *config.Config, pass string) string {
	if c == nil {
		return pass
	}
	for _, cred := range c.Creds {
		if cred.Name == pass {
			return cred.Pass
		}
	}
	return pass
}

func applyCredential(c *config.Config, name, currentUser, currentPass, currentKeypath string, userSet, keyPathSet bool) (user, pass, keypath string, found bool) {
	user, pass, keypath = currentUser, currentPass, currentKeypath
	if c == nil {
		return user, pass, keypath, false
	}
	cred := c.GetCredByName(name)
	if cred == nil {
		return user, pass, keypath, false
	}
	pass = cred.Pass
	if !userSet && len(cred.User) != 0 {
		user = cred.User
	}
	if !keyPathSet && len(cred.Keypath) != 0 {
		keypath = cred.Keypath
	}
	return user, pass, keypath, true
}

func resolveJumpHost(c *config.Config, expr string) (*ssh.JumpHost, error) {
	if c == nil {
		c = &config.Config{}
	}
	host, port, cred, _, _ := c.GetHost(expr)
	user, _, keypath := c.GetCred(cred)
	user, host, port = parseHost(host, user, port)
	if len(host) == 0 {
		return nil, fmt.Errorf("empty jump host")
	}
	if len(user) == 0 {
		user = "root"
	}
	if len(port) == 0 {
		port = "22"
	}
	if len(keypath) == 0 {
		keypath = defaultKeyPath()
	}
	return &ssh.JumpHost{
		Host:    host,
		User:    user,
		Port:    port,
		KeyPath: keypath,
	}, nil
}

func startssh(ip, user, port, pass, keypath, cmds, cmd string, verbose bool, jumpHost *ssh.JumpHost, proxyCommand string, forwards []string) {
	tuser, tip, tport := parseHost(ip, user, port)
	if len(tip) != 0 {
		ip = tip
	}
	if len(tuser) != 0 {
		user = tuser
	}
	if len(tport) != 0 {
		port = tport
	}

	if len(cmd) == 0 || verbose {
		klog.Infof("connecting to ip: %v:%v ..., with user: %v, pass: ***", ip, port, user)
	}

	options := []ssh.Option{
		ssh.SetPort(port),
		ssh.SetKeyPath(keypath),
	}
	if jumpHost != nil {
		options = append(options, ssh.SetJumpHost(*jumpHost))
	}
	if proxyCommand != "" {
		options = append(options, ssh.SetProxyCommand(proxyCommand))
	}
	if len(forwards) != 0 {
		options = append(options, ssh.SetRemoteForwards(forwards))
	}
	if len(cmd) == 0 {
		options = append(options, ssh.SetInitCmds(cmds))
	}
	t := ssh.New(ip, user, pass, options...)
	var err error
	if len(cmd) != 0 {
		err = t.Run(cmd)
	} else {
		err = t.Start()
	}
	if err != nil {
		if len(cmd) != 0 {
			if status, ok := ssh.ExitStatus(err); ok {
				t.Close()
				os.Exit(status)
			}
		}
		exiterr("start term", err)
	}
	t.Close()
}

func exit(msg string) {
	klog.Errorf(msg)
	os.Exit(1)
}

func exiterr(msg string, err error) {
	klog.Errorf("%v, err: %v", msg, err)
	os.Exit(1)
}

func defaultKeyPath() string {
	return filepath.Join(homedir(), ".ssh/id_rsa")
}
func homedir() string {
	usr, _ := osuser.Current()
	return usr.HomeDir
}

func parseHost(host, originUser, originPort string) (user, ip, port string) {
	user = originUser
	port = originPort
	if len(port) == 0 {
		port = "22"
	}
	if strings.Contains(host, "@") {
		user = strings.Split(host, "@")[0]
		if len(user) == 0 {
			user = originUser
		}
		if len(user) == 0 {
			user = originUser
		}
		ip = strings.Split(host, "@")[1]
	} else {
		ip = host
	}

	if strings.Contains(ip, ":") {
		ipStr := strings.Split(ip, ":")[0]
		portStr := strings.Split(ip, ":")[1]
		if len(portStr) == 0 {
			portStr = port
		}
		if len(portStr) == 0 {
			portStr = port
		}
		return user, ipStr, portStr
	}
	return user, ip, port
}
