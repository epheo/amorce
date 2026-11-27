package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// Two images in one layout share a layer; each is read by its own tag.
func TestReadLayoutByTag(t *testing.T) {
	dir := t.TempDir()
	put := func(v any) string {
		b, _ := json.Marshal(v)
		sum := fmt.Sprintf("%x", sha256.Sum256(b))
		p := filepath.Join(dir, "blobs", "sha256", sum)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatal(err)
		}
		return "sha256:" + sum
	}
	type m = map[string]any
	shared := m{"size": 100}
	image := func(id string, own int) string {
		cfg := put(m{"config": m{"Labels": m{"io.github.epheo.installer.id": id}}})
		return put(m{"config": m{"digest": cfg}, "layers": []m{shared, {"size": own}}})
	}
	ix := m{"manifests": []m{
		{"digest": image("microshift", 1), "annotations": m{"org.opencontainers.image.ref.name": "microshift-latest"}},
		{"digest": image("storage", 20), "annotations": m{"org.opencontainers.image.ref.name": "stornas-os-latest"}},
	}}
	b, _ := json.Marshal(ix)
	os.WriteFile(filepath.Join(dir, "index.json"), b, 0o644)

	labels, size, err := readLayout(dir, "stornas-os-latest")
	if err != nil || labels["io.github.epheo.installer.id"] != "storage" || size != 120 {
		t.Fatalf("stornas-os: %v %d %v", labels, size, err)
	}
	if labels, _, _ := readLayout(dir, "microshift-latest"); labels["io.github.epheo.installer.id"] != "microshift" {
		t.Fatalf("microshift: %v", labels)
	}
	if _, _, err := readLayout(dir, "dotvirt-appliance-latest"); err == nil {
		t.Fatal("a tag that is not there")
	}
}
