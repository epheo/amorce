package engine

import (
	"strings"
	"testing"
)

var virt = Purpose{ID: "virtualization", Name: "Virtualization", Names: []string{"console", "git"},
	Identity: "kanidm", IdentityName: "idm", Settings: "/etc/dotvirt/appliance.env", ReservedUsers: []string{"admin"}}

func TestCheckDomain(t *testing.T) {
	for d, ok := range map[string]bool{
		"": true, "virt.example.com": true, "a.b": true, "xn--bcher-kva.example": true,
		"*.virt.example.com": false, "virt.example.com.": false, "Virt.example.com": false,
		"localhost": false, "10.0.0.1": false, "a.123": false, "bücher.example": false,
		"-a.example.com": false, "a_b.example.com": false,
		strings.Repeat("a.", 124) + "com": false,
	} {
		if got := CheckDomain(d, virt.Names) == ""; got != ok {
			t.Errorf("CheckDomain(%q) ok = %v, want %v: %s", d, got, ok, CheckDomain(d, virt.Names))
		}
	}
}

func TestCheckAdmin(t *testing.T) {
	for u, ok := range map[string]bool{"alice": true, "a1": true, "a": false, "1a": false, "al.ice": false,
		"al_ice": false, "al-ice": false, "Alice": false, "admin": false, strings.Repeat("a", 33): false} {
		if got := CheckAdminUser(u, virt.ReservedUsers) == ""; got != ok {
			t.Errorf("CheckAdminUser(%q) ok = %v, want %v", u, got, ok)
		}
	}
	for d, ok := range map[string]bool{"": true, "Alice Martin": true, "Zoë Ångström": true, "O'Brien": false,
		"a$b": false, "a`b": false, "a\\b": false, "a\nb": false, strings.Repeat("é", 65): false} {
		if got := CheckDisplay(d) == ""; got != ok {
			t.Errorf("CheckDisplay(%q) ok = %v, want %v", d, got, ok)
		}
	}
	for e, ok := range map[string]bool{"alice@example.com": true, "a.b+c@lab.example.org": true, "": false,
		"alice": false, "a@b": false, "a@@b.com": false, "a b@c.com": false, "zoë@example.com": false, "a'b@c.com": false} {
		if got := CheckEmail(e) == ""; got != ok {
			t.Errorf("CheckEmail(%q) ok = %v, want %v", e, got, ok)
		}
	}
}

func TestCheckNTP(t *testing.T) {
	for _, tc := range []struct {
		in []string
		ok bool
	}{{nil, true}, {[]string{"10.20.0.53", "ntp.lab", "ntp1"}, true}, {[]string{"ntp lab"}, false}, {[]string{"-x"}, false}} {
		if got := CheckNTP(tc.in) == ""; got != tc.ok {
			t.Errorf("CheckNTP(%q) ok = %v, want %v", tc.in, got, tc.ok)
		}
	}
}

func TestCheckNames(t *testing.T) {
	alice := Admin{User: "alice", Display: "Alice Martin", Email: "alice@example.com"}
	for _, tc := range []struct {
		p    Purpose
		n    Names
		want string // a part of the message; empty means accepted
	}{
		{virt, Names{}, ""},
		{virt, Names{Domain: "virt.example.com"}, ""},
		{virt, Names{Domain: "virt.example.com", Identity: "kanidm", Admin: alice}, ""},
		{virt, Names{Identity: "kanidm", Admin: alice}, "needs a domain"},
		{virt, Names{Domain: "virt.example.com", Identity: "kanidm"}, "needs a name"},
		{virt, Names{Domain: "virt.example.com", Identity: "kanidm", Admin: Admin{User: "alice"}}, "email"},
		{virt, Names{Domain: "virt.example.com", Identity: "keycloak", Admin: alice}, "offers no Keycloak"},
		{virt, Names{Domain: "virt.example.com", Admin: alice}, "identity provider only"},
		{Purpose{Name: "Storage"}, Names{Domain: "virt.example.com"}, "serves no names"},
		{Purpose{Name: "Storage"}, Names{}, ""},
	} {
		got := CheckNames(tc.p, tc.n)
		if (tc.want == "") != (got == "") || !strings.Contains(got, tc.want) {
			t.Errorf("CheckNames(%s, %+v) = %q, want %q", tc.p.Name, tc.n, got, tc.want)
		}
	}
}

// The image parses the file by splitting at the first =, so it carries no
// comments, quotes or keys it does not know.
func TestSettingsFile(t *testing.T) {
	if got := SettingsFile(Names{}); got != "DOMAIN=\nIDENTITY=none\n" {
		t.Errorf("by address:\n%s", got)
	}
	got := SettingsFile(Names{Domain: "virt.example.com", Identity: "kanidm",
		Admin: Admin{User: "alice", Display: "Alice Martin", Email: "alice@example.com"}})
	want := "DOMAIN=virt.example.com\nIDENTITY=kanidm\nADMIN_USER=alice\nADMIN_DISPLAY=Alice Martin\nADMIN_EMAIL=alice@example.com\n"
	if got != want {
		t.Errorf("with kanidm:\n%s", got)
	}
	if got := CheckedNames(virt, Names{Domain: "d.example", Identity: "kanidm"}); strings.Join(got, " ") != "console.d.example git.d.example idm.d.example" {
		t.Errorf("checked %v", got)
	}
	if got := CheckedNames(virt, Names{Domain: "d.example"}); len(got) != 2 {
		t.Errorf("without kanidm, checked %v", got)
	}
}
