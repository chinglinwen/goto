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
