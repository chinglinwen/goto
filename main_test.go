package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chinglinwen/goto/config"
)

func TestParseHost(t *testing.T) {
	tests := []struct {
		host       string
		originUser string
		originPort string
		wantUser   string
		wantIP     string
		wantPort   string
	}{
		{
			host:       "user@ip",
			originUser: "",
			originPort: "",
			wantUser:   "user",
			wantIP:     "ip",
			wantPort:   "22",
		},
		{
			host:       "ip",
			originUser: "root",
			originPort: "",
			wantUser:   "root",
			wantIP:     "ip",
			wantPort:   "22",
		},
		{
			host:       "ip:port",
			originUser: "",
			originPort: "",
			wantUser:   "",
			wantIP:     "ip",
			wantPort:   "port",
		},
		{
			host:       "ip:port",
			originUser: "foo",
			originPort: "",
			wantUser:   "foo",
			wantIP:     "ip",
			wantPort:   "port",
		},
		{
			host:       "user@ip:port",
			originUser: "",
			originPort: "",
			wantUser:   "user",
			wantIP:     "ip",
			wantPort:   "port",
		},
	}

	for _, test := range tests {
		user, ip, port := parseHost(test.host, test.originUser, test.originPort)
		if user != test.wantUser {
			t.Errorf("parseHost(%q, %q, %q) = user %q, want %q", test.host, test.originUser, test.originPort, user, test.wantUser)
		}
		if ip != test.wantIP {
			t.Errorf("parseHost(%q, %q, %q) = ip %q, want %q", test.host, test.originUser, test.originPort, ip, test.wantIP)
		}
		if port != test.wantPort {
			t.Errorf("parseHost(%q, %q, %q) = port %q, want %q", test.host, test.originUser, test.originPort, port, test.wantPort)
		}
	}
}

func TestDecodePassword(t *testing.T) {
	tests := []struct {
		name    string
		pass    string
		want    string
		wantErr bool
	}{
		{
			name: "plain",
			pass: "password",
			want: "password",
		},
		{
			name: "base64",
			pass: "base64:cGFzc3dvcmQ=",
			want: "password",
		},
		{
			name:    "invalid base64",
			pass:    "base64:not base64",
			wantErr: true,
		},
	}

	for _, test := range tests {
		got, err := decodePassword(test.pass)
		if test.wantErr {
			if err == nil {
				t.Errorf("decodePassword(%q) got nil err, want err", test.pass)
			}
			continue
		}
		if err != nil {
			t.Errorf("decodePassword(%q) got err %v", test.pass, err)
			continue
		}
		if got != test.want {
			t.Errorf("decodePassword(%q) = %q, want %q", test.pass, got, test.want)
		}
	}
}

func TestResolvePassword(t *testing.T) {
	c := &config.Config{
		Creds: []struct {
			Name    string `yaml:"name"`
			User    string `yaml:"user"`
			Pass    string `yaml:"pass"`
			Keypath string `yaml:"keypath"`
		}{
			{
				Name: "vm",
				Pass: "secret",
			},
			{
				Name: "encoded",
				Pass: "base64:c2VjcmV0",
			},
		},
	}

	tests := []struct {
		name string
		pass string
		want string
	}{
		{
			name: "credential name",
			pass: "vm",
			want: "secret",
		},
		{
			name: "credential base64 password stays encoded for later decode",
			pass: "encoded",
			want: "base64:c2VjcmV0",
		},
		{
			name: "plain password",
			pass: "password",
			want: "password",
		},
		{
			name: "nil config",
			pass: "password",
			want: "password",
		},
	}

	for _, test := range tests {
		candidate := c
		if test.name == "nil config" {
			candidate = nil
		}
		got := resolvePassword(candidate, test.pass)
		if got != test.want {
			t.Errorf("resolvePassword(%q) = %q, want %q", test.pass, got, test.want)
		}
	}
}

