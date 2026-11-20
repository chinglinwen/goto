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
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pkg/sftp"
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

	remoteForwards []string
	listeners      []net.Listener
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

// SetRemoteForwards registers remote forward specs, each in the form
// [bind_address:]port:host:hostport. The target host:hostport is dialed from
// the local machine, the listener is opened on the remote side.
func SetRemoteForwards(specs []string) Option {
	return func(t *SSHTerminal) {
		if len(specs) == 0 {
			return
		}
		klog.V(2).Info("set remote forwards: ", specs)
		t.remoteForwards = append(t.remoteForwards, specs...)
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
	client, err := t.Dial()
	if err != nil {
		return err
	}

	if err := t.startRemoteForwards(); err != nil {
		_ = t.Close()
		return err
	}

	session, err := client.NewSession()
	if err != nil {
		_ = t.Close()
		return err
	}
	t.sess = session
	return nil
}

// Dial returns the ssh client, dialling once and caching it for the process
// lifetime. Jump and ProxyCommand transports stay alive until Close. Callers
// that only need the raw client (for example sftp) use this instead of
// connect(), which also opens a session and remote forwards.
func (t *SSHTerminal) Dial() (*ssh.Client, error) {
	if t.client != nil {
		return t.client, nil
	}
	client, err := t.dialTarget()
	if err != nil {
		return nil, err
	}
	t.client = client
	return client, nil
}

// SFTPClient opens an sftp client over the dialled ssh client.
func (t *SSHTerminal) SFTPClient() (*sftp.Client, error) {
	client, err := t.Dial()
	if err != nil {
		return nil, err
	}
	return sftp.NewClient(client)
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

// ParseRemoteForward splits a forward spec of the form
// [bind_address:]port:host:hostport into the remote listen address and the
// local dial address.
func ParseRemoteForward(spec string) (listenAddr, dialAddr string, err error) {
	parts := strings.Split(spec, ":")
	var bindAddr, bindPort, host, hostPort string
	switch len(parts) {
	case 3:
		bindAddr, bindPort, host, hostPort = "localhost", parts[0], parts[1], parts[2]
	case 4:
		bindAddr, bindPort, host, hostPort = parts[0], parts[1], parts[2], parts[3]
	default:
		return "", "", fmt.Errorf("invalid -R %q: want [bind_address:]port:host:hostport", spec)
	}
	if len(bindAddr) == 0 || len(host) == 0 {
		return "", "", fmt.Errorf("invalid -R %q: empty bind address or host", spec)
	}
	for _, p := range []string{bindPort, hostPort} {
		n, convErr := strconv.Atoi(p)
		if convErr != nil || n < 1 || n > 65535 {
			return "", "", fmt.Errorf("invalid -R %q: bad port %q", spec, p)
		}
	}
	return net.JoinHostPort(bindAddr, bindPort), net.JoinHostPort(host, hostPort), nil
}

func (t *SSHTerminal) startRemoteForwards() error {
	for _, spec := range t.remoteForwards {
		listenAddr, dialAddr, err := ParseRemoteForward(spec)
		if err != nil {
			return err
		}
		listener, err := t.client.Listen("tcp", listenAddr)
		if err != nil {
			return fmt.Errorf("remote forward %q: %w", spec, err)
		}
		t.listeners = append(t.listeners, listener)
		klog.V(2).Info("remote forward listening: ", listenAddr, " -> ", dialAddr)
		go t.forward(listener, dialAddr)
	}
	return nil
}

func (t *SSHTerminal) forward(listener net.Listener, dialAddr string) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			defer conn.Close()
			local, err := net.Dial("tcp", dialAddr)
			if err != nil {
				klog.Errorf("remote forward dial %v err: %v", dialAddr, err)
				return
			}
			defer local.Close()
			done := make(chan struct{}, 2)
			go func() { _, _ = io.Copy(local, conn); done <- struct{}{} }()
			go func() { _, _ = io.Copy(conn, local); done <- struct{}{} }()
			<-done
		}()
	}
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
	for _, listener := range t.listeners {
		if err := listener.Close(); err != nil && closeErr == nil {
			closeErr = err
		}
	}
	t.listeners = nil
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
