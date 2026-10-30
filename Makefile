# Set these to the desired values
ARTIFACT_ID=cldk
VERSION=0.1.0
GOTAG=1.27.3
MAKEFILES_VERSION=11.2.0
LINT_VERSION?=v2.15.0

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
include build/make/mocks.mk
include build/make/release.mk
include build/make/clean.mk
