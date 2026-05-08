package main

import (
	"flag"
	"fmt"
	"goterm/config"
	"goterm/ssh"
	"os"
	osuser "os/user"
	"path/filepath"
	"regexp"
	"strings"

	"k8s.io/klog/v2"
)

const version = "v1.0.3"

func helpfunc() {
	flag.PrintDefaults()
	fmt.Print(`
Usage: goterm <name>
       goterm <name|ip[:port]|expr|pattern> [default]
       goterm <name|ip[:port]|expr|pattern> [vm]
       goterm [-port=2222] [-user=userfoo] [-initcmds='sudo su -\n'] <name|ip[:port]|expr|pattern> [vm] 
`)
}
func main() {
	var (
		port    string
		user    string
		keyPath string
		cmds    string
		label   string
		filter  string
		showVer bool
	)
	flag.StringVar(&port, "port", "", "port to connect")
	flag.StringVar(&user, "user", "", "user to auth")
	flag.StringVar(&keyPath, "keypath", defaultKeyPath(), "private key auth")
	flag.StringVar(&cmds, "initcmds", "", "init cmds after login")
	flag.StringVar(&label, "l", "", "label filter for host")
	flag.StringVar(&filter, "f", "", "regexp filter for host")
	flag.BoolVar(&showVer, "v", false, "print version")
	flag.Usage = helpfunc
	klog.InitFlags(flag.NewFlagSet("klog", flag.ContinueOnError))

	flag.Parse()
	if showVer {
		fmt.Println(version)
		return
	}
	klog.V(2).Info("debug info...")

	c, err := config.ParseConfig()
	if err != nil {
		exiterr("parse config ", err)
	}

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
	if len(args) == 0 {
		exit("no name or ip to connect")
	}

	klog.V(2).Infof("keypath: %v", keyPath)

	klog.V(2).Info("args: ", args)
	ipStr := args[0]
	var cred string
	if len(args) >= 2 {
		cred = args[1]
	}

	klog.V(2).Infof("get hosts: %v, cred: %v", ipStr, cred)
	chost, cport, ccred, ccmds := c.GetHost(ipStr)
	klog.V(2).Infof("chost: %v, cport: %v, ccred: %v, ccmds: %v", chost, cport, ccred, ccmds)
	if len(chost) == 0 {
		// exit("there's no config for " + expr)
		klog.V(2).Info("using best effort to guess target host with default creds")
	}
	if len(cred) != 0 {
		klog.V(2).Infof("cred not from host config, using: %v", cred)
		ccred = cred
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
	if len(cmds) != 0 {
		ccmds = cmds
	}
	if len(cpass) == 0 {
		klog.V(2).Infof("cred not from cred config, using: %v directly as pass", cred)
		cpass = cred
	}

	if len(cuser) == 0 {
		cuser = "root"
	}
	if len(cport) == 0 {
		cport = "22"
	}

	klog.V(2).Infof("chost: %v, cport: %v, cuser: %v, cpass: %v, ckeypath: %v, ccmds: %v", chost, cport, cuser, cpass, ckeypath, ccmds)
	// klog.Infof("connecting to %v ..., with user: %v", expr, cuser)
	startssh(chost, cuser, cport, cpass, ckeypath, ccmds)
}

func startssh(ip, user, port, pass, keypath, cmds string) {
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

	currentUser, _ := osuser.Current()
	if user == currentUser.Username {
		klog.Infof("connecting to ip: %v:%v ..., with user: %v, pass: ***", ip, port, user)
	} else {
		klog.Infof("connecting to ip: %v:%v ..., with user: %v, pass: %v", ip, port, user, pass)
	}

	t := ssh.New(ip, user, pass,
		ssh.SetPort(port),
		ssh.SetKeyPath(keypath),
		ssh.SetInitCmds(cmds))
	err := t.Start()
	if err != nil {
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
