GO ?= go
TARGET_OS = $(shell $(GO) env GOOS)
GUI_FLAGS = $(if $(filter windows,$(TARGET_OS)),-ldflags=-H=windowsgui,)
EXE = $(if $(filter windows,$(TARGET_OS)),.exe,)

.PHONY: build build-all test check
build:
	$(GO) build -buildvcs=false -trimpath $(GUI_FLAGS) -o bin/pi-manager$(EXE) .

build-all:
	GO="$(GO)" ./build.sh

test:
	$(GO) test ./...

check: test
	$(GO) vet ./...
