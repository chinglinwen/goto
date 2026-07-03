package ssh

import (
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strings"
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
	ip           string
	user         string
	pass         string
	port         string
	exitMsg      string
	initcmds     string
	privKeyPath  string
	proxyCommand string

	sess       *ssh.Session
	client     *ssh.Client
	jumpClient *ssh.Client
	proxyCmd   *exec.Cmd
	proxyConn  net.Conn
	jump       *JumpHost

	stdout io.Reader
	stdin  io.Writer
	stderr io.Reader
}

type JumpHost struct {
	Host    string
	User    string
	Port    string
	KeyPath string
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
		t.privKeyPath = expandHome(keypath)
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

func SetJumpHost(jump JumpHost) Option {
	return func(t *SSHTerminal) {
		if len(jump.Host) == 0 {
			return
		}
		if len(jump.Port) == 0 {
			jump.Port = defaultSSHPort
		}
		if len(jump.User) == 0 {
			jump.User = "root"
		}
		if len(jump.KeyPath) == 0 {
			jump.KeyPath = defaultKeyPath()
		}
		jump.KeyPath = expandHome(jump.KeyPath)
		t.jump = &jump
	}
}

func SetProxyCommand(command string) Option {
	return func(t *SSHTerminal) {
		t.proxyCommand = command
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
	client, err := t.dialTarget()
	if err != nil {
		return err
	}
	t.client = client

	session, err := client.NewSession()
	if err != nil {
		_ = t.Close()
		return err
	}
	t.sess = session
	return nil
}

func (t *SSHTerminal) dialTarget() (*ssh.Client, error) {
	sshConfig, err := clientConfig(t.user, t.pass, t.privKeyPath)
	if err != nil {
		return nil, err
	}
	targetAddr := t.ip + ":" + t.port
	if t.jump == nil && t.proxyCommand != "" {
		conn, err := t.dialProxyCommand()
		if err != nil {
			return nil, err
		}
		clientConn, chans, reqs, err := ssh.NewClientConn(conn, targetAddr, sshConfig)
		if err != nil {
			_ = conn.Close()
			return nil, err
		}
		return ssh.NewClient(clientConn, chans, reqs), nil
	}
	if t.jump == nil {
		return ssh.Dial("tcp", targetAddr, sshConfig)
	}

	jumpConfig, err := clientConfig(t.jump.User, "", t.jump.KeyPath)
	if err != nil {
		return nil, err
	}
	jumpAddr := t.jump.Host + ":" + t.jump.Port
	jumpClient, err := ssh.Dial("tcp", jumpAddr, jumpConfig)
	if err != nil {
		return nil, err
	}
	t.jumpClient = jumpClient

	conn, err := jumpClient.Dial("tcp", targetAddr)
	if err != nil {
		_ = jumpClient.Close()
		return nil, err
	}
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, targetAddr, sshConfig)
	if err != nil {
		_ = conn.Close()
		_ = jumpClient.Close()
		return nil, err
	}
	return ssh.NewClient(clientConn, chans, reqs), nil
}

func (t *SSHTerminal) dialProxyCommand() (net.Conn, error) {
	command := t.expandProxyCommand()
	cmd := exec.Command("sh", "-c", command)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	local, remote := net.Pipe()
	t.proxyCmd = cmd
	t.proxyConn = local
	go func() {
		_, _ = io.Copy(stdin, local)
		_ = stdin.Close()
	}()
	go func() {
		_, _ = io.Copy(local, stdout)
		_ = local.Close()
		_ = cmd.Wait()
	}()
	return remote, nil
}

func (t *SSHTerminal) expandProxyCommand() string {
	replacer := strings.NewReplacer(
		"%h", t.ip,
		"%p", t.port,
		"%r", t.user,
		"%n", t.ip,
		"%%", "%",
	)
	return replacer.Replace(t.proxyCommand)
}

func clientConfig(user, pass, keypath string) (*ssh.ClientConfig, error) {
	auth := ssh.Password(pass)
	if len(pass) == 0 {
		signer, err := signerFromKeyPath(keypath)
		if err != nil {
			return nil, err
		}
		auth = ssh.PublicKeys(signer)
	}
	return &ssh.ClientConfig{
		User: user,
		Auth: []ssh.AuthMethod{
			auth,
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
	}, nil
}

func signerFromKeyPath(keypath string) (ssh.Signer, error) {
	keypath = expandHome(keypath)
	klog.V(2).Info("using keypath: ", keypath)
	key, err := ioutil.ReadFile(keypath)
	if err != nil {
		return nil, err
	}
	return ssh.ParsePrivateKey(key)
}

func PublicKeyForPrivateKey(keypath string) (string, error) {
	signer, err := signerFromKeyPath(keypath)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(signer.PublicKey()))), nil
}

func (t *SSHTerminal) Start() error {
	if err := t.connect(); err != nil {
		return err
	}
	return t.interactiveSession()
}

func (t *SSHTerminal) Run(cmd string) error {
	if err := t.connect(); err != nil {
		return err
	}
	t.sess.Stdout = os.Stdout
	t.sess.Stderr = os.Stderr
	return t.sess.Run(cmd)
}

func ExitStatus(err error) (int, bool) {
	if exitErr, ok := err.(*ssh.ExitError); ok {
		return exitErr.ExitStatus(), true
	}
	return 0, false
}

func (t *SSHTerminal) Close() error {
	var closeErr error
	if t.sess != nil {
		if err := t.sess.Close(); err != nil && err != io.EOF {
			closeErr = err
		}
	}
	if t.client != nil {
		if err := t.client.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if t.jumpClient != nil {
		if err := t.jumpClient.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if t.proxyConn != nil {
		if err := t.proxyConn.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	if t.proxyCmd != nil && t.proxyCmd.Process != nil {
		_ = t.proxyCmd.Process.Kill()
	}
	return closeErr
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
func expandHome(path string) string {
	if path == "~" {
		return homedir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(homedir(), path[2:])
	}
	return path
}
func homedir() string {
	usr, _ := user.Current()
	return usr.HomeDir
}
