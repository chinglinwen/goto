package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseSide(t *testing.T) {
	tests := []struct {
		spec       string
		wantRemote bool
		wantHost   string
		wantPath   string
	}{
		{spec: "user@host:/tmp/a", wantRemote: true, wantHost: "user@host", wantPath: "/tmp/a"},
		{spec: "host:/a/b", wantRemote: true, wantHost: "host", wantPath: "/a/b"},
		{spec: "host:rel/path", wantRemote: true, wantHost: "host", wantPath: "rel/path"},
		{spec: "user@host:file", wantRemote: true, wantHost: "user@host", wantPath: "file"},
		{spec: "./a:b", wantRemote: false, wantPath: "./a:b"},
		{spec: "/tmp/x:y", wantRemote: false, wantPath: "/tmp/x:y"},
		{spec: "a/b:c", wantRemote: false, wantPath: "a/b:c"},
		{spec: "plain.txt", wantRemote: false, wantPath: "plain.txt"},
		{spec: "/tmp/regular", wantRemote: false, wantPath: "/tmp/regular"},
		{spec: "host:/path:with:colons", wantRemote: true, wantHost: "host", wantPath: "/path:with:colons"},
	}

	for _, test := range tests {
		got := parseSide(test.spec)
		if got.remote != test.wantRemote {
			t.Errorf("parseSide(%q) remote = %v, want %v", test.spec, got.remote, test.wantRemote)
		}
		if got.host != test.wantHost {
			t.Errorf("parseSide(%q) host = %q, want %q", test.spec, got.host, test.wantHost)
		}
		if got.path != test.wantPath {
			t.Errorf("parseSide(%q) path = %q, want %q", test.spec, got.path, test.wantPath)
		}
	}
}

func TestSidesForPut(t *testing.T) {
	src, dst := parseSide("/tmp/a.txt"), parseSide("host:/remote/b.txt")

	if src.remote {
		t.Error("local source parsed as remote")
	}
	if !dst.remote {
		t.Error("remote destination parsed as local")
	}
	if dst.path != "/remote/b.txt" {
		t.Errorf("dst.path = %q, want /remote/b.txt", dst.path)
	}

	// a remote-looking argument in put position is a rejected input
	bad := parseSide("host:/remote/a.txt")
	if !bad.remote {
		t.Error("remote source spec parsed as local")
	}
	// a local-looking destination in put position is a rejected input
	badDst := parseSide("/tmp/just-local")
	if badDst.remote {
		t.Error("local destination spec parsed as remote")
	}
}

func TestSidesForGet(t *testing.T) {
	src, dst := parseSide("host:/remote/a.txt"), parseSide("/tmp/a.txt")

	if !src.remote {
		t.Error("remote source parsed as local")
	}
	if dst.remote {
		t.Error("local destination parsed as remote")
	}

	// a local-looking argument in get source position is a rejected input
	bad := parseSide("/tmp/just-local")
	if bad.remote {
		t.Error("local source spec parsed as remote")
	}
	// a remote-looking destination in get position is a rejected input
	badDst := parseSide("host:/remote/b.txt")
	if !badDst.remote {
		t.Error("remote destination spec parsed as local")
	}
}

func TestDestMode(t *testing.T) {
	tests := []struct {
		existingDir   bool
		trailingSlash bool
		wantIntoDir   bool
	}{
		{existingDir: false, trailingSlash: false, wantIntoDir: false},
		{existingDir: true, trailingSlash: false, wantIntoDir: true},
		{existingDir: false, trailingSlash: true, wantIntoDir: true},
		{existingDir: true, trailingSlash: true, wantIntoDir: true},
	}

	for _, test := range tests {
		got := destMode(test.existingDir, test.trailingSlash)
		if got != test.wantIntoDir {
			t.Errorf("destMode(dir=%v, slash=%v) = %v, want %v",
				test.existingDir, test.trailingSlash, got, test.wantIntoDir)
		}
	}
}

