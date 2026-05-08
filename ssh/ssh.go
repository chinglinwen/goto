package ssh

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/terminal"
	"k8s.io/klog"
)

const (
	defaultSSHPort = "22"
)

type SSHTerminal struct {
	ip          string
	user        string
	pass        string
	port        string
	exitMsg     string
	initcmds    string
	privKeyPath string

	sess   *ssh.Session
	client *ssh.Client

	stdout io.Reader
	stdin  io.Writer
	stderr io.Reader
}

type Option func(*SSHTerminal)

func SetPort(port string) Option {
	return func(t *SSHTerminal) {
		if len(port) == 0 {
			return
		}
		klog.V(2).Info("set port: ", port)
		t.port = port
	}
}
func SetExitMessage(msg string) Option {
	return func(t *SSHTerminal) {
		if len(msg) == 0 {
			return
		}
		klog.V(2).Info("set exitMsg: ", msg)
		t.exitMsg = msg
	}
}

func SetKeyPath(keypath string) Option {
	return func(t *SSHTerminal) {
		if len(keypath) == 0 {
			return
		}
		klog.V(2).Info("set keypath: ", keypath)
		t.privKeyPath = keypath
	}
}

func SetInitCmds(initcmds string) Option {
	return func(t *SSHTerminal) {
		if len(initcmds) == 0 {
			return
		}
		klog.Info("set initcmds: ", initcmds)
		klog.V(2).Info("set initcmds: ", initcmds)
		t.initcmds = initcmds
	}
}

func New(ip, user, pass string, options ...Option) *SSHTerminal {
	t := &SSHTerminal{
		ip:          ip,
		user:        user,
		pass:        pass,
		port:        defaultSSHPort,
		privKeyPath: defaultKeyPath(),
	}
	for _, op := range options {
		op(t)
	}
	return t
}

func (t *SSHTerminal) connect() error {
	auth := ssh.Password(t.pass)
	// if no pass provided, use key based
	if len(t.pass) == 0 {
		signer, err := t.getSigner()
		if err != nil {
			return err
		}
		auth = ssh.PublicKeys(signer)
	}
	sshConfig := &ssh.ClientConfig{
		User: t.user,
		Auth: []ssh.AuthMethod{
			auth,
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}
	client, err := ssh.Dial("tcp", t.ip+":"+t.port, sshConfig)
	if err != nil {
		return err
	}
	t.client = client

	session, err := client.NewSession()
	if err != nil {
		return err
	}
	t.sess = session
	return nil
}

func (t *SSHTerminal) getSigner() (ssh.Signer, error) {
	klog.V(2).Info("using keypath: ", t.privKeyPath)
	key, err := ioutil.ReadFile(t.privKeyPath)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(key)
}

func (t *SSHTerminal) Start() error {
	if err := t.connect(); err != nil {
		return err
	}
	return t.interactiveSession()
}

func (t *SSHTerminal) Close() error {
	if err := t.sess.Close(); err != nil {
		return err
	}
	return t.client.Close()
}

func (t *SSHTerminal) updateTerminalSize() {

	go func() {
		// SIGWINCH is sent to the process when the window size of the terminal has
		// changed.
		sigwinchCh := make(chan os.Signal, 1)
		signal.Notify(sigwinchCh, syscall.SIGWINCH)

		fd := int(os.Stdin.Fd())
		termWidth, termHeight, err := terminal.GetSize(fd)
		if err != nil {
			klog.Errorf("getsize err: %v", err)
		}

		for {
			select {
			// The client updated the size of the local PTY. This change needs to occur
			// on the server side PTY as well.
			case sigwinch := <-sigwinchCh:
				if sigwinch == nil {
					return
				}
				currTermWidth, currTermHeight, err := terminal.GetSize(fd)

				// Terminal size has not changed, don't do anything.
				if currTermHeight == termHeight && currTermWidth == termWidth {
					continue
				}

				t.sess.WindowChange(currTermHeight, currTermWidth)
				if err != nil {
					klog.Errorf("Unable to send window-change reqest: %s.", err)
					continue
				}
				termWidth, termHeight = currTermWidth, currTermHeight
			}
		}
	}()

}

func (t *SSHTerminal) interactiveSession() error {
	defer func() {
		if t.exitMsg == "" {
			fmt.Fprintln(os.Stdout, "bye at ", time.Now().Format(time.RFC822))
		} else {
			fmt.Fprintln(os.Stdout, t.exitMsg)
		}
	}()

	fd := int(os.Stdin.Fd())
	state, err := terminal.MakeRaw(fd)
	if err != nil {
		return err
	}
	defer terminal.Restore(fd, state)

	termWidth, termHeight, err := terminal.GetSize(fd)
	if err != nil {
		return err
	}

	termType := os.Getenv("TERM")
	if termType == "" {
		termType = "xterm-256color"
	}

	err = t.sess.RequestPty(termType, termHeight, termWidth, ssh.TerminalModes{})
	if err != nil {
		return err
	}

	t.updateTerminalSize()

	t.stdin, err = t.sess.StdinPipe()
	if err != nil {
		return err
	}
	t.stdout, err = t.sess.StdoutPipe()
	if err != nil {
		return err
	}
	t.stderr, err = t.sess.StderrPipe()

	go io.Copy(os.Stderr, t.stderr)
	go io.Copy(os.Stdout, t.stdout)
	go func() {
		buf := make([]byte, 128)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				klog.Errorf("stdin read err: %v", err)
				return
			}
			if n > 0 {
				_, err = t.stdin.Write(buf[:n])
				if err != nil {
					klog.Errorf("stdin write buf err: %v", err)
					t.exitMsg = err.Error()
					return
				}
			}
		}
	}()
	err = t.sess.Shell()
	if err != nil {
		return err
	}
	t.doinitcmds()

	err = t.sess.Wait()
	if err != nil {
		return err
	}
	return nil
}

func (t *SSHTerminal) doinitcmds() {
	if len(t.initcmds) == 0 {
		return
	}
	_, err := t.stdin.Write([]byte(t.initcmds))
	if err != nil {
		klog.Errorf("stdin write buf err: %v", err)
	}
}
func defaultKeyPath() string {
	return filepath.Join(homedir(), ".ssh/id_rsa")
}
func homedir() string {
	usr, _ := user.Current()
	return usr.HomeDir
}
