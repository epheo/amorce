// Package site reads the file that answers the wizard for many machines: one
// file per site, never one per machine.
package site

import (
	"bytes"
	"fmt"
	"net"
	"strings"

	"go.yaml.in/yaml/v3"
)

// FileName is where the file sits on any install medium, and in the
// installed /etc/microshift-installer.
const FileName = "site.yaml"

type File struct {
	Site     string    `yaml:"site,omitempty"`
	Report   *Report   `yaml:"report,omitempty"`
	Defaults Answers   `yaml:"defaults,omitempty"`
	Machines []Machine `yaml:"machines"`
}

// Report names the first machine: it lists every install of the site, and
// workers join its cluster.
type Report struct {
	// Address is the first machine's address, with or without a port.
	Address string `yaml:"address"`
	// Token is the site's secret: it posts reports and joins workers, so
	// the file is kept like a password. It never crosses the network.
	Token string `yaml:"token"`
}

// Host is Address without its port.
func (r Report) Host() string {
	if h, _, err := net.SplitHostPort(r.Address); err == nil {
		return h
	}
	return r.Address
}

// Names lists the machines the file names, for the first machine's board.
func (f *File) Names() []string {
	var out []string
	for _, m := range f.Machines {
		if m.Name != "" {
			out = append(out, m.Name)
		}
	}
	return out
}

type Machine struct {
	Match   Match `yaml:"match"`
	Answers `yaml:",inline"`
}

// Match names one machine by its serial number or any of its MACs.
type Match struct {
	Serial string `yaml:"serial,omitempty"`
	MAC    string `yaml:"mac,omitempty"`
}

// Answers are the wizard's screens; an empty field is asked on screen.
type Answers struct {
	Name     string `yaml:"name,omitempty"`
	Purpose  string `yaml:"purpose,omitempty"`
	Keyboard string `yaml:"keyboard,omitempty"`
	// Address is "dhcp" or a fixed address with its prefix length.
	Address string   `yaml:"address,omitempty"`
	Network *Network `yaml:"network,omitempty"`
	Disks   *Disks   `yaml:"disks,omitempty"`
	// SSHKeys are installed for root; cloud-init's name, since admins know it.
	SSHKeys []string `yaml:"ssh_authorized_keys,omitempty"`
	// Profile is the distro's: controller, the default, runs its own
	// cluster; worker joins the first machine's.
	Profile string `yaml:"profile,omitempty"`
}

// Worker is the profile of a machine that joins the first machine's cluster.
const Worker = "worker"

type Network struct {
	Interface string   `yaml:"interface,omitempty"`
	VLAN      int      `yaml:"vlan,omitempty"`
	Gateway   string   `yaml:"gateway,omitempty"`
	DNS       []string `yaml:"dns,omitempty"`
}

type Disks struct {
	// System is "auto", or a disk's kernel name, serial or /dev/disk/by-id name.
	System string `yaml:"system,omitempty"`
	// Data is "auto", the purpose's plan, or "none".
	Data string `yaml:"data,omitempty"`
	// Erase is "blank", the default, or "any": unattended installs stop at
	// Confirm before erasing a disk that holds something, unless "any".
	Erase string `yaml:"erase,omitempty"`
}

