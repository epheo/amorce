package live

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/epheo/amorce/internal/engine"
	"github.com/epheo/amorce/internal/media"
	"github.com/epheo/amorce/internal/site"
)

// liveRoot is where dmsquash-live mounts the boot media.
const liveRoot = "/run/initramfs/live"

// mediaRoots finds every medium with images: the boot media, and any other
// medium labeled MSPAYLOAD*, which keeps images apart from the installer.
func (s *System) mediaRoots(ctx context.Context) []string {
	roots := []string{liveRoot}
	labels, _ := filepath.Glob("/dev/disk/by-label/MSPAYLOAD*")
	for _, l := range labels {
		mnt := filepath.Join("/run/install/media", filepath.Base(l))
		if _, err := os.Stat(filepath.Join(mnt, media.IndexPath)); err != nil {
			os.MkdirAll(mnt, 0o755)
			if _, err := s.cmd(ctx, "mount", "-o", "ro", l, mnt); err != nil {
				continue
			}
		}
		roots = append(roots, mnt)
	}
	return roots
}

func (s *System) Purposes(ctx context.Context) ([]engine.Purpose, error) {
	var out []engine.Purpose
	seen := map[string]bool{}
	for _, root := range s.mediaRoots(ctx) {
		ix, err := media.Read(root)
		if err != nil {
			continue
		}
		for _, im := range ix.Images {
			if seen[im.ID] {
				continue
			}
			seen[im.ID] = true
			out = append(out, engine.Purpose{
				ID: im.ID, Name: im.Name, Summary: im.Summary, Includes: im.Includes,
				DataDisks: im.DataDisks, Target: im.Target, Bytes: im.Bytes, NeedsSecureBootOff: im.SecureBoot == "off",
				Names: im.Names, Identity: im.Identity, IdentityName: im.IdentityName, Settings: im.Settings,
				ReservedUsers: im.ReservedUsers,
				Source:        fmt.Sprintf("oci:%s:%s", filepath.Join(root, "images", im.Layout), im.Tag),
			})
		}
	}
	return out, nil
}

// Site returns the first site file on the media, or nil.
func (s *System) Site(ctx context.Context) ([]byte, error) {
	for _, root := range s.mediaRoots(ctx) {
		b, err := os.ReadFile(filepath.Join(root, site.FileName))
		if err == nil {
			s.logf("site file from %s", root)
			return b, nil
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return nil, nil
}

// sourceDevice is the block device a purpose's images are read from.
func sourceDevice(source string) string {
	path := ociPath(source)
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return ""
	}
	best, dev := "", ""
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 10 {
			continue
		}
		mnt := f[4]
		sep := 6
		for sep < len(f) && f[sep] != "-" {
			sep++
		}
		if sep+2 >= len(f) {
			continue
		}
		if strings.HasPrefix(path, mnt) && len(mnt) > len(best) {
			best, dev = mnt, f[sep+2]
		}
	}
	return dev
}