func TestApplyCredential(t *testing.T) {
	c := &config.Config{
		Creds: []struct {
			Name    string `yaml:"name"`
			User    string `yaml:"user"`
			Pass    string `yaml:"pass"`
			Keypath string `yaml:"keypath"`
		}{
			{
				Name:    "vm",
				User:    "admin",
				Pass:    "secret",
				Keypath: "/tmp/key",
			},
		},
	}

	tests := []struct {
		name           string
		cred           string
		currentUser    string
		currentPass    string
		currentKeypath string
		userSet        bool
		keyPathSet     bool
		wantUser       string
		wantPass       string
		wantKeypath    string
		wantFound      bool
	}{
		{
			name:        "credential applies user password and keypath",
			cred:        "vm",
			wantUser:    "admin",
			wantPass:    "secret",
			wantKeypath: "/tmp/key",
			wantFound:   true,
		},
		{
			name:        "explicit user is preserved",
			cred:        "vm",
			currentUser: "root",
			userSet:     true,
			wantUser:    "root",
			wantPass:    "secret",
			wantKeypath: "/tmp/key",
			wantFound:   true,
		},
		{
			name:           "explicit keypath is preserved",
			cred:           "vm",
			currentKeypath: "/tmp/current-key",
			keyPathSet:     true,
			wantUser:       "admin",
			wantPass:       "secret",
			wantKeypath:    "/tmp/current-key",
			wantFound:      true,
		},
		{
			name:           "unknown credential leaves values unchanged",
			cred:           "missing",
			currentUser:    "root",
			currentPass:    "password",
			currentKeypath: "/tmp/current-key",
			wantUser:       "root",
			wantPass:       "password",
			wantKeypath:    "/tmp/current-key",
		},
	}

	for _, test := range tests {
		gotUser, gotPass, gotKeypath, gotFound := applyCredential(c, test.cred, test.currentUser, test.currentPass, test.currentKeypath, test.userSet, test.keyPathSet)
		if gotUser != test.wantUser {
			t.Errorf("%s: user = %q, want %q", test.name, gotUser, test.wantUser)
		}
		if gotPass != test.wantPass {
			t.Errorf("%s: pass = %q, want %q", test.name, gotPass, test.wantPass)
		}
		if gotKeypath != test.wantKeypath {
			t.Errorf("%s: keypath = %q, want %q", test.name, gotKeypath, test.wantKeypath)
		}
		if gotFound != test.wantFound {
			t.Errorf("%s: found = %v, want %v", test.name, gotFound, test.wantFound)
		}
	}
}

func TestNopassAlias(t *testing.T) {
	tests := []struct {
		target string
		want   string
	}{
		{target: "vm1", want: "vm1"},
		{target: "root@10.0.0.1", want: "10.0.0.1"},
		{target: "root@10.0.0.1:2222", want: "10.0.0.1"},
		{target: "10.0.0.1:2222", want: "10.0.0.1"},
	}

	for _, test := range tests {
		got := nopassAlias(test.target)
		if got != test.want {
			t.Errorf("nopassAlias(%q) = %q, want %q", test.target, got, test.want)
		}
	}
}

func TestSSHConfigFileName(t *testing.T) {
	tests := []struct {
		alias string
		want  string
	}{
		{alias: "10.0.0.1", want: "10.0.0.1"},
		{alias: "vm-1", want: "vm-1"},
		{alias: "root@10.0.0.1:2222", want: "root_10.0.0.1_2222"},
		{alias: "../bad", want: "bad"},
		{alias: "", want: "host"},
	}

	for _, test := range tests {
		got := sshConfigFileName(test.alias)
		if got != test.want {
			t.Errorf("sshConfigFileName(%q) = %q, want %q", test.alias, got, test.want)
		}
	}
}

func TestBuildAuthorizedKeysCommandQuotesPublicKey(t *testing.T) {
	key := "ssh-rsa AAAA comment'withquote"
	got := buildAuthorizedKeysCommand(key)
	if !strings.Contains(got, "mkdir -p ~/.ssh") {
		t.Fatalf("command = %q, want mkdir setup", got)
	}
	if !strings.Contains(got, "'ssh-rsa AAAA comment'\"'\"'withquote'") {
		t.Fatalf("command = %q, want shell-quoted key", got)
	}
}

func TestSetupNopassKeyPathDefaultsWhenEmpty(t *testing.T) {
	if got := setupNopassKeyPath(""); got != defaultKeyPath() {
		t.Fatalf("setupNopassKeyPath(empty) = %q, want %q", got, defaultKeyPath())
	}
	if got := setupNopassKeyPath("/tmp/id_rsa"); got != "/tmp/id_rsa" {
		t.Fatalf("setupNopassKeyPath(explicit) = %q, want /tmp/id_rsa", got)
	}
}

func TestUpsertSSHConfigEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	entry := sshConfigEntry{
		Alias:        "vm1",
		HostName:     "10.0.0.1",
		User:         "admin",
		Port:         "2222",
		IdentityFile: "/tmp/id_rsa",
	}

	if err := upsertSSHConfigEntry(path, entry, false); err != nil {
		t.Fatalf("upsert new entry got err %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "Host vm1\n  HostName 10.0.0.1\n  User admin\n  Port 2222\n  IdentityFile /tmp/id_rsa\n\n"
	if string(content) != want {
		t.Fatalf("config content = %q, want %q", string(content), want)
	}

	err = upsertSSHConfigEntry(path, entry, false)
	if err == nil {
		t.Fatal("upsert existing entry got nil err, want conflict")
	}
	if !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("upsert existing err = %v, want already exists", err)
	}

	entry.User = "root"
	if err := upsertSSHConfigEntry(path, entry, true); err != nil {
		t.Fatalf("force upsert got err %v", err)
	}
	content, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "  User root\n") {
		t.Fatalf("force content = %q, want replaced user", string(content))
	}
	if strings.Contains(string(content), "  User admin\n") {
		t.Fatalf("force content = %q, still contains old user", string(content))
	}
}

func TestUpsertSSHConfigEntryPreservesOtherBlocks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	initial := "Host old\n  HostName 10.0.0.2\n\nHost vm1\n  HostName old.example\n  User old\n\nMatch all\n  ForwardAgent no\n"
	if err := os.WriteFile(path, []byte(initial), 0600); err != nil {
		t.Fatal(err)
	}
	entry := sshConfigEntry{
		Alias:        "vm1",
		HostName:     "10.0.0.1",
		User:         "admin",
		Port:         "22",
		IdentityFile: "/tmp/id_rsa",
	}

	if err := upsertSSHConfigEntry(path, entry, true); err != nil {
		t.Fatalf("force upsert got err %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(content)
	if !strings.Contains(got, "Host old\n  HostName 10.0.0.2\n\n") {
		t.Fatalf("content = %q, lost old host block", got)
	}
	if !strings.Contains(got, "Match all\n  ForwardAgent no\n") {
		t.Fatalf("content = %q, lost match block", got)
	}
	if !strings.Contains(got, "Host vm1\n  HostName 10.0.0.1\n  User admin\n") {
		t.Fatalf("content = %q, did not replace vm1 block", got)
	}
}

func TestEnsureSSHConfigInclude(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ssh", "config")

	if err := ensureSSHConfigInclude(path, "config.d/*"); err != nil {
		t.Fatalf("ensure include got err %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "Include config.d/*\n\n" {
		t.Fatalf("new config = %q", string(content))
	}

	if err := ensureSSHConfigInclude(path, "config.d/*"); err != nil {
		t.Fatalf("second ensure include got err %v", err)
	}
	content, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(content), "Include config.d/*") != 1 {
		t.Fatalf("config = %q, want one include", string(content))
	}
}

func TestEnsureSSHConfigIncludePrependsExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".ssh", "config")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("Host *\n  ServerAliveInterval 60\n"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := ensureSSHConfigInclude(path, "config.d/*"); err != nil {
		t.Fatalf("ensure include got err %v", err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "Include config.d/*\n\nHost *\n  ServerAliveInterval 60\n"
	if string(content) != want {
		t.Fatalf("config = %q, want %q", string(content), want)
	}
}

func TestUpsertGeneratedSSHConfigEntry(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, ".ssh", "config")
	generatedPath := filepath.Join(dir, ".ssh", "config.d", "vm1")
	entry := sshConfigEntry{
		Alias:        "vm1",
		HostName:     "10.0.0.1",
		User:         "admin",
		Port:         "22",
		IdentityFile: "/tmp/id_rsa",
	}

	if err := upsertGeneratedSSHConfigEntry(mainPath, generatedPath, entry, false); err != nil {
		t.Fatalf("upsert generated got err %v", err)
	}
	mainContent, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(mainContent) != "Include config.d/*\n\n" {
		t.Fatalf("main config = %q", string(mainContent))
	}
	generatedContent, err := os.ReadFile(generatedPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(generatedContent), "Host vm1\n  HostName 10.0.0.1\n") {
		t.Fatalf("generated config = %q", string(generatedContent))
	}
}

func TestUpsertGeneratedSSHConfigEntryRefusesManualHost(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, ".ssh", "config")
	generatedPath := filepath.Join(dir, ".ssh", "config.d", "vm1")
	if err := os.MkdirAll(filepath.Dir(mainPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainPath, []byte("Host vm1\n  HostName old.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entry := sshConfigEntry{
		Alias:        "vm1",
		HostName:     "10.0.0.1",
		User:         "admin",
		Port:         "22",
		IdentityFile: "/tmp/id_rsa",
	}

	err := upsertGeneratedSSHConfigEntry(mainPath, generatedPath, entry, true)
	if err == nil {
		t.Fatal("upsert generated got nil err, want manual host conflict")
	}
	if !strings.Contains(err.Error(), "edit it manually") {
		t.Fatalf("err = %v, want manual edit message", err)
	}
}

func TestUpsertGeneratedSSHConfigEntryRefusesOtherIncludedHost(t *testing.T) {
	dir := t.TempDir()
	mainPath := filepath.Join(dir, ".ssh", "config")
	generatedPath := filepath.Join(dir, ".ssh", "config.d", "vm1")
	otherPath := filepath.Join(dir, ".ssh", "config.d", "manual")
	if err := os.MkdirAll(filepath.Dir(otherPath), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("Host vm1\n  HostName old.example\n"), 0600); err != nil {
		t.Fatal(err)
	}
	entry := sshConfigEntry{
		Alias:        "vm1",
		HostName:     "10.0.0.1",
		User:         "admin",
		Port:         "22",
		IdentityFile: "/tmp/id_rsa",
	}

	err := upsertGeneratedSSHConfigEntry(mainPath, generatedPath, entry, true)
	if err == nil {
		t.Fatal("upsert generated got nil err, want included host conflict")
	}
	if !strings.Contains(err.Error(), otherPath) {
		t.Fatalf("err = %v, want other included path", err)
	}
}

func TestResolveJumpHost(t *testing.T) {
	c := &config.Config{
		Creds: []struct {
			Name    string `yaml:"name"`
			User    string `yaml:"user"`
			Pass    string `yaml:"pass"`
			Keypath string `yaml:"keypath"`
		}{
			{
				Name:    "jump-cred",
				User:    "jumpuser",
				Pass:    "ignored",
				Keypath: "/tmp/jump_key",
			},
		},
		Hosts: []struct {
			Name     string `yaml:"name"`
			Host     string `yaml:"host"`
			Cred     string `yaml:"cred"`
			Port     string `yaml:"port"`
			Label    string `yaml:"label"`
			Jump     string `yaml:"jump"`
			InitCmds string `yaml:"initcmds"`
		}{
			{
				Name: "bastion",
				Host: "10.0.0.1",
				Cred: "jump-cred",
				Port: "2222",
			},
		},
	}

	tests := []struct {
		name        string
		expr        string
		wantHost    string
		wantUser    string
		wantPort    string
		wantKeyPath string
	}{
		{
			name:        "configured jump host",
			expr:        "bastion",
			wantHost:    "10.0.0.1",
			wantUser:    "jumpuser",
			wantPort:    "2222",
			wantKeyPath: "/tmp/jump_key",
		},
		{
			name:        "inline jump host",
			expr:        "admin@10.0.0.2:2200",
			wantHost:    "10.0.0.2",
			wantUser:    "admin",
			wantPort:    "2200",
			wantKeyPath: defaultKeyPath(),
		},
		{
			name:        "inline jump host defaults",
			expr:        "10.0.0.3",
			wantHost:    "10.0.0.3",
			wantUser:    "root",
			wantPort:    "22",
			wantKeyPath: defaultKeyPath(),
		},
	}

	for _, test := range tests {
		got, err := resolveJumpHost(c, test.expr)
		if err != nil {
			t.Errorf("resolveJumpHost(%q) got err %v", test.expr, err)
			continue
		}
		if got.Host != test.wantHost {
			t.Errorf("resolveJumpHost(%q) host = %q, want %q", test.expr, got.Host, test.wantHost)
		}
		if got.User != test.wantUser {
			t.Errorf("resolveJumpHost(%q) user = %q, want %q", test.expr, got.User, test.wantUser)
		}
		if got.Port != test.wantPort {
			t.Errorf("resolveJumpHost(%q) port = %q, want %q", test.expr, got.Port, test.wantPort)
		}
		if got.KeyPath != test.wantKeyPath {
			t.Errorf("resolveJumpHost(%q) keypath = %q, want %q", test.expr, got.KeyPath, test.wantKeyPath)
		}
	}
}

func TestGetHostReturnsJump(t *testing.T) {
	c := &config.Config{
		Hosts: []struct {
			Name     string `yaml:"name"`
			Host     string `yaml:"host"`
			Cred     string `yaml:"cred"`
			Port     string `yaml:"port"`
			Label    string `yaml:"label"`
			Jump     string `yaml:"jump"`
			InitCmds string `yaml:"initcmds"`
		}{
			{
				Name: "app",
				Host: "10.0.0.10",
				Cred: "app-cred",
				Port: "22",
				Jump: "bastion",
			},
		},
	}

	host, _, _, _, jump := c.GetHost("app")
	if host != "10.0.0.10" {
		t.Errorf("GetHost host = %q, want 10.0.0.10", host)
	}
	if jump != "bastion" {
		t.Errorf("GetHost jump = %q, want bastion", jump)
	}
}
