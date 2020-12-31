CC = cc
CXX = c++
STRIP = strip

include platform_host.mk

ifneq ($(CROSS_TRIPLE),)
	CC := $(CROSS_TRIPLE)-$(CC)
	CXX := $(CROSS_TRIPLE)-$(CXX)
	STRIP := $(CROSS_TRIPLE)-strip
endif

include platform_target.mk

ifeq ($(TARGET_ARCH), x86)
	GOARCH = 386
else ifeq ($(TARGET_ARCH), x64)
	GOARCH = amd64
else ifeq ($(TARGET_ARCH), arm)
	GOARCH = arm
	GOARM = 6
else ifeq ($(TARGET_ARCH), armv6)
	GOARCH = arm
	GOARM = 6
else ifeq ($(TARGET_ARCH), armv7)
	GOARCH = arm
	GOARM = 7
	PKGDIR = -pkgdir /go/pkg/linux_armv7
else ifeq ($(TARGET_ARCH), arm64)
	GOARCH = arm64
	GOARM =
endif

ifeq ($(TARGET_OS), windows)
	EXT = .exe
	GOOS = windows
	# TODO Remove for golang 1.8
	# https://github.com/golang/go/issues/8756
	# GO_LDFLAGS = -extldflags=-Wl,--allow-multiple-definition -v
	# GO_LDFLAGS += -linkmode=external -extld=$(CC) -extldflags "-static-libgcc -static-libstdc++ -Wl,--allow-multiple-definition -v"
else ifeq ($(TARGET_OS), darwin)
	EXT =
	GOOS = darwin
	# Needs this or cgo will try to link with libgcc, which will fail
	CC := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang
	CXX := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang++
	GO_LDFLAGS += -linkmode=external -extld=$(CC) -extldflags "-lm"
else ifeq ($(TARGET_OS), linux)
	EXT =
	GOOS = linux
	GO_LDFLAGS += -linkmode=external -extld=$(CC) -extldflags "-L $(CROSS_ROOT)/lib/ -lm -lstdc++"
else ifeq ($(TARGET_OS), android)
	EXT =
	GOOS = android
	ifeq ($(TARGET_ARCH), arm)
		GOARM = 7
	else
		GOARM =
	endif
	# GO_LDFLAGS += -extldflags=-pie
	GO_LDFLAGS += -linkmode=external -extld=$(CC) -extldflags "-pie -lm -lc++_shared" 
	# GO_LDFLAGS += -linkmode=external -extldflags "-pie"
	CC := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang
	CXX := $(CROSS_ROOT)/bin/$(CROSS_TRIPLE)-clang++
endif


PLATFORMS = \
	android-arm \
	android-arm64 \
	android-x64 \
	android-x86 \
	linux-armv6 \
	linux-armv7 \
	linux-arm64 \
	linux-x64 \
	linux-x86 \
	windows-x64 \
	windows-x86 \
	darwin-x64


# CC = armv7-linux-gnueabihf-gcc
# CXX = armv7-linux-gnueabihf-g++
# GOPATH = ~/go
# 	
# DOCKER = docker
# DOCKER_IMAGE = libtorrent-go
# GO_PACKAGE_NS = github.com/ElementumOrg
# 
# CC = armv7-linux-gnueabihf-gcc
# CXX = armv7-linux-gnueabihf-g++
# GOPATH = /home/viorel/go
# STRIP = armv7-linux-gnueabihf-strip
# 
# NAME = torrent2http
# GO = go
# CGO_ENABLED = 1
# OUTPUT_NAME = $(NAME)$(EXT)
# BUILD_PATH = build/$(TARGET_OS)_$(TARGET_ARCH)
# LIBRARY_PATH = $(GOPATH)/pkg/$(GOOS)_$(GOARCH)/$(GO_PACKAGE_NS)


PROJECT = elementumorg
NAME = torrent2http
GO_PKG = github.com/elgatito/elementum
GO = go
GIT = git
DOCKER = docker
DOCKER_IMAGE = libtorrent-go
UPX = upx
GIT_VERSION = $(shell $(GIT) describe --tags)
CGO_ENABLED = 1
OUTPUT_NAME = $(NAME)$(EXT)
BUILD_PATH = build/$(TARGET_OS)_$(TARGET_ARCH)
LIBTORRENT_GO = github.com/ElementumOrg/libtorrent-go
LIBTORRENT_GO_HOME = $(shell go env GOPATH)/src/$(LIBTORRENT_GO)
GO_BUILD_TAGS =
GO_LDFLAGS += -s -w -X $(GO_PKG)/util.Version=$(GIT_VERSION)
GO_EXTRALDFLAGS =

