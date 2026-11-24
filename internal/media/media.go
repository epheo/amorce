// Package media is the layout of the images on install media, shared by the
// ISO writer and the installer that reads it.
package media

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	DataDisks string   `json:"dataDisks"` // none or pairs
	Layout    string   `json:"layout"`    // directory under images/
	Tag       string   `json:"tag"`
	Target    string   `json:"target"` // registry reference updates come from
	// SecureBoot is "off" when the image cannot run with Secure Boot on.
	SecureBoot string `json:"secureBoot,omitempty"`
	Bytes      int64  `json:"bytes"` // compressed layers, for install progress
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
