package main

import "testing"

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
