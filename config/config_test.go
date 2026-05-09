package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestParseConfig(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	configDir := filepath.Join(home, ".ssh")
	if err := os.MkdirAll(configDir, 0700); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "goto.yaml")
	if err := os.WriteFile(configPath, []byte(`creds:
- name: vm
  user: root
  pass: password
hosts:
- name: 11
  host: 10.47.120.11
  cred: vm
  port: 22
`), 0600); err != nil {
		t.Fatal(err)
	}

	c, err := ParseConfig()
	if err != nil {
		t.Error("parse err", err)
		return
	}
	if len(c.Creds) != 1 {
		t.Fatalf("creds len = %d, want 1", len(c.Creds))
	}
	pretty("c", c)
}

func TestParseSSHConfigHost(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config")
	if err := os.WriteFile(path, []byte(`
Host internal-*
  User admin
  Port 2222
  IdentityFile ~/.ssh/internal_id
  ProxyCommand ssh bastion -W %h:%p

Host internal-vm
  HostName 10.47.120.11

Host *
  User root
  Port 22
`), 0600); err != nil {
		t.Fatal(err)
	}

	got := parseSSHConfigHost(path, "internal-vm")
	if !got.Found {
		t.Fatal("parseSSHConfigHost did not find internal-vm")
	}
	if got.HostName != "10.47.120.11" {
		t.Errorf("HostName = %q, want 10.47.120.11", got.HostName)
	}
	if got.User != "admin" {
		t.Errorf("User = %q, want admin", got.User)
	}
	if got.Port != "2222" {
		t.Errorf("Port = %q, want 2222", got.Port)
	}
	if got.IdentityFile != "~/.ssh/internal_id" {
		t.Errorf("IdentityFile = %q, want ~/.ssh/internal_id", got.IdentityFile)
	}
	if got.ProxyCommand != "ssh bastion -W %h:%p" {
		t.Errorf("ProxyCommand = %q, want ssh bastion -W %%h:%%p", got.ProxyCommand)
	}
}

func pretty(prefix string, a interface{}) {
	b, _ := json.MarshalIndent(a, "", "  ")
	fmt.Printf("%v: %s\n", prefix, string(b))
}
