package live

import (
	"context"
	"net"
	"net/netip"
	"os/exec"
	"strings"
	"time"
)

// Resolve asks the resolver the live network was given, as the installed
// system will.
func (s *System) Resolve(ctx context.Context, name string) ([]netip.Addr, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(ctx, "ip4", name)
	if err != nil {
		if de, ok := err.(*net.DNSError); ok && de.IsNotFound {
			err = errNotFound
		}
		s.logf("dns %s: %v", name, err)
		return nil, err
	}
	for i, a := range addrs {
		addrs[i] = a.Unmap()
	}
	s.logf("dns %s: %v", name, addrs)
	return addrs, nil
}

type notFound struct{}

func (notFound) Error() string { return "not found" }

var errNotFound error = notFound{}

// ntpAnswers asks server for the time once, without setting the clock.
func ntpAnswers(ctx context.Context, server string) bool {
	return exec.CommandContext(ctx, "chronyd", "-Q", "-t", "4", "server "+server+" iburst maxsamples 1").Run() == nil
}

// NTPDir holds the installer's time servers; chrony reads it as a sourcedir.
const NTPDir = "etc/amorce/chrony"

// NTPSources lists the time servers in chrony's sources format.
func NTPSources(servers []string) string {
	b := "# Written by the installer: the site's time servers.\n"
	for _, s := range servers {
		b += "server " + s + " iburst\n"
	}
	return b
}

// ChronyConf points chrony at the installer's servers in place of the
// image's own, which an air-gapped site cannot reach; without servers it
// gives the image's back.
func ChronyConf(conf string, servers bool) string {
	dir := "sourcedir /" + NTPDir
	mark := "  # replaced by " + dir
	var out []string
	for _, line := range strings.Split(strings.TrimRight(conf, "\n"), "\n") {
		f := strings.Fields(line)
		switch {
		case strings.TrimSpace(line) == dir || line == chronyNote:
			continue
		case servers && len(f) > 0 && (f[0] == "pool" || f[0] == "server"):
			line = "#" + line + mark
		case !servers && strings.HasSuffix(line, mark):
			line = strings.TrimSuffix(strings.TrimPrefix(line, "#"), mark)
		}
		out = append(out, line)
	}
	if servers {
		out = append(out, chronyNote, dir)
	}
	return strings.Join(out, "\n") + "\n"
}

const chronyNote = "# The site's time servers, from the installer."