.PHONY: $(PLATFORMS)

all: $(PLATFORMS)

$(PLATFORMS):
	$(MAKE) build TARGET_OS=$(firstword $(subst -, ,$@)) TARGET_ARCH=$(word 2, $(subst -, ,$@))

force:
	@true

$(BUILD_PATH):
	mkdir -p $(BUILD_PATH)

$(BUILD_PATH)/$(OUTPUT_NAME): $(BUILD_PATH) force
	LDFLAGS='$(LDFLAGS)' \
	CFLAGS='$(CFLAGS) -std=c++11' \
	CC='$(CC)' CXX='$(CXX)' \
	GOOS='$(GOOS)' GOARCH='$(GOARCH)' GOARM='$(GOARM)' \
	CGO_ENABLED='$(CGO_ENABLED)' \
	$(GO) build -v \
		-gcflags '$(GO_GCFLAGS)' \
		-ldflags '$(GO_LDFLAGS)' \
		-o '$(BUILD_PATH)/$(OUTPUT_NAME)' \
		$(PKGDIR)
	# set -x && \
	# $(GO) vet -unsafeptr=false .
	chmod -R 777 $(BUILD_PATH)

vendor_darwin vendor_linux:

vendor_windows:
	find $(shell go env GOPATH)/pkg/$(GOOS)_$(GOARCH) -name *.dll -exec cp -f {} $(BUILD_PATH) \;

vendor_android:
	cp $(CROSS_ROOT)/sysroot/usr/lib/$(CROSS_TRIPLE)/libc++_shared.so $(BUILD_PATH)
	chmod +rx $(BUILD_PATH)/libc++_shared.so
	# cp $(CROSS_ROOT)/$(CROSS_TRIPLE)/lib/libgnustl_shared.so $(BUILD_PATH)
	# chmod +rx $(BUILD_PATH)/libgnustl_shared.so

vendor_libs_windows:

vendor_libs_android:
	$(CROSS_ROOT)/sysroot/usr/lib/$(CROSS_TRIPLE)/libc++_shared.so
	# $(CROSS_ROOT)/$(CROSS_TRIPLE)/lib/libgnustl_shared.so

build: force
ifeq ($(TARGET_OS), windows)
	GOOS=windows $(GO) get -u github.com/StackExchange/wmi
endif	
	$(DOCKER) run -it --rm -v $(HOME):$(HOME) -v /tmp:/tmp -t -e GOPATH=$(GOPATH) -w $(shell pwd) $(DOCKER_IMAGE):$(TARGET_OS)-$(TARGET_ARCH) make clean dist TARGET_OS=$(TARGET_OS) TARGET_ARCH=$(TARGET_ARCH);

	
ifeq ($(TARGET_ARCH), arm)
dist: $(BUILD_PATH)/$(OUTPUT_NAME) vendor_$(TARGET_OS) strip
else ifeq ($(TARGET_ARCH), armv6)
dist: $(BUILD_PATH)/$(OUTPUT_NAME) vendor_$(TARGET_OS) strip
else ifeq ($(TARGET_ARCH), armv7)
dist: $(BUILD_PATH)/$(OUTPUT_NAME) vendor_$(TARGET_OS) strip
else ifeq ($(TARGET_ARCH), arm64)
dist: $(BUILD_PATH)/$(OUTPUT_NAME) vendor_$(TARGET_OS) strip
else ifeq ($(TARGET_OS), darwin)
dist: $(BUILD_PATH)/$(OUTPUT_NAME) vendor_$(TARGET_OS) strip
else
dist: $(BUILD_PATH)/$(OUTPUT_NAME) vendor_$(TARGET_OS) strip
endif
# dist: $(BUILD_PATH)/$(OUTPUT_NAME)

clean:
	rm -rf $(BUILD_PATH)
strip: 
	@find $(BUILD_PATH) -type f ! -exec $(STRIP) {} \;
#-lm -lc -lgcc -lc
