package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/epheo/amorce/internal/media"
	"github.com/epheo/amorce/internal/site"
)

type strList []string

func (s *strList) String() string     { return strings.Join(*s, ",") }
func (s *strList) Set(v string) error { *s = append(*s, v); return nil }

// isoCmd writes install media from this very image: its root becomes the
// live system, and each --image becomes a purpose on the media.
func isoCmd(args []string) error {
	fs := flag.NewFlagSet("iso", flag.ExitOnError)
	out := fs.String("out", "", "ISO file to write")
	var images strList
	fs.Var(&images, "image", "bootc image to put on the media, repeatable (registry reference or skopeo transport:ref)")
	label := fs.String("label", "MSINST", "volume label the live system boots from")
	payloadOnly := fs.Bool("payload-only", false, "write only the images and site file, labeled MSPAYLOAD (MSPAYLOAD-SITE with no images), for media the installer reads beside its own")
	root := fs.String("root", "/", "live root to put on the media")
	kargs := fs.String("kargs", "", "extra kernel arguments for the live system")
	// One image without labels can be described by flags.
	id := fs.String("id", "", "purpose id when the image has no labels")
	name := fs.String("name", "", "purpose name when the image has no labels")
	summary := fs.String("summary", "", "one-line summary when the image has no labels")
	includes := fs.String("includes", "", "what it includes, separated by ';', when the image has no labels")
	dataDisks := fs.String("data-disks", "", "none or pairs, when the image has no labels")
	targetRef := fs.String("target", "", "registry reference updates come from, when the image is not pulled from one")
	sbOff := fs.Bool("needs-secure-boot-off", false, "the image cannot run with Secure Boot on, when it has no label saying so")
	sitePath := fs.String("site", "", "site file answering the wizard for the machines it names; with --payload-only alone, media for one site")
	fs.Parse(args)
	if *out == "" {
		return fmt.Errorf("iso needs --out")
	}
	// Each medium needs its own label to be found; site media sit beside
	// image media.
	if *payloadOnly && *label == "MSINST" {
		*label = "MSPAYLOAD"
		if len(images) == 0 {
			*label = "MSPAYLOAD-SITE"
		}
	}
	work := filepath.Join(filepath.Dir(*out), ".iso-work")
	os.RemoveAll(work)
	tree := filepath.Join(work, "iso")
	defer os.RemoveAll(work)

	var ix media.Index
	for _, ref := range images {
		over := media.Image{ID: *id, Name: *name, Summary: *summary, DataDisks: *dataDisks, Target: *targetRef}
		if *sbOff {
			over.SecureBoot = "off"
		}
		if *includes != "" {
			over.Includes = strings.Split(*includes, ";")
		}
		if len(images) > 1 {
			over = media.Image{}
		}
		im, err := addImage(tree, ref, over)
		if err != nil {
			return err
		}
		fmt.Printf("%s: installs as %s, updates from %s\n", ref, im.Name, im.Target)
		ix.Images = append(ix.Images, im)
	}
	if len(ix.Images) > 0 {
		if err := media.Write(tree, ix); err != nil {
			return err
		}
	}
	if *sitePath != "" {
		if err := addSite(tree, *sitePath); err != nil {
			return err
		}
	}
	if !*payloadOnly {
		if err := liveTree(*root, tree, *label, *kargs); err != nil {
			return err
		}
	}
	xargs := []string{"-as", "mkisofs", "-quiet", "-o", *out, "-R", "-J", "-V", *label, "-iso-level", "3"}
	if !*payloadOnly {
		xargs = append(xargs, "-e", "images/efiboot.img", "-no-emul-boot", "-isohybrid-gpt-basdat")
	}
	if err := sh("xorriso", append(xargs, tree)...); err != nil {
		return err
	}
	fmt.Println("wrote", *out)
	return nil
}

// addSite checks the site file before any machine reads it from the media.
func addSite(tree, path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	f, err := site.Parse(b)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(tree, 0o755); err != nil {
		return err
	}
	fmt.Printf("%s: site %q, %d machines\n", path, f.Site, len(f.Machines))
	return os.WriteFile(filepath.Join(tree, site.FileName), b, 0o644)
}

