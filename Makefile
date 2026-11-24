IMAGE ?= localhost/microshift-installer:dev
OUT ?= out

.PHONY: image iso test screens

image:
	podman build -t $(IMAGE) .

# make iso ISOFLAGS='--image ghcr.io/epheo/stornas-os:latest'
iso: image
	mkdir -p $(OUT)
	podman run --rm -v $(abspath $(OUT)):/out:Z $(IMAGE) iso --out /out/installer.iso $(ISOFLAGS)

test:
	go vet ./...
	go test ./...

screens:
	go run ./cmd/installer screens $(OUT)/screens