func TestCheckRecursive(t *testing.T) {
	dir := t.TempDir()
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = checkRecursive(info, dir, false)
	if err == nil {
		t.Fatal("dir without -r got nil err, want err")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("err = %q, want it to name the path %q", err, dir)
	}
	if !strings.Contains(err.Error(), "-r") {
		t.Errorf("err = %q, want it to mention -r", err)
	}
	if err := checkRecursive(info, dir, true); err != nil {
		t.Errorf("dir with -r got err %v", err)
	}

	file := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err = os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRecursive(info, file, false); err != nil {
		t.Errorf("file without -r got err %v", err)
	}
}

// TestPlanUploadRecursive checks the recursive plan collects relative paths of
// a temp local tree, including directories.
func TestPlanUploadRecursive(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "b.txt"), []byte("b"), 0600); err != nil {
		t.Fatal(err)
	}

	ops, err := planUpload(root, "/remote/dst")
	if err != nil {
		t.Fatalf("planUpload got err %v", err)
	}

	type wantOp struct {
		dstPath string
		isDir   bool
	}
	want := []wantOp{
		{dstPath: "/remote/dst", isDir: true},
		{dstPath: "/remote/dst/a.txt", isDir: false},
		{dstPath: "/remote/dst/sub", isDir: true},
		{dstPath: "/remote/dst/sub/b.txt", isDir: false},
	}
	if len(ops) != len(want) {
		t.Fatalf("planUpload returned %d ops, want %d: %+v", len(ops), len(want), ops)
	}
	for i, w := range want {
		if ops[i].dstPath != w.dstPath {
			t.Errorf("ops[%d].dstPath = %q, want %q", i, ops[i].dstPath, w.dstPath)
		}
		if ops[i].isDir != w.isDir {
			t.Errorf("ops[%d].isDir = %v, want %v", i, ops[i].isDir, w.isDir)
		}
	}
}

// TestPlanUploadModePropagation checks the plan carries source mode bits.
func TestPlanUploadModePropagation(t *testing.T) {
	root := t.TempDir()
	file := filepath.Join(root, "script.sh")
	if err := os.WriteFile(file, []byte("#!/bin/sh\n"), 0741); err != nil {
		t.Fatal(err)
	}

	ops, err := planUpload(file, "/remote/script.sh")
	if err != nil {
		t.Fatalf("planUpload single file got err %v", err)
	}
	if len(ops) != 1 {
		t.Fatalf("planUpload returned %d ops, want 1", len(ops))
	}
	if ops[0].mode.Perm() != 0741 {
		t.Errorf("op mode = %o, want 741", ops[0].mode.Perm())
	}
	if ops[0].isDir {
		t.Error("op isDir = true, want false for a file")
	}

	// recursive source: nested file mode is preserved
	sub := filepath.Join(root, "d")
	if err := os.Mkdir(sub, 0755); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(sub, "n.txt")
	if err := os.WriteFile(nested, []byte("n"), 0644); err != nil {
		t.Fatal(err)
	}
	ops, err = planUpload(root, "/remote")
	if err != nil {
		t.Fatalf("planUpload recursive got err %v", err)
	}
	for _, op := range ops {
		if op.dstPath == "/remote/d/n.txt" && op.mode.Perm() != 0644 {
			t.Errorf("nested op mode = %o, want 644", op.mode.Perm())
		}
	}
}

// TestPlanUploadSymlinkRejected checks symlinks anywhere in the tree error out
// with the offending path.
func TestPlanUploadSymlinkRejected(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "real.txt"), []byte("r"), 0644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sub, "link")
	if err := os.Symlink(filepath.Join(sub, "real.txt"), link); err != nil {
		t.Fatal(err)
	}

	_, err := planUpload(root, "/remote")
	if err == nil {
		t.Fatal("planUpload with symlink got nil err, want err")
	}
	if !strings.Contains(err.Error(), link) {
		t.Errorf("err = %q, want it to name the offending path %q", err, link)
	}
}

// TestPlanUploadMissingSource checks a missing local source errors.
func TestPlanUploadMissingSource(t *testing.T) {
	_, err := planUpload(filepath.Join(t.TempDir(), "missing"), "/remote")
	if err == nil {
		t.Fatal("planUpload missing source got nil err, want err")
	}
}
