package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/chinglinwen/goto/config"
	"github.com/chinglinwen/goto/ssh"
	"github.com/pkg/sftp"
)

// Path convention in this file: REMOTE paths are always POSIX and only go
// through the `path` package; LOCAL paths go through `filepath`. Never mix
// them: the remote side is POSIX even when the local OS is not.

// side is one side of a goto put/get command, parsed with scp semantics.
type side struct {
	remote bool
	host   string // [user@]host, set when remote
	path   string
}

// parseSide reports whether a side is remote: it is remote iff it splits at
// the first ':' and the text before the colon contains no '/', so local paths
// like ./a:b and /tmp/x:y stay local. user@ is optional.
func parseSide(s string) side {
	colon := strings.Index(s, ":")
	if colon < 0 {
		return side{path: s}
	}
	prefix := s[:colon]
	if strings.Contains(prefix, "/") {
		return side{path: s}
	}
	return side{remote: true, host: prefix, path: s[colon+1:]}
}

// splitUserHost splits a [user@]host into the user and host parts.
func splitUserHost(h string) (user, host string) {
	if i := strings.Index(h, "@"); i >= 0 {
		return h[:i], h[i+1:]
	}
	return "", h
}

// checkRecursive rejects a directory source when -r is not given.
func checkRecursive(info os.FileInfo, p string, recursive bool) error {
	if info.IsDir() && !recursive {
		return fmt.Errorf("%s is a directory; pass -r to copy directories", p)
	}
	return nil
}

// destMode decides how a single source maps onto its destination:
// intoDir means the source is copied under its base name inside the
// destination directory; otherwise the destination is the exact target path.
// The destination is treated as a directory when it already is one or ends
// with "/".
func destMode(existingDir, trailingSlash bool) (intoDir bool) {
	return existingDir || trailingSlash
}

// copyOp is one unit of work for a transfer.
type copyOp struct {
	srcPath string
	dstPath string
	mode    os.FileMode
	isDir   bool
}

