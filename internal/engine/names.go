package engine

import (
	"context"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	labelRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)
	// Forgejo refuses some names with dots, underscores or dashes.
	adminRE = regexp.MustCompile(`^[a-z][a-z0-9]{1,31}$`)
)

// dnsName says what is wrong with a lowercase DNS name of two labels or
// more, or returns "".
func dnsName(d string) string {
	labels := strings.Split(d, ".")
	if len(labels) < 2 {
		return "Two parts or more, like virt.example.com."
	}
	for _, l := range labels {
		if !labelRE.MatchString(l) {
			return "Use a to z, 0 to 9 and inner dashes between the dots."
		}
	}
	if strings.Trim(labels[len(labels)-1], "0123456789") == "" {
		return "A domain ends with a name, not a number."
	}
	return ""
}

// CheckDomain explains what is wrong with domain for an image serving
// names under it, or returns "". Empty is an answer: by address.
func CheckDomain(domain string, names []string) string {
	switch {
	case domain == "":
		return ""
	case strings.HasPrefix(domain, "*."):
		return "Give the domain without *., like virt.example.com."
	case strings.HasSuffix(domain, "."):
		return "Give the domain without the final dot."
	case strings.ToLower(domain) != domain:
		return "Lowercase only."
	}
	for _, r := range domain {
		if r > unicode.MaxASCII {
			return "ASCII only: give an international name in its xn-- form."
		}
	}
	if _, err := netip.ParseAddr(domain); err == nil {
		return "That is an address: leave the domain empty to use it."
	}
	if msg := dnsName(domain); msg != "" {
		return msg
	}
	for _, n := range names {
		if len(n)+1+len(domain) > 253 {
			return "Too long for " + n + "." + domain + "."
		}
	}
	return ""
}

// CheckAdminUser explains what is wrong with the first admin's name, or
// returns "".
func CheckAdminUser(user string, reserved []string) string {
	switch {
	case user == "":
		return "The first admin needs a name."
	case !adminRE.MatchString(user):
		return "2 to 32 of a to z and 0 to 9, a letter first."
	case slices.Contains(reserved, user):
		return user + " is taken by the system: pick another."
	}
	return ""
}

// CheckDisplay explains what is wrong with a display name, or returns "".
// Quotes, backslashes and dollars would read differently in a shell or a
// systemd unit.
func CheckDisplay(s string) string {
	if utf8.RuneCountInString(s) > 64 {
		return "64 characters at most."
	}
	for _, r := range s {
		if !unicode.IsPrint(r) || strings.ContainsRune("\"'\\$`", r) {
			return "No quotes, \\, $ or ` in a name."
		}
	}
	return ""
}

// CheckEmail explains what is wrong with an email address, or returns "".
func CheckEmail(s string) string {
	local, domain, ok := strings.Cut(s, "@")
	switch {
	case s == "":
		return "The first admin needs an email."
	case !ok || local == "" || strings.Contains(domain, "@") || len(s) > 254 || len(local) > 64:
		return "An email looks like alice@example.com."
	}
	for _, r := range local {
		if r <= ' ' || r > unicode.MaxASCII || strings.ContainsRune("\"'\\$`<>()[],;:", r) {
			return "An email looks like alice@example.com, ASCII only."
		}
	}
	if dnsName(strings.ToLower(domain)) != "" {
		return "The email's domain is not a domain."
	}
	return ""
}

// CheckNTP explains what is wrong with a list of time servers, or returns "".
func CheckNTP(servers []string) string {
	for _, s := range servers {
		if _, err := netip.ParseAddr(s); err == nil {
			continue
		}
		if dnsName(s) != "" && !labelRE.MatchString(s) {
			return "NTP " + s + " is not an address or a name."
		}
	}
	return ""
}

// ProviderName is how the screens name an identity provider.
func ProviderName(id string) string {
	if id == "" {
		return ""
	}
	return strings.ToUpper(id[:1]) + id[1:]
}

// CheckNames explains what is wrong with n for purpose p, or returns "".
func CheckNames(p Purpose, n Names) string {
	if !p.OffersNames() {
		if n.Domain != "" || n.Identity != "" {
			return p.Name + " serves no names: leave domain and identity out."
		}
		return ""
	}
	if msg := CheckDomain(n.Domain, append(slices.Clone(p.Names), p.IdentityName)); msg != "" {
		return msg
	}
	if n.Identity == "" {
		if n.Admin != (Admin{}) {
			return "A first admin is for an identity provider only."
		}
		return ""
	}
	switch {
	case n.Identity != p.Identity:
		return p.Name + " offers no " + ProviderName(n.Identity) + "."
	case n.Domain == "":
		return ProviderName(n.Identity) + " needs a domain."
	}
	if msg := CheckAdminUser(n.Admin.User, p.ReservedUsers); msg != "" {
		return msg
	}
	if msg := CheckDisplay(n.Admin.Display); msg != "" {
		return msg
	}
	return CheckEmail(n.Admin.Email)
}

// SettingsFile is the purpose's settings file: KEY=value lines, read
// verbatim, so no comments.
func SettingsFile(n Names) string {
	id := n.Identity
	if id == "" {
		id = "none"
	}
	b := "DOMAIN=" + n.Domain + "\nIDENTITY=" + id + "\n"
	if n.Identity != "" {
		b += "ADMIN_USER=" + n.Admin.User + "\n"
		if n.Admin.Display != "" {
			b += "ADMIN_DISPLAY=" + n.Admin.Display + "\n"
		}
		b += "ADMIN_EMAIL=" + n.Admin.Email + "\n"
	}
	return b
}

// CheckedNames are the names DNS must point at the machine for n.
func CheckedNames(p Purpose, n Names) []string {
	if n.Domain == "" {
		return nil
	}
	var out []string
	for _, name := range p.Names {
		out = append(out, name+"."+n.Domain)
	}
	if n.Identity != "" && p.IdentityName != "" {
		out = append(out, p.IdentityName+"."+n.Domain)
	}
	return out
}

// SetNames keeps the names and sign-in, and asks DNS about the names.
func (e *Engine) SetNames(ctx context.Context, n Names) string {
	e.mu.Lock()
	p, _ := e.st.Purpose()
	e.mu.Unlock()
	if msg := CheckNames(p, n); msg != "" {
		return msg
	}
	e.update(func(s *State) { s.Names = n })
	e.CheckDNS(ctx, n)
	return ""
}

// CheckDNS resolves the names n serves in the background, once per set of
// names; NameChecksFor says which set the checks answer.
func (e *Engine) CheckDNS(ctx context.Context, n Names) {
	e.mu.Lock()
	p, _ := e.st.Purpose()
	names := CheckedNames(p, n)
	key := strings.Join(names, " ")
	same := key == e.st.NameChecksFor
	e.mu.Unlock()
	if same {
		return
	}
	e.update(func(s *State) { s.NameChecks, s.NameChecksFor, s.NamesChecking = nil, key, key != "" })
	if key == "" {
		return
	}
	go func() {
		var out []NameCheck
		for _, name := range names {
			c := NameCheck{Name: name}
			addrs, err := e.sys.Resolve(ctx, name)
			if err != nil {
				c.Err = err.Error()
			}
			c.Addrs = addrs
			out = append(out, c)
		}
		e.update(func(s *State) {
			if s.NameChecksFor != key {
				return
			}
			s.NameChecks, s.NamesChecking = out, false
		})
	}()
}

// Points says whether DNS sends name to the machine's address.
func (c NameCheck) Points(addr netip.Addr) bool {
	return c.Err == "" && len(c.Addrs) == 1 && c.Addrs[0] == addr
}
