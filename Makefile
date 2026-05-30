# ===============================
# Project Configuration
# ===============================
APP_NAME        := anthropic-proxy
WORKDIR         := $(CURDIR)
BUILD_DIR       := build
GOCMD           := go
GOBUILD         := $(GOCMD) build
GO_TAGS         := netgo
LDFLAGS         := -s -w
RELEASE_FLAGS   := -trimpath -tags=$(GO_TAGS) -ldflags="$(LDFLAGS)"

APP_SOURCE      := $(WORKDIR)/cmd/anthropic-proxy
# ===============================
# OS / ARCH Matrix (per OS)
# ===============================
LINUX_ARCHES   := amd64 arm64 arm 386
DARWIN_ARCHES  := amd64 arm64
FREEBSD_ARCHES := amd64 arm64 arm 386
OPENBSD_ARCHES := amd64 arm 386
WINDOWS_ARCHES := amd64 arm64 386

# ===============================
# Host Detection
# ===============================
UNAME_S := $(shell uname -s)
UNAME_M := $(shell uname -m)

ifeq ($(UNAME_S),Linux)
  HOST_OS := linux
else ifeq ($(UNAME_S),Darwin)
  HOST_OS := darwin
else ifeq ($(UNAME_S),FreeBSD)
  HOST_OS := freebsd
else ifeq ($(UNAME_S),OpenBSD)
  HOST_OS := openbsd
else
  HOST_OS := unknown
endif

ifeq ($(UNAME_M),x86_64)
  HOST_ARCH := amd64
else ifeq ($(UNAME_M),aarch64)
  HOST_ARCH := arm64
else ifeq ($(UNAME_M),i386)
  HOST_ARCH := 386
else
  HOST_ARCH := $(UNAME_M)
endif


# ===============================
# Helper (generic Go build)
# ===============================
define build-go
	@echo ">> Building $(1) for $(2)/$(3)"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(2) GOARCH=$(3) \
	$(GOBUILD) $(RELEASE_FLAGS) \
	-o $(BUILD_DIR)/$(1)-$(2)-$(3) \
	$(4)
endef

# Windows builds need .exe suffix
define build-go-windows
	@echo ">> Building $(1) for $(2)/$(3)"
	@mkdir -p $(BUILD_DIR)
	CGO_ENABLED=0 GOOS=$(2) GOARCH=$(3) \
	$(GOBUILD) $(RELEASE_FLAGS) \
	-o $(BUILD_DIR)/$(1)-$(2)-$(3).exe \
	$(4)
endef

define build-all-binaries
	$(call build-go,$(APP_NAME),$(1),$(2),$(APP_SOURCE))
endef

define build-all-binaries-windows
	$(call build-go-windows,$(APP_NAME),$(1),$(2),$(APP_SOURCE))
endef

# ===============================
# Default Targets
# ===============================
.PHONY: default all clean local run

default: local

all: \
	$(foreach a,$(LINUX_ARCHES),build-linux-$(a)) \
	$(foreach a,$(DARWIN_ARCHES),build-darwin-$(a)) \
	$(foreach a,$(FREEBSD_ARCHES),build-freebsd-$(a)) \
	$(foreach a,$(OPENBSD_ARCHES),build-openbsd-$(a)) \
	$(foreach a,$(WINDOWS_ARCHES),build-windows-$(a))

clean:
	rm -rf $(BUILD_DIR)

local:
	$(call build-go,$(APP_NAME),$(HOST_OS),$(HOST_ARCH),$(APP_SOURCE))

run: local
	./$(BUILD_DIR)/$(APP_NAME)-$(HOST_OS)-$(HOST_ARCH)

# ===============================
# OS / ARCH Targets
# ===============================
$(foreach a,$(LINUX_ARCHES), \
  $(eval build-linux-$(a): ; $(call build-all-binaries,linux,$(a))) )

$(foreach a,$(DARWIN_ARCHES), \
  $(eval build-darwin-$(a): ; $(call build-all-binaries,darwin,$(a))) )

$(foreach a,$(FREEBSD_ARCHES), \
  $(eval build-freebsd-$(a): ; $(call build-all-binaries,freebsd,$(a))) )

$(foreach a,$(OPENBSD_ARCHES), \
  $(eval build-openbsd-$(a): ; $(call build-all-binaries,openbsd,$(a))) )

$(foreach a,$(WINDOWS_ARCHES), \
  $(eval build-windows-$(a): ; $(call build-all-binaries-windows,windows,$(a))) )
