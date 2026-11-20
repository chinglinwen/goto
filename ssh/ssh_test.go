package ssh

import "testing"

func TestExpandProxyCommand(t *testing.T) {
	term := New(
		"10.47.120.11",
		"root",
		"",
		SetPort("2222"),
		SetProxyCommand("ssh bastion -W %h:%p -l %r"),
	)

	got := term.expandProxyCommand()
	want := "ssh bastion -W 10.47.120.11:2222 -l root"
	if got != want {
		t.Fatalf("expandProxyCommand() = %q, want %q", got, want)
	}
}

func TestParseRemoteForward(t *testing.T) {
	tests := []struct {
		spec       string
		wantListen string
		wantDial   string
		wantErr    bool
	}{
		{spec: "40000:127.0.0.1:40000", wantListen: "localhost:40000", wantDial: "127.0.0.1:40000"},
		{spec: "0.0.0.0:8080:10.0.0.5:80", wantListen: "0.0.0.0:8080", wantDial: "10.0.0.5:80"},
		{spec: "40000:localhost:40000", wantListen: "localhost:40000", wantDial: "localhost:40000"},
		{spec: "40000:127.0.0.1", wantErr: true},
		{spec: "40000:127.0.0.1:40000:extra:more", wantErr: true},
		{spec: "abc:127.0.0.1:40000", wantErr: true},
		{spec: "40000:127.0.0.1:70000", wantErr: true},
		{spec: ":40000:127.0.0.1:40000", wantErr: true},
		{spec: "40000::40000", wantErr: true},
	}
	for _, tt := range tests {
		listenAddr, dialAddr, err := ParseRemoteForward(tt.spec)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseRemoteForward(%q) err = nil, want error", tt.spec)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseRemoteForward(%q) unexpected err: %v", tt.spec, err)
			continue
		}
		if listenAddr != tt.wantListen || dialAddr != tt.wantDial {
			t.Errorf("ParseRemoteForward(%q) = %q, %q; want %q, %q", tt.spec, listenAddr, dialAddr, tt.wantListen, tt.wantDial)
		}
	}
}
