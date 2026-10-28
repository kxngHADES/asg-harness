VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PREFIX  ?= $(HOME)/.local

.PHONY: build install uninstall test lint run dashboard snapshot clean

build:            ## build ./asg
	go build -trimpath -ldflags "$(LDFLAGS)" -o asg ./cmd/asg

install: build    ## install to $(PREFIX)/bin
	install -d $(PREFIX)/bin
	install -m 755 asg $(PREFIX)/bin/asg

uninstall:
	rm -f $(PREFIX)/bin/asg

test:
	go test ./...

lint:
	gofmt -l . | (! grep .) && go vet ./...

run: build        ## terminal agent
	./asg

dashboard: build  ## web version
	./asg dashboard

snapshot:         ## cross-compile every platform into dist/ (needs goreleaser)
	goreleaser release --snapshot --clean

clean:
	rm -rf asg dist
