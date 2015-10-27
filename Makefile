CC = cc
CXX = c++
PLATFORMS = android-arm \
	darwin-x64 \
	linux-x86 \
	linux-x64 \
	linux-arm \
	windows-x86 \
	windows-x64

DOCKER = docker
DOCKER_IMAGE = anteo/libtorrent-go
GO_PACKAGE_NS = github.com/anteo

include platform_host.mk

ifneq ($(CROSS_TRIPLE),)
	CC := $(CROSS_TRIPLE)-$(CC)
	CXX := $(CROSS_TRIPLE)-$(CXX)
endif

include platform_target.mk

ifeq ($(TARGET_ARCH),x86)
	GOARCH = 386
else ifeq ($(TARGET_ARCH),x64)
	GOARCH = amd64
else ifeq ($(TARGET_ARCH),arm)
	GOARCH = arm
	GOARM = 7
endif

BUILDMODE = default
ifeq ($(TARGET_OS), windows)
	EXT = .exe
	GOOS = windows
	LDFLAGS := $(LDFLAGS) -linkmode=external -extld=$(CC) '-extldflags=-lstdc++ -static'
else ifeq ($(TARGET_OS), darwin)
	EXT =
	GOOS = darwin
	LDFLAGS := $(LDFLAGS) -linkmode=external -extld=$(CC) -extldflags=-lstdc++
else ifeq ($(TARGET_OS), linux)
	EXT =
	GOOS = linux
	LDFLAGS := $(LDFLAGS) -linkmode=external -extld=$(CC) -extldflags=-lstdc++ -extldflags=-lrt
else ifeq ($(TARGET_OS), android)
	EXT =
	GOOS = android
	LDFLAGS := $(LDFLAGS) -linkmode=external -extld=$(CC) -extldflags=-lstdc++
	BUILDMODE = pie
endif

NAME = torrent2http
GO = go
CGO_ENABLED = 1
OUTPUT_NAME = $(NAME)$(EXT)
BUILD_PATH = build/$(TARGET_OS)_$(TARGET_ARCH)
LIBRARY_PATH = $(GOPATH)/pkg/$(GOOS)_$(GOARCH)/$(GO_PACKAGE_NS)

.PHONY: $(PLATFORMS)

all: $(PLATFORMS)

$(PLATFORMS):
	$(DOCKER) run -it --rm -v $(HOME):$(HOME) -v /tmp:/tmp -t -e GOPATH=$(GOPATH) -w $(shell pwd) $(DOCKER_IMAGE):$@ make clean dist;

$(BUILD_PATH):
	mkdir -p $(BUILD_PATH)

$(BUILD_PATH)/$(OUTPUT_NAME): $(BUILD_PATH)
	CC=$(CC) GOOS=$(GOOS) GOARCH=$(GOARCH) GOARM=$(GOARM) CGO_ENABLED=$(CGO_ENABLED) $(GO) build -v -x -o $(BUILD_PATH)/$(OUTPUT_NAME) -ldflags="$(LDFLAGS)" -buildmode=$(BUILDMODE)

dist: $(BUILD_PATH)/$(OUTPUT_NAME)

clean:
	rm -rf $(BUILD_PATH)
