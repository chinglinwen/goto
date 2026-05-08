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

func pretty(prefix string, a interface{}) {
	b, _ := json.MarshalIndent(a, "", "  ")
	fmt.Printf("%v: %s\n", prefix, string(b))
}