func sh(name string, args ...string) error {
	c := exec.Command(name, args...)
	c.Stdout, c.Stderr = os.Stderr, os.Stderr
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %v", name, err)
	}
	return nil
}

// liveTree adds the kernel, the live root and the EFI boot image.
func liveTree(root, tree, label, kargs string) error {
	mods, _ := filepath.Glob(filepath.Join(root, "usr/lib/modules/*/vmlinuz"))
	if len(mods) != 1 {
		return fmt.Errorf("want one kernel under %s/usr/lib/modules, found %d", root, len(mods))
	}
	kdir := filepath.Dir(mods[0])
	pxe := filepath.Join(tree, "images/pxeboot")
	efi := filepath.Join(tree, "EFI/BOOT")
	for _, d := range []string{pxe, efi, filepath.Join(tree, "LiveOS")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	copies := [][2]string{
		{filepath.Join(kdir, "vmlinuz"), filepath.Join(pxe, "vmlinuz")},
		{filepath.Join(kdir, "initramfs-live.img"), filepath.Join(pxe, "initrd.img")},
		{filepath.Join(root, "usr/lib/bootupd/updates/EFI/BOOT/BOOTX64.EFI"), filepath.Join(efi, "BOOTX64.EFI")},
		{filepath.Join(root, "usr/lib/bootupd/updates/EFI/centos/grubx64.efi"), filepath.Join(efi, "grubx64.efi")},
	}
	for _, c := range copies {
		if err := sh("cp", c[0], c[1]); err != nil {
			return err
		}
	}
	args := strings.TrimSpace("root=live:CDLABEL=" + label + " rd.live.image enforcing=0 quiet loglevel=3 " +
		"systemd.show_status=false audit=0 console=tty0 console=ttyS0,115200 " + kargs)
	cfg := fmt.Sprintf(`set default=0
set timeout=3
search --no-floppy --set=root -l %s
menuentry "Install" {
  linux /images/pxeboot/vmlinuz %s
  initrd /images/pxeboot/initrd.img
}
menuentry "Install, plain console look" {
  linux /images/pxeboot/vmlinuz %s inst.look=normal
  initrd /images/pxeboot/initrd.img
}
`, label, args, args)
	if err := os.WriteFile(filepath.Join(efi, "grub.cfg"), []byte(cfg), 0o644); err != nil {
		return err
	}

	// The container's own mounts are not part of the image: leave them out
	// and put back what the live system expects in their place.
	sq := []string{root, filepath.Join(tree, "LiveOS/squashfs.img"), "-comp", "zstd", "-noappend", "-quiet",
		"-no-xattrs", "-wildcards",
		"-e", "proc", "sys", "dev", "run", "tmp", "var/tmp", "out", "etc/hosts", "etc/hostname", "etc/resolv.conf",
		"usr/lib/modules/*/initramfs*.img",
		"-p", "proc d 555 0 0", "-p", "sys d 555 0 0", "-p", "dev d 755 0 0", "-p", "run d 755 0 0",
		"-p", "tmp d 1777 0 0", "-p", "var/tmp d 1777 0 0",
		"-p", "etc/hosts f 644 0 0 printf '127.0.0.1 localhost\\n::1 localhost\\n'",
		"-p", "etc/resolv.conf s 777 0 0 ../run/NetworkManager/resolv.conf",
	}
	if rel, err := filepath.Rel(root, filepath.Dir(tree)); err == nil && !strings.HasPrefix(rel, "..") {
		sq = append(sq, "-e", rel)
	}
	if err := sh("mksquashfs", sq...); err != nil {
		return err
	}

	img := filepath.Join(tree, "images/efiboot.img")
	if err := sh("mkfs.vfat", "-C", img, "16384"); err != nil {
		return err
	}
	if err := sh("mmd", "-i", img, "::/EFI", "::/EFI/BOOT"); err != nil {
		return err
	}
	return sh("mcopy", "-i", img, filepath.Join(efi, "BOOTX64.EFI"), filepath.Join(efi, "grubx64.efi"),
		filepath.Join(efi, "grub.cfg"), "::/EFI/BOOT/")
}

// addImage copies one image into images/ and describes it from its labels.
func addImage(tree, ref string, over media.Image) (media.Image, error) {
	src, target := ref, ""
	transports := []string{"docker://", "oci:", "oci-archive:", "containers-storage:", "docker-archive:"}
	isTransport := false
	for _, t := range transports {
		isTransport = isTransport || strings.HasPrefix(ref, t)
	}
	if !isTransport {
		src, target = "docker://"+ref, ref
	}
	if t, ok := strings.CutPrefix(ref, "docker://"); ok {
		target = t
	}
	// name:tag from the last path element, whatever the transport.
	last := ref[strings.LastIndex(ref, "/")+1:]
	last = strings.Split(last, "@")[0]
	layout, tag, ok := strings.Cut(last, ":")
	if !ok || tag == "" {
		tag = "latest"
	}
	dir := filepath.Join(tree, "images", layout)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return media.Image{}, err
	}
	if err := sh("skopeo", "copy", "-q", src, "oci:"+dir+":"+tag); err != nil {
		return media.Image{}, err
	}
	labels, size, err := readLayout(dir)
	if err != nil {
		return media.Image{}, err
	}
	l := func(k string) string { return labels[media.LabelPrefix+k] }
	// Updates come from where the image was taken. Labels are inherited by
	// every image built FROM this one, so the target label only speaks for
	// local builds and digest pins, which name no place to update from.
	pulled := target
	if strings.Contains(pulled, "@") {
		pulled = ""
	}
	im := media.Image{
		ID: l("id"), Name: l("name"), Summary: l("summary"), DataDisks: l("data-disks"), SecureBoot: l("secure-boot"),
		Layout: layout, Tag: tag, Target: firstOf(over.Target, pulled, l("target"), target), Bytes: size,
	}
	if v := l("includes"); v != "" {
		im.Includes = strings.Split(v, ";")
	}
	im.ID = firstOf(over.ID, im.ID)
	im.Name = firstOf(over.Name, im.Name, labels["org.opencontainers.image.title"], layout)
	im.Summary = firstOf(over.Summary, im.Summary)
	im.DataDisks = firstOf(over.DataDisks, im.DataDisks, "none")
	if over.Includes != nil {
		im.Includes = over.Includes
	}
	im.SecureBoot = firstOf(over.SecureBoot, im.SecureBoot)
	im.ID = firstOf(im.ID, strings.ToLower(strings.ReplaceAll(im.Name, " ", "-")))
	if im.Target == "" {
		return im, fmt.Errorf("%s: no registry to update from; label it %starget", ref, media.LabelPrefix)
	}
	return im, nil
}

func firstOf(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}

// readLayout returns an OCI layout's labels and compressed layer bytes.
func readLayout(dir string) (map[string]string, int64, error) {
	blob := func(digest string) ([]byte, error) {
		alg, hex, _ := strings.Cut(digest, ":")
		return os.ReadFile(filepath.Join(dir, "blobs", alg, hex))
	}
	var index struct {
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "index.json"))
	if err != nil {
		return nil, 0, err
	}
	if err := json.Unmarshal(b, &index); err != nil || len(index.Manifests) == 0 {
		return nil, 0, fmt.Errorf("%s: no manifest", dir)
	}
	var man struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Layers []struct {
			Size int64 `json:"size"`
		} `json:"layers"`
	}
	if b, err = blob(index.Manifests[0].Digest); err != nil {
		return nil, 0, err
	}
	if err := json.Unmarshal(b, &man); err != nil {
		return nil, 0, err
	}
	var size int64
	for _, l := range man.Layers {
		size += l.Size
	}
	var cfg struct {
		Config struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if b, err = blob(man.Config.Digest); err != nil {
		return nil, 0, err
	}
	return cfg.Config.Labels, size, json.Unmarshal(b, &cfg)
}
