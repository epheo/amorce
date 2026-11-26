// Package engine owns the install: what was found, what was chosen, and the
// install itself. Frontends only read snapshots and call its methods.
package engine

import (
	"context"
	"net/netip"
	"strings"
	"time"

	"github.com/epheo/amorce/internal/report"
)

// Step is the wizard screen every frontend shows.
type Step int

const (
	StepHello Step = iota
	StepNetwork
	StepPurpose
	StepDisks
	StepName
	StepConfirm
	StepInstalling
	StepReady
)

// WizardSteps counts the screens before the install starts.
const WizardSteps = int(StepConfirm) + 1

type Machine struct {
	// Serial is the firmware's serial number, what site files match first.
	Serial      string
	CPUs        int
	MemoryBytes uint64
	UEFI        bool
	SecureBoot  bool
	Disks       []Disk
	NICs        []NIC
}

type Disk struct {
	Name   string // kernel name, e.g. sda
	Path   string // stable /dev/disk/by-id path
	Model  string
	Serial string
	Size   uint64
	SSD    bool
	// Holds says what is on the disk now; empty when it looks blank.
	Holds string
	// Health is empty when SMART has nothing to say.
	Health  string
	Failing bool
	// Controller names the hardware RAID controller when the disk is one of
	// its volumes; the drives behind it are out of sight.
	Controller string
}

// Stable says whether Path names this disk at every start: a kernel name
// can point at another disk on the installed system.
func (d Disk) Stable() bool { return strings.HasPrefix(d.Path, "/dev/disk/by-id/") }

type NIC struct {
	Name      string
	MAC       string
	Carrier   bool
	SpeedMbps int
	Neighbor  *Neighbor
}

// Neighbor is what the switch port announces over LLDP.
type Neighbor struct {
	Switch string
	Port   string
	VLAN   int // port VLAN, 0 when not announced
	Seen   time.Time
}

// Purpose is one installable image, described by labels on that image.
type Purpose struct {
	ID       string
	Name     string
	Summary  string
	Includes []string
	// Source is the bootc source image reference, e.g. oci:/run/media/images/x:latest.
	Source string
	// Target is where updates come from after install.
	Target string
	// DataDisks is how the purpose uses disks beyond the system disk: none,
	// DataPairs or DataThin.
	DataDisks string
	// Bytes is the image's compressed size, for install progress.
	Bytes int64
	// NeedsSecureBootOff is set when the image cannot run with Secure Boot on.
	NeedsSecureBootOff bool
}

type NetMode int

const (
	NetDHCP NetMode = iota
	NetStatic
)

type NetConfig struct {
	Mode    NetMode
	Iface   string
	VLAN    int // 0 is untagged
	Address netip.Prefix
	Gateway netip.Addr
	DNS     []netip.Addr
}

// Check is one live test result.
type Check int

const (
	CheckUnknown Check = iota
	CheckOK
	CheckFailed
)

type NetReport struct {
	Config NetConfig // what is in effect, with the DHCP lease filled in
	Up     bool      // an address is configured
	// Conflict is set when another machine answers for our address.
	Conflict string
	Gateway  Check
	DNS      Check
	// GatewayVLANs lists where the gateway answers instead; 0 is untagged.
	GatewayVLANs []int
	At           time.Time
}

type DiskPlan struct {
	System string // disk name
	Pools  []Pool
	Unused []string
}

type Pool struct {
	Name  string
	Raid  string // raid1 or none
	Disks []string
}

type StepStatus int

const (
	Pending StepStatus = iota
	Running
	Done
	Skipped
	Failed
)

type InstallStep struct {
	Title  string
	Status StepStatus
	Detail string
}

type InstallState struct {
	Steps    []InstallStep
	Progress float64 // whole install, 0 to 1
	Started  time.Time
	Log      []string
	Finished bool
	Err      string
	// MOKPassword is set when the next start asks to enroll our key.
	MOKPassword string
}

// Cluster is a first machine heard on the network: workers join its
// cluster.
type Cluster struct {
	Site    string
	Name    string
	Address string
	Purpose string
	// Open is set while its screen shows join words.
	Open bool
	Seen time.Time
}

// Admission is the first machine's answer to the right words.
type Admission struct {
	// Site is its site file, which holds the site's token.
	Site []byte
	// Purpose is what it runs; its workers run the same.
	Purpose string
	// Names are the machines it knows, so a new one gets a free name.
	Names []string
}

// InstallRequest is everything the install needs; nothing else is read.
type InstallRequest struct {
	Machine  Machine
	Purpose  Purpose
	Plan     DiskPlan
	Net      NetConfig
	Hostname string
	Keyboard string
	// Site is this machine's answers as a site file, kept on the installed
	// system so more machines can be installed the same way.
	Site []byte
	// SSHKeys are installed for root.
	SSHKeys []string
	// Profile is "worker" for a machine that joins the first machine's
	// cluster at its first start.
	Profile string
}

// InstallEvent reports install progress from the system to the engine.
type InstallEvent struct {
	Step     int // index into InstallSteps
	Status   StepStatus
	Detail   string
	Progress float64 // within the step, 0 to 1
	Log      string
	MOK      string
}

// System is every side effect: the live media in production, a fake in demos.
type System interface {
	Discover(ctx context.Context) (Machine, error)
	Purposes(ctx context.Context) ([]Purpose, error)
	Keymaps() []string
	SetKeymap(name string) error
	// Network returns the address the live system has now, if any.
	Network(ctx context.Context) NetReport
	// TestNetwork applies cfg to the live system and tests it.
	TestNetwork(ctx context.Context, cfg NetConfig) NetReport
	// Neighbors reports switch ports heard over LLDP until ctx ends.
	Neighbors(ctx context.Context, found func(nic string, n Neighbor))
	Install(ctx context.Context, req InstallRequest, ev func(InstallEvent)) error
	Reboot() error
	// Site returns the site file found on the media, or nil.
	Site(ctx context.Context) ([]byte, error)
	// Clusters reports first machines heard on the network until ctx ends.
	Clusters(ctx context.Context, found func(Cluster))
	// Admit asks the first machine at address to admit this one; the words
	// are the ones on its screen.
	Admit(ctx context.Context, address string, words []string, me report.Report) (Admission, error)
}

// InstallSteps are the install's visible steps, in order.
var InstallSteps = []string{
	"Check the hardware",
	"Wipe the chosen disks",
	"Copy the system image",
	"Apply network settings",
	"Hand data disks over",
	"Add the Secure Boot key",
}

// Install step indexes, for System implementations.
const (
	InstCheck = iota
	InstWipe
	InstCopy
	InstNetwork
	InstData
	InstSecureBoot
)

// stepWeights spread the progress bar; copying the image dominates.
var stepWeights = []float64{0.03, 0.05, 0.80, 0.04, 0.04, 0.04}
