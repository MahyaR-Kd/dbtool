VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS  := -ldflags "-X dbtool/cmd.Version=$(VERSION)"
BINARY   := dbtool

.PHONY: build install clean

build:
	go build $(LDFLAGS) -o $(BINARY) .

install:
	go install $(LDFLAGS) .

clean:
	rm -f $(BINARY)
