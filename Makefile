# Set these to the desired values
ARTIFACT_ID=cldk
VERSION=0.1.0
GOTAG=1.27.4
MAKEFILES_VERSION=11.2.0
LINT_VERSION?=v2.15.0
GOVULNCHECK_VERSION?=v1.9.0

.DEFAULT_GOAL:=compile

GO_ENV_VARS=CGO_ENABLED=0 GOOS=linux GOARCH=amd64

# Integration tests need no docker-compose environment, so the default start/stop hooks are disabled
PRE_INTEGRATIONTESTS=
POST_INTEGRATIONTESTS=

include build/make/variables.mk
include build/make/self-update.mk
include build/make/dependencies-gomod.mk
include build/make/build.mk
include build/make/test-common.mk
include build/make/test-unit.mk
include build/make/test-integration.mk
include build/make/static-analysis.mk
include build/make/vulnerability-scan.mk
include build/make/mocks.mk
include build/make/release.mk
include build/make/clean.mk

##@ Release assets

RELEASE_PLATFORMS=linux/amd64 linux/arm64 darwin/amd64 darwin/arm64
RELEASE_ASSETS_DIR=$(TARGET_DIR)/dist
RELEASE_ASSET_FILES=$(ARTIFACT_ID) LICENSE README.md CHANGELOG.md

.PHONY: release-assets
release-assets: vendor ## Build the release archives of all platforms and their checksums into target/dist
	@rm -rf $(RELEASE_ASSETS_DIR) && mkdir -p $(RELEASE_ASSETS_DIR)
	@for platform in $(RELEASE_PLATFORMS); do \
		os=$${platform%/*}; arch=$${platform#*/}; \
		name=$(ARTIFACT_ID)_$(VERSION)_$${os}_$${arch}; \
		dir=$(RELEASE_ASSETS_DIR)/$${name}; \
		echo "Building $${name}..."; \
		mkdir -p $${dir} && cp LICENSE README.md CHANGELOG.md $${dir}/ || exit 1; \
		CGO_ENABLED=0 GOOS=$${os} GOARCH=$${arch} go build -mod=vendor -trimpath $(LDFLAGS) -o $${dir}/$(ARTIFACT_ID) . || exit 1; \
		tar $(TAR_ARGS) -czf $${dir}.tar.gz -C $${dir} $(RELEASE_ASSET_FILES) && rm -rf $${dir} || exit 1; \
	done
	@cd $(RELEASE_ASSETS_DIR) && sha256sum *.tar.gz > checksums.txt
	@ls -1 $(RELEASE_ASSETS_DIR)
