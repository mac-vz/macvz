# Files are installed under $(DESTDIR)/$(PREFIX)
PREFIX ?= /usr/local
DEST := $(shell echo "$(DESTDIR)/$(PREFIX)" | sed 's:///*:/:g; s://*$$::')

GO ?= go

TAR ?= tar

PACKAGE := github.com/balaji113/macvz

VERSION=1.0.0
VERSION_TRIMMED := 1.0.0

GO_BUILD := $(GO) build -ldflags="-s -w -X $(PACKAGE)/pkg/version.Version=$(VERSION)"

.PHONY: all
all: binaries codesign

.PHONY: binaries
binaries: \
	_output/bin/macvz \

.PHONY: _output/bin/macvz
_output/bin/macvz:
	# The hostagent must be compiled with CGO_ENABLED=1 so that net.LookupIP() in the DNS server
	# calls the native resolver library and not the simplistic version in the Go library.
	CGO_ENABLED=1 $(GO_BUILD) -o $@ ./cmd/macvz

.PHONY: codesign
codesign:
	codesign --entitlements vz.entitlements -s - ./_output/bin/macvz

.PHONY: install
install:
	mkdir -p "$(DEST)"
	cp -av _output/* "$(DEST)"

.PHONY: uninstall
uninstall:
	@test -f "$(DEST)/bin/macvz" || (echo "macvz not found in $(DEST) prefix"; exit 1)
	rm -rf \
		"$(DEST)/bin/macvz" \
		"$(DEST)/bin/vfcli"

.PHONY: lint
lint:
	golangci-lint run ./...
	yamllint .
	find . -name '*.sh' | xargs shellcheck
	find . -name '*.sh' | xargs shfmt -s -d

.PHONY: clean
clean:
	rm -rf _output

.PHONY: artifacts-darwin
artifacts-darwin:
	mkdir -p _artifacts
	GOOS=darwin GOARCH=amd64 make clean binaries
	$(TAR) -C _output/ -czvf _artifacts/macvz-$(VERSION_TRIMMED)-Darwin-x86_64.tar.gz ./
	GOOS=darwin GOARCH=arm64 make clean binaries
	$(TAR) -C _output -czvf _artifacts/macvz-$(VERSION_TRIMMED)-Darwin-arm64.tar.gz ./

