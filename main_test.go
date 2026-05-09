package main

import (
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