// Parse reads a site file; unknown fields are errors, so typos never pass
// as unanswered questions.
func Parse(b []byte) (*File, error) {
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(b))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("site file: %w", err)
	}
	if r := f.Report; r != nil && (r.Address == "" || r.Token == "") {
		return nil, fmt.Errorf("site file: report needs an address and a token")
	}
	for i, m := range f.Machines {
		if m.Match.Serial == "" && m.Match.MAC == "" {
			return nil, fmt.Errorf("site file: machine %d has no match: give a serial or a mac", i+1)
		}
		if m.Match.MAC != "" {
			if _, err := net.ParseMAC(m.Match.MAC); err != nil {
				return nil, fmt.Errorf("site file: machine %d: %w", i+1, err)
			}
		}
	}
	for _, a := range append([]Answers{f.Defaults}, machineAnswers(f.Machines)...) {
		if a.Disks == nil {
			continue
		}
		if d := a.Disks.Data; d != "" && d != "auto" && d != "none" {
			return nil, fmt.Errorf("site file: disks.data is auto or none, not %q", d)
		}
		if e := a.Disks.Erase; e != "" && e != "blank" && e != "any" {
			return nil, fmt.Errorf("site file: disks.erase is blank or any, not %q", e)
		}
	}
	for i, a := range append([]Answers{f.Defaults}, machineAnswers(f.Machines)...) {
		switch a.Profile {
		case "", "controller":
		case Worker:
			if f.Report == nil {
				return nil, fmt.Errorf("site file: a worker joins the machine report names; add report")
			}
		default:
			return nil, fmt.Errorf("site file: profile is controller or worker, not %q", a.Profile)
		}
		if i == 0 {
			continue
		}
		m := merge(f.Defaults, a)
		if r := f.Report; r != nil && m.Profile == Worker && (m.Address == r.Host() || strings.HasPrefix(m.Address, r.Host()+"/")) {
			return nil, fmt.Errorf("site file: %s is the first machine, which workers join: it cannot be one", r.Host())
		}
	}
	for _, a := range append([]Answers{f.Defaults}, machineAnswers(f.Machines)...) {
		for _, k := range a.SSHKeys {
			if len(strings.Fields(k)) < 2 || strings.ContainsAny(k, "\n\r") {
				return nil, fmt.Errorf("site file: %q is not one OpenSSH public key line", k)
			}
		}
	}
	return &f, nil
}

func machineAnswers(ms []Machine) []Answers {
	var out []Answers
	for _, m := range ms {
		out = append(out, m.Answers)
	}
	return out
}

// Identity is what a machine can be matched by.
type Identity struct {
	Serial string
	MACs   []string
}

// Find returns the answers for id, defaults filled in, and whether the file
// names this machine.
func (f *File) Find(id Identity) (Answers, bool) {
	for _, m := range f.Machines {
		if m.Match.matches(id) {
			return merge(f.Defaults, m.Answers), true
		}
	}
	return Answers{}, false
}

// Matches says whether m names the machine id.
func (m Machine) Matches(id Identity) bool { return m.Match.matches(id) }

func (m Match) matches(id Identity) bool {
	if m.Serial != "" && strings.EqualFold(strings.TrimSpace(m.Serial), strings.TrimSpace(id.Serial)) {
		return true
	}
	if m.MAC == "" {
		return false
	}
	want, _ := net.ParseMAC(m.MAC)
	for _, s := range id.MACs {
		if got, err := net.ParseMAC(s); err == nil && bytes.Equal(got, want) {
			return true
		}
	}
	return false
}

// merge fills every answer m leaves empty from d.
func merge(d, m Answers) Answers {
	out := m
	or := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	out.Name = or(m.Name, d.Name)
	out.Purpose = or(m.Purpose, d.Purpose)
	out.Keyboard = or(m.Keyboard, d.Keyboard)
	out.Address = or(m.Address, d.Address)
	out.Profile = or(m.Profile, d.Profile)
	if out.SSHKeys == nil {
		out.SSHKeys = d.SSHKeys
	}
	if d.Network != nil || m.Network != nil {
		var dn, mn Network
		if d.Network != nil {
			dn = *d.Network
		}
		if m.Network != nil {
			mn = *m.Network
		}
		n := mn
		n.Interface = or(mn.Interface, dn.Interface)
		if n.VLAN == 0 {
			n.VLAN = dn.VLAN
		}
		n.Gateway = or(mn.Gateway, dn.Gateway)
		if n.DNS == nil {
			n.DNS = dn.DNS
		}
		out.Network = &n
	}
	if d.Disks != nil || m.Disks != nil {
		var dd, md Disks
		if d.Disks != nil {
			dd = *d.Disks
		}
		if m.Disks != nil {
			md = *m.Disks
		}
		out.Disks = &Disks{System: or(md.System, dd.System), Data: or(md.Data, dd.Data), Erase: or(md.Erase, dd.Erase)}
	}
	return out
}

// Marshal writes f with a header that says how to reuse it.
func Marshal(f *File) ([]byte, error) {
	var b bytes.Buffer
	b.WriteString("# Written by the installer. Add machines, then make media with:\n")
	b.WriteString("#   installer iso --site site.yaml --image ...\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(f); err != nil {
		return nil, err
	}
	return b.Bytes(), enc.Close()
}
