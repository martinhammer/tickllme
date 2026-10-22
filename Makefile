# SPDX-License-Identifier: GPL-3.0-or-later

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
STATICCHECK ?= go run honnef.co/go/tools/cmd/staticcheck@latest
MAKEFAT ?= go run github.com/randall77/makefat@latest
MCPB ?= npx --yes @anthropic-ai/mcpb
GOBUILD := CGO_ENABLED=0 go build -trimpath -ldflags "$(LDFLAGS)"

# The builds the plugin and the Desktop extension carry (decisions 4 and 5).
BUILDS := linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64

DIST := dist
PLUGIN := $(DIST)/plugin
BUNDLE := $(DIST)/mcpb
PLUGIN_ZIP := $(DIST)/tickllme-plugin-$(VERSION).zip
MCPB_FILE := $(DIST)/tickllme-$(VERSION).mcpb

.PHONY: build test lint builds plugin mcpb release-local check-version clean

build:
	$(GOBUILD) -o tickllme ./cmd/tickllme

test:
	go test ./...

lint:
	go vet ./...
	$(STATICCHECK) ./...

# Static binaries for every target in dist/builds/<os>-<arch>/, plus the
# universal macOS binary in dist/builds/darwin-universal/.
builds:
	@for t in $(BUILDS); do \
		os=$${t%/*}; arch=$${t#*/}; ext=; [ $$os = windows ] && ext=.exe; \
		echo "$(DIST)/builds/$$os-$$arch/tickllme$$ext"; \
		GOOS=$$os GOARCH=$$arch $(GOBUILD) -o $(DIST)/builds/$$os-$$arch/tickllme$$ext ./cmd/tickllme || exit 1; \
	done
	@mkdir -p $(DIST)/builds/darwin-universal
	$(MAKEFAT) $(DIST)/builds/darwin-universal/tickllme $(DIST)/builds/darwin-amd64/tickllme $(DIST)/builds/darwin-arm64/tickllme

# The plugin zip of decision 4: the plugin files at its root, and
# builds/<os>-<arch>/ for the launcher to pick from.
plugin: builds check-version
	rm -rf $(PLUGIN) $(PLUGIN_ZIP)
	mkdir -p $(PLUGIN)/builds
	cp -R .claude-plugin skills launcher LICENSE README.md $(PLUGIN)/
	rm -f $(PLUGIN)/.claude-plugin/marketplace.json
	for b in linux-amd64 linux-arm64 darwin-universal windows-amd64; do cp -R $(DIST)/builds/$$b $(PLUGIN)/builds/; done
	chmod 755 $(PLUGIN)/launcher/tickllme.sh $(PLUGIN)/builds/*/tickllme
	cd $(PLUGIN) && zip -qrX ../$(notdir $(PLUGIN_ZIP)) .
	@echo $(PLUGIN_ZIP)

# The Desktop extension of decision 5.
mcpb: builds check-version
	rm -rf $(BUNDLE) $(MCPB_FILE)
	mkdir -p $(BUNDLE)/server/linux-amd64 $(BUNDLE)/server/linux-arm64 $(BUNDLE)/launcher
	cp mcpb/manifest.json LICENSE $(BUNDLE)/
	cp launcher/tickllme.sh $(BUNDLE)/launcher/
	cp $(DIST)/builds/darwin-universal/tickllme $(BUNDLE)/server/tickllme
	cp $(DIST)/builds/windows-amd64/tickllme.exe $(BUNDLE)/server/tickllme.exe
	cp $(DIST)/builds/linux-amd64/tickllme $(BUNDLE)/server/linux-amd64/
	cp $(DIST)/builds/linux-arm64/tickllme $(BUNDLE)/server/linux-arm64/
	chmod 755 $(BUNDLE)/launcher/tickllme.sh $(BUNDLE)/server/tickllme $(BUNDLE)/server/linux-*/tickllme
	$(MCPB) validate $(BUNDLE)/manifest.json
	$(MCPB) pack $(BUNDLE) $(MCPB_FILE)

release-local: plugin mcpb
	@sha256sum $(PLUGIN_ZIP) $(MCPB_FILE)

# The manifests must carry the version being built (vX.Y.Z → X.Y.Z), so
# Claude Code sees an update. A dev build skips the check.
check-version:
	@v=$(VERSION); v=$${v#v}; case $$v in [0-9]*.[0-9]*.[0-9]*) ;; *) exit 0;; esac; \
	for f in .claude-plugin/plugin.json mcpb/manifest.json; do \
		grep -q "\"version\": \"$$v\"" $$f || { echo "$$f doesn't say version $$v" >&2; exit 1; }; \
	done

clean:
	rm -rf $(DIST) tickllme
