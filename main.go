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

const version = "v1.0.7"

func helpfunc() {
	flag.CommandLine.SetOutput(os.Stdout)
	flag.PrintDefaults()
	fmt.Print(`
Usage: goto <name>
       goto -V
       goto <name|ip[:port]|expr|pattern> <cmd...>
       echo 'uptime' | goto <name|ip[:port]|expr|pattern>
       goto [-cmd='uptime'] <name|ip[:port]|expr|pattern>
       goto [-user=root] [-p=password] <ip[:port]>
       goto [-port=2222] [-user=userfoo] [-initcmds='sudo su -\n'] <name|ip[:port]|expr|pattern>

Examples:
  goto 11                                      # interactive login using config host 11
  goto root@10.47.120.11:2222                 # interactive login with inline user and port
  goto -user=root -p=password 10.47.120.11    # direct password login without config
  goto -user=root -p=base64:cGFzc3dvcmQ= 10.47.120.11
  goto -keypath=~/.ssh/id_rsa root@10.47.120.11
  goto 11 uptime                              # batch command from positional args
  goto -cmd='uname -a' 11                     # batch command from -cmd
  echo 'df -h' | goto 11                      # batch command from stdin
  goto -v 11 uptime                           # verbose batch execution
  goto -l prod                                # list hosts by label
  goto -f '10\.47\.120'                       # list hosts by regexp
  goto -initcmds='sudo su -\n' 11             # interactive login, then run sudo su -

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
`)
}
func main() {
	var (
		port    string
		user    string
		pass    string
		keyPath string
		cmds    string
		cmd     string
		label   string
		filter  string
		verbose bool
		showVer bool
	)
	flag.StringVar(&port, "port", "", "port to connect")
	flag.StringVar(&user, "user", "", "user to auth")
	flag.StringVar(&pass, "p", "", "password to auth; use base64:<value> to decode")
	flag.StringVar(&keyPath, "keypath", defaultKeyPath(), "private key path, e.g. ~/.ssh/id_rsa")
	flag.StringVar(&cmds, "initcmds", "", "init cmds after login")
	flag.StringVar(&cmd, "cmd", "", "command to run in batch mode")
	flag.StringVar(&label, "l", "", "label filter for host")
	flag.StringVar(&filter, "f", "", "regexp filter for host")
	flag.BoolVar(&verbose, "v", false, "verbose output")
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
	keyPathSet := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == "keypath" {
			keyPathSet = true
		}
	})
	klog.V(2).Info("debug info...")

	c, configErr := config.ParseConfig()
	if configErr != nil {
		klog.V(2).Infof("parse config err: %v", configErr)
		c = &config.Config{}
	}

	if len(label) != 0 {
		if configErr != nil {
			exiterr("parse config ", configErr)
		}
		for _, v := range c.Hosts {
			if v.Label == label {
				fmt.Printf("name: %v, host: %v\n", v.Name, v.Host)
			}
		}
		return
	}
	if len(filter) != 0 {
		if configErr != nil {
			exiterr("parse config ", configErr)
		}
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
	if len(args) == 0 {
		exit("no name or ip to connect")
	}

	klog.V(2).Infof("keypath: %v", keyPath)

	klog.V(2).Info("args: ", args)
	ipStr := args[0]
	if len(cmd) == 0 {
		cmd = strings.Join(args[1:], " ")
	}
	if len(cmd) == 0 {
		var err error
		cmd, err = readCommandFromStdin()
		if err != nil {
			exiterr("read command from stdin", err)
		}
	}

	klog.V(2).Infof("get hosts: %v", ipStr)
	chost, cport, ccred, ccmds := c.GetHost(ipStr)
	klog.V(2).Infof("chost: %v, cport: %v, ccred: %v, ccmds: %v", chost, cport, ccred, ccmds)
	if len(chost) == 0 {
		// exit("there's no config for " + expr)
		klog.V(2).Info("using best effort to guess target host with default creds")
	}

	klog.V(2).Info("get cred: ", ccred)
	cuser, cpass, ckeypath := c.GetCred(ccred)
	klog.V(2).Infof("cuser: %v,cpass: %v, ckeypath: %v", cuser, cpass, ckeypath)
	if len(port) != 0 {
		cport = port
	}
	if len(user) != 0 {
		cuser = user
	}
	if len(pass) != 0 {
		cpass = pass
	}
	if keyPathSet {
		ckeypath = keyPath
	}
	if len(cmds) != 0 {
		ccmds = cmds
	}
	if len(cpass) == 0 {
		klog.V(2).Info("no password from cred config")
	}
	cpass, decodeErr := decodePassword(cpass)
	if decodeErr != nil {
		exiterr("decode password", decodeErr)
	}

	if len(cuser) == 0 {
		cuser = "root"
	}
	if len(cport) == 0 {
		cport = "22"
	}

	klog.V(2).Infof("chost: %v, cport: %v, cuser: %v, cpass: %v, ckeypath: %v, ccmds: %v", chost, cport, cuser, cpass, ckeypath, ccmds)
	// klog.Infof("connecting to %v ..., with user: %v", expr, cuser)
	startssh(chost, cuser, cport, cpass, ckeypath, ccmds, cmd, verbose)
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

func startssh(ip, user, port, pass, keypath, cmds, cmd string, verbose bool) {
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
		currentUser, _ := osuser.Current()
		if user == currentUser.Username {
			klog.Infof("connecting to ip: %v:%v ..., with user: %v, pass: ***", ip, port, user)
		} else {
			klog.Infof("connecting to ip: %v:%v ..., with user: %v, pass: %v", ip, port, user, pass)
		}
	}

	options := []ssh.Option{
		ssh.SetPort(port),
		ssh.SetKeyPath(keypath),
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
