package live

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	"github.com/epheo/microshift-installer/internal/engine"
)

type blockdev struct {
	Name     string     `json:"name"`
	Size     uint64     `json:"size"`
	Rota     bool       `json:"rota"`
	Type     string     `json:"type"`
	Model    string     `json:"model"`
	Serial   string     `json:"serial"`
	RO       bool       `json:"ro"`
	FSType   string     `json:"fstype"`
	Label    string     `json:"label"`
	PTType   string     `json:"pttype"`
	Children []blockdev `json:"children"`
}

func lsblk(ctx context.Context, dev ...string) ([]blockdev, error) {
	args := append([]string{"-J", "-b", "-o", "NAME,SIZE,ROTA,TYPE,MODEL,SERIAL,RO,FSTYPE,LABEL,PTTYPE"}, dev...)
	out, err := exec.CommandContext(ctx, "lsblk", args...).Output()
	if err != nil {
		return nil, err
	}
	var v struct {
		Blockdevices []blockdev `json:"blockdevices"`
	}
	return v.Blockdevices, json.Unmarshal(out, &v)
}

func (s *System) disks(ctx context.Context) ([]engine.Disk, error) {
	devs, err := lsblk(ctx)
	if err != nil {
		return nil, err
	}
	skip := s.mediaDisks()
	ids := byID()
	var out []engine.Disk
	for _, d := range devs {
		if d.Type != "disk" || d.RO || d.Size == 0 || skip[d.Name] ||
			strings.HasPrefix(d.Name, "zram") || strings.HasPrefix(d.Name, "loop") {
			continue
		}
		path := ids[d.Name]
		if path == "" {
			path = "/dev/" + d.Name
		}
		disk := engine.Disk{
			Name: d.Name, Path: path, Model: strings.TrimSpace(d.Model), Serial: strings.TrimSpace(d.Serial),
			Size: d.Size, SSD: !d.Rota, Holds: holds(d),
		}
		disk.Health, disk.Failing = smart(ctx, d.Name)
		out = append(out, disk)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// mediaDisks are the disks behind the boot media and payload media.
func (s *System) mediaDisks() map[string]bool {
	skip := map[string]bool{}
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return skip
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		mnt := f[4]
		if mnt != liveRoot && !strings.HasPrefix(mnt, "/run/install/media") {
			continue
		}
		for i := 6; i+2 < len(f); i++ {
			if f[i] == "-" {
				skip[parentDisk(f[i+2])] = true
			}
		}
	}
	labels, _ := filepath.Glob("/dev/disk/by-label/MSPAYLOAD*")
	for _, l := range labels {
		if t, err := filepath.EvalSymlinks(l); err == nil {
			skip[parentDisk(t)] = true
		}
	}
	return skip
}

// parentDisk maps /dev/sdb1 to sdb through sysfs.
func parentDisk(dev string) string {
	name := filepath.Base(dev)
	if _, err := os.Stat(filepath.Join("/sys/block", name)); err == nil {
		return name
	}
	link, err := filepath.EvalSymlinks(filepath.Join("/sys/class/block", name))
	if err != nil {
		return name
	}
	return filepath.Base(filepath.Dir(link))
}

// byID picks one stable name per disk: wwn first, then the bus name.
func byID() map[string]string {
	links, _ := filepath.Glob("/dev/disk/by-id/*")
	rank := func(n string) int {
		for i, p := range []string{"wwn-", "nvme-eui", "nvme-", "ata-", "scsi-", "usb-"} {
			if strings.HasPrefix(n, p) {
				return i
			}
		}
		return 99
	}
	slices.SortFunc(links, func(a, b string) int {
		return rank(filepath.Base(a)) - rank(filepath.Base(b))
	})
	out := map[string]string{}
	for _, l := range links {
		base := filepath.Base(l)
		if strings.Contains(base, "-part") || strings.HasPrefix(base, "dm-") || strings.HasPrefix(base, "lvm-") {
			continue
		}
		t, err := filepath.EvalSymlinks(l)
		if err != nil {
			continue
		}
		if _, ok := out[filepath.Base(t)]; !ok {
			out[filepath.Base(t)] = l
		}
	}
	return out
}

// holds says in plain words what is on a disk, or "" when it looks blank.
func holds(d blockdev) string {
	var types []string
	var walk func(b blockdev)
	walk = func(b blockdev) {
		if b.FSType != "" {
			types = append(types, b.FSType)
		}
		for _, c := range b.Children {
			walk(c)
		}
	}
	walk(d)
	has := func(t string) bool { return slices.Contains(types, t) }
	switch {
	case has("linux_raid_member"):
		return "a RAID member"
	case has("LVM2_member"):
		return "LVM volumes"
	case has("zfs_member"):
		return "a ZFS pool"
	case has("vfat") && (has("xfs") || has("ext4") || has("btrfs")):
		return "an installed system"
	case len(types) == 1:
		return article(types[0]) + " " + types[0] + " filesystem"
	case len(types) > 1:
		return fmt.Sprintf("%d filesystems", len(types))
	case len(d.Children) > 0:
		return "empty partitions"
	}
	return ""
}

func article(w string) string {
	if strings.ContainsRune("aeiouxAEIOUX", rune(w[0])) {
		return "an"
	}
	return "a"
}

// smart reads the drive's own health verdict; virtual disks have none.
func smart(ctx context.Context, name string) (string, bool) {
	out, _ := exec.CommandContext(ctx, "smartctl", "-j", "-H", "-A", "/dev/"+name).Output()
	var v struct {
		Status *struct {
			Passed bool `json:"passed"`
		} `json:"smart_status"`
		ATA struct {
			Table []struct {
				ID  int `json:"id"`
				Raw struct {
					Value int64 `json:"value"`
				} `json:"raw"`
			} `json:"table"`
		} `json:"ata_smart_attributes"`
		NVMe *struct {
			Used   int   `json:"percentage_used"`
			Errors int64 `json:"media_errors"`
		} `json:"nvme_smart_health_information_log"`
	}
	if json.Unmarshal(out, &v) != nil || v.Status == nil {
		return "", false
	}
	if !v.Status.Passed {
		return "failing, says the disk itself", true
	}
	for _, a := range v.ATA.Table {
		// 5 reallocated, 197 pending, 198 uncorrectable sectors.
		if (a.ID == 5 || a.ID == 197 || a.ID == 198) && a.Raw.Value > 0 {
			return "early wear, still fine", false
		}
	}
	if v.NVMe != nil && v.NVMe.Used >= 80 {
		return fmt.Sprintf("worn, %d%% of its life used", v.NVMe.Used), false
	}
	return "", false
}
