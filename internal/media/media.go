// Package media is the layout of the images on install media, shared by the
// ISO writer and the installer that reads it.
package media

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// IndexPath is relative to the media root.
const IndexPath = "images/purposes.json"

// LabelPrefix starts the image labels that describe a purpose.
const LabelPrefix = "io.github.epheo.installer."

// Image is one purpose: an OCI layout under images/ plus what to call it.
type Image struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Summary   string   `json:"summary"`
	Includes  []string `json:"includes,omitempty"`
	DataDisks string   `json:"dataDisks"` // none, pairs or thin
	Layout    string   `json:"layout"`    // directory under images/
	Tag       string   `json:"tag"`
	Target    string   `json:"target"` // registry reference updates come from
	// SecureBoot is "off" when the image cannot run with Secure Boot on.
	SecureBoot string `json:"secureBoot,omitempty"`
	Bytes      int64  `json:"bytes"` // compressed layers, for install progress
	// Names are what the image serves under a site's wildcard domain.
	Names []string `json:"names,omitempty"`
	// Identity is a sign-in provider the image can install, and
	// IdentityName its name under the domain.
	Identity     string `json:"identity,omitempty"`
	IdentityName string `json:"identityName,omitempty"`
	// Settings is where the installer writes the names and sign-in.
	Settings      string   `json:"settings,omitempty"`
	ReservedUsers []string `json:"reservedUsers,omitempty"`
}

var nameRE = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]{0,61}[a-z0-9])?$`)

// NamesFromLabels reads the domain, identity, settings and reserved-users
// labels into im, and refuses a set the installer could not honor.
func (im *Image) NamesFromLabels(l func(string) string) error {
	split := func(v string) []string {
		if v == "" {
			return nil
		}
		return strings.Split(v, ";")
	}
	im.Names = split(l("domain"))
	im.Settings = l("settings")
	im.ReservedUsers = split(l("reserved-users"))
	if v := l("identity"); v != "" {
		id, name, ok := strings.Cut(v, ":")
		if !ok || !nameRE.MatchString(id) || !nameRE.MatchString(name) {
			return fmt.Errorf("label %sidentity is provider:name, like kanidm:idm, not %q", LabelPrefix, v)
		}
		im.Identity, im.IdentityName = id, name
	}
	for _, n := range append(slices.Clone(im.Names), im.IdentityName) {
		if n != "" && !nameRE.MatchString(n) {
			return fmt.Errorf("label %sdomain: %q is not one DNS label", LabelPrefix, n)
		}
	}
	switch {
	case im.Identity != "" && len(im.Names) == 0:
		return fmt.Errorf("label %sidentity needs %sdomain: a provider needs names", LabelPrefix, LabelPrefix)
	case len(im.Names) > 0 && (!filepath.IsAbs(im.Settings) || !strings.HasPrefix(filepath.Clean(im.Settings), "/etc/")):
		return fmt.Errorf("label %ssettings is a file under /etc, where the answers go", LabelPrefix)
	case len(im.Names) == 0 && im.Settings != "":
		return fmt.Errorf("label %ssettings without %sdomain: there is nothing to write", LabelPrefix, LabelPrefix)
	}
	return nil
}

type Index struct {
	Images []Image `json:"images"`
}

func Read(root string) (Index, error) {
	var ix Index
	b, err := os.ReadFile(filepath.Join(root, IndexPath))
	if err != nil {
		return ix, err
	}
	return ix, json.Unmarshal(b, &ix)
}

func Write(root string, ix Index) error {
	b, err := json.MarshalIndent(ix, "", "  ")
	if err != nil {
		return err
	}
	p := filepath.Join(root, IndexPath)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o644)
}
