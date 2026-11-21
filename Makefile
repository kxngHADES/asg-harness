VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)
PREFIX  ?= $(HOME)/.local

.PHONY: build install uninstall test lint run dashboard desktop desktop-test desktop-package setup-preview snapshot clean

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

desktop:          ## desktop app (separate module in desktop/)
	cd desktop && go build -tags desktop,production -ldflags "-s -w $(if $(filter Windows_NT,$(OS)),-H windowsgui)" -o asg-desktop$(if $(filter Windows_NT,$(OS)),.exe) .

desktop-package:  ## release archive for this OS into dist-desktop/ (what CI ships)
	bash desktop/build/package.sh $(if $(filter Windows_NT,$(OS)),windows amd64,$(if $(filter Darwin,$(shell uname -s)),darwin universal,linux amd64)) $(VERSION) dist-desktop

desktop-test:
	cd desktop && gofmt -l . | (! grep .) && go vet -tags desktop,production . ./installer && go test -tags desktop,production . ./installer

setup-preview:    ## the setup wizard without a payload (walks through every screen, installs nothing)
	cd desktop && go build -tags desktop,production -ldflags "-s -w $(if $(filter Windows_NT,$(OS)),-H windowsgui)" -o asg-setup$(if $(filter Windows_NT,$(OS)),.exe) ./installer

snapshot:         ## cross-compile every platform into dist/ (needs goreleaser)
	goreleaser release --snapshot --clean

clean:
	rm -rf asg dist dist-desktop desktop/asg-desktop desktop/asg-desktop.exe desktop/devserve.exe desktop/asg-setup desktop/asg-setup.exe