// planUpload collects the copy operations for a local source into dstRoot.
// dstRoot is the exact remote destination: either the target file itself for
// a single-file source, or the target directory for a recursive source.
// Relative paths of the source tree are preserved under dstRoot. Symlinks
// anywhere in the tree are rejected.
func planUpload(srcRoot, dstRoot string) ([]copyOp, error) {
	info, err := os.Stat(srcRoot)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return []copyOp{{srcPath: srcRoot, dstPath: dstRoot, mode: info.Mode()}}, nil
	}
	var ops []copyOp
	err = filepath.WalkDir(srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("symlink not supported: %s", p)
		}
		rel, err := filepath.Rel(srcRoot, p)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		ops = append(ops, copyOp{
			srcPath: p,
			dstPath: path.Join(dstRoot, filepath.ToSlash(rel)),
			mode:    info.Mode(),
			isDir:   d.IsDir(),
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return ops, nil
}

// runTransfer implements `goto put` and `goto get`. Both resolve the remote
// side through the same resolveTarget helper the login path uses.
func runTransfer(c *config.Config, sub string, args []string, recursive, verbose bool, flags resolveFlags) {
	switch sub {
	case "put":
		runPut(c, args, recursive, verbose, flags)
	case "get":
		runGet(c, args, recursive, verbose, flags)
	default:
		exit("unknown transfer subcommand " + sub)
	}
}

func runPut(c *config.Config, args []string, recursive, verbose bool, flags resolveFlags) {
	if len(args) != 2 {
		exit("usage: goto put [-r] [-v] <local> <remote>")
	}
	src, dst := parseSide(args[0]), parseSide(args[1])
	if src.remote {
		exiterr("put", fmt.Errorf("remote source %q; use get to download", args[0]))
	}
	if !dst.remote {
		exiterr("put", fmt.Errorf("destination must be remote [user@]host:path, got %q", args[1]))
	}
	if dst.path == "" {
		exiterr("put", fmt.Errorf("missing remote path in %q", args[1]))
	}
	t, err := resolveSideTarget(c, dst.host, flags)
	if err != nil {
		exiterr("resolve target", err)
	}
	tr, sc, err := dialSFTP(t)
	if err != nil {
		exiterr("put connect", err)
	}
	defer closeSFTP(sc, tr)

	intoDir := destMode(remoteIsDir(sc, dst.path), strings.HasSuffix(dst.path, "/"))
	info, err := os.Stat(src.path)
	if err != nil {
		exiterr("put", err)
	}
	if err := checkRecursive(info, src.path, recursive); err != nil {
		exiterr("put", err)
	}
	dstRoot := dst.path
	if intoDir {
		dstRoot = path.Join(dst.path, filepath.Base(src.path))
	}
	ops, err := planUpload(src.path, dstRoot)
	if err != nil {
		exiterr("put", err)
	}
	for _, op := range ops {
		if op.isDir {
			if err := mkdirAllRemote(sc, op.dstPath); err != nil {
				exiterr("put", err)
			}
			continue
		}
		if err := putFile(sc, op.srcPath, op.dstPath, op.mode); err != nil {
			exiterr("put", err)
		}
		if verbose {
			fmt.Fprintln(os.Stderr, op.dstPath)
		}
	}
}

func runGet(c *config.Config, args []string, recursive, verbose bool, flags resolveFlags) {
	if len(args) != 2 {
		exit("usage: goto get [-r] [-v] <remote> <local>")
	}
	src, dst := parseSide(args[0]), parseSide(args[1])
	if !src.remote {
		exiterr("get", fmt.Errorf("source must be remote [user@]host:path, got %q", args[0]))
	}
	if src.path == "" {
		exiterr("get", fmt.Errorf("missing remote path in %q", args[0]))
	}
	if dst.remote {
		exiterr("get", fmt.Errorf("destination must be a local path, got remote %q; use put to upload", args[1]))
	}
	t, err := resolveSideTarget(c, src.host, flags)
	if err != nil {
		exiterr("resolve target", err)
	}
	tr, sc, err := dialSFTP(t)
	if err != nil {
		exiterr("get connect", err)
	}
	defer closeSFTP(sc, tr)

	intoDir := destMode(localIsDir(dst.path), strings.HasSuffix(dst.path, "/"))
	info, err := sc.Stat(src.path)
	if err != nil {
		exiterr("get", err)
	}
	if err := checkRecursive(info, src.path, recursive); err != nil {
		exiterr("get", err)
	}
	dstRoot := dst.path
	if intoDir {
		dstRoot = filepath.Join(dst.path, path.Base(src.path))
	}
	if info.IsDir() {
		if err := os.MkdirAll(dstRoot, info.Mode().Perm()); err != nil {
			exiterr("get", err)
		}
		// remoteWalk yields remote POSIX paths; only `path` touches them.
		err = remoteWalk(sc, src.path, func(p string, fi os.FileInfo) error {
			rel := relRemote(src.path, p)
			lp := filepath.Join(dstRoot, filepath.FromSlash(rel))
			if fi.IsDir() {
				return os.MkdirAll(lp, fi.Mode().Perm())
			}
			if err := getFile(sc, p, lp, fi); err != nil {
				return err
			}
			if verbose {
				fmt.Fprintln(os.Stderr, lp)
			}
			return nil
		})
		if err != nil {
			exiterr("get", err)
		}
		return
	}
	if err := getFile(sc, src.path, dstRoot, info); err != nil {
		exiterr("get", err)
	}
	if verbose {
		fmt.Fprintln(os.Stderr, dstRoot)
	}
}

// resolveSideTarget resolves the remote side of a put/get command. A user
// given in the [user@]host:path form wins over config and credentials.
func resolveSideTarget(c *config.Config, hostExpr string, flags resolveFlags) (target, error) {
	user, host := splitUserHost(hostExpr)
	if user != "" {
		flags.user = user
		flags.userSet = true
	}
	return resolveTarget(c, host, flags)
}

// dialSFTP opens an sftp client over the resolved target.
func dialSFTP(t target) (tr *ssh.SSHTerminal, sc *sftp.Client, err error) {
	options := []ssh.Option{
		ssh.SetPort(t.port),
		ssh.SetKeyPath(t.keypath),
	}
	if t.jumpHost != nil {
		options = append(options, ssh.SetJumpHost(*t.jumpHost))
	}
	if t.proxyCommand != "" {
		options = append(options, ssh.SetProxyCommand(t.proxyCommand))
	}
	tr = ssh.New(t.host, t.user, t.pass, options...)
	sc, err = tr.SFTPClient()
	if err != nil {
		return nil, nil, err
	}
	return tr, sc, nil
}

func closeSFTP(sc *sftp.Client, tr *ssh.SSHTerminal) {
	_ = sc.Close()
	_ = tr.Close()
}

// putFile copies one local file to an exact remote path and applies the
// source mode bits.
func putFile(sc *sftp.Client, localPath, remotePath string, mode os.FileMode) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := mkdirAllRemote(sc, path.Dir(remotePath)); err != nil {
		return err
	}
	rf, err := sc.Create(remotePath)
	if err != nil {
		return err
	}
	if _, err := io.Copy(rf, f); err != nil {
		_ = rf.Close()
		return err
	}
	if err := rf.Chmod(mode.Perm()); err != nil {
		_ = rf.Close()
		return err
	}
	return rf.Close()
}

// getFile copies one remote file to an exact local path and applies the
// remote mode bits.
func getFile(sc *sftp.Client, remotePath, localPath string, info os.FileInfo) error {
	if err := os.MkdirAll(filepath.Dir(localPath), 0755); err != nil {
		return err
	}
	rf, err := sc.Open(remotePath)
	if err != nil {
		return err
	}
	defer rf.Close()
	lf, err := os.OpenFile(localPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(lf, rf); err != nil {
		_ = lf.Close()
		return err
	}
	if err := lf.Close(); err != nil {
		return err
	}
	return os.Chmod(localPath, info.Mode().Perm())
}

// relRemote returns p relative to root. Both are remote POSIX paths and the
// computation is purely lexical slash handling, so it behaves the same on
// every host. remoteWalk builds p under root via path.Join, so the prefix
// always matches.
func relRemote(root, p string) string {
	if p == root {
		return "."
	}
	root = strings.TrimSuffix(root, "/")
	if strings.HasPrefix(p, root+"/") {
		return p[len(root)+1:]
	}
	return p
}

// remoteWalk walks a remote tree top-down. All paths here are remote POSIX
// paths, handled only with the `path` package. Symlinks anywhere in the tree
// are rejected; nothing is skipped silently.
func remoteWalk(sc *sftp.Client, root string, fn func(p string, fi os.FileInfo) error) error {
	infos, err := sc.ReadDir(root)
	if err != nil {
		return err
	}
	for _, fi := range infos {
		p := path.Join(root, fi.Name())
		if fi.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("symlink not supported: %s", p)
		}
		if err := fn(p, fi); err != nil {
			return err
		}
		if fi.IsDir() {
			if err := remoteWalk(sc, p, fn); err != nil {
				return err
			}
		}
	}
	return nil
}

// mkdirAllRemote creates dir and its missing parents on the remote side.
// dir is a remote POSIX path.
func mkdirAllRemote(sc *sftp.Client, dir string) error {
	if dir == "" || dir == "/" || dir == "." {
		return nil
	}
	if fi, err := sc.Stat(dir); err == nil {
		if fi.IsDir() {
			return nil
		}
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	parent := path.Dir(dir)
	if parent != dir {
		if err := mkdirAllRemote(sc, parent); err != nil {
			return err
		}
	}
	if err := sc.Mkdir(dir); err != nil {
		// another writer may have created it in between
		if _, statErr := sc.Stat(dir); statErr != nil {
			return err
		}
	}
	return nil
}

func remoteIsDir(sc *sftp.Client, p string) bool {
	fi, err := sc.Stat(p)
	return err == nil && fi.IsDir()
}

func localIsDir(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.IsDir()
}
