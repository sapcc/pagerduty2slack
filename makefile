IMAGE   ?= sapcc/pagerduty2slack
VERSION = $(shell git rev-parse --verify HEAD | head -c 8)
GOOS    ?= $(shell go env | grep GOOS | cut -d'"' -f2)
BINARY  := pagerduty2slack

LDFLAGS := -X github.com/sapcc/pagerduty2slack/pagerduty2slack.VERSION=$(VERSION)
GOFLAGS := -ldflags "$(LDFLAGS)"

SRCDIRS  := .
PACKAGES := $(shell find $(SRCDIRS) -type d)
GOFILES  := $(addsuffix /*.go,$(PACKAGES))
GOFILES  := $(wildcard $(GOFILES))

#GLIDE := $(shell command -v glide 2> /dev/null)

.PHONY: all clean vendor tests static-check

all: test build

build:
	bin/$(GOOS)/$(BINARY)
	bin/%/$(BINARY): $(GOFILES) Makefile
		GOOS=$* GOARCH=amd64 go build $(GOFLAGS) -v -i -o bin/$*/$(BINARY) .

build-easy:
	env GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -v -i -o bin/$(BINARY) ./cmd

run:
	go -o $(BINARY_NAME) -v ./...
	./$(BINARY_NAME)

static-check:
	@if s="$$(gofmt -s -l *.go pkg 2>/dev/null)"                            && test -n "$$s"; then printf ' => %s\n%s\n' gofmt  "$$s"; false; fi
	@if s="$$(golint . && find pkg -type d -exec golint {} \; 2>/dev/null)" && test -n "$$s"; then printf ' => %s\n%s\n' golint "$$s"; false; fi

tests: build static-check
	go test -v github.com/sapcc/ldap2slack/...

docker-build: tests bin/linux/$(BINARY)
	docker build -t $(IMAGE):$(VERSION) .

docker-push: docker-build
	docker push $(IMAGE):$(VERSION)

clean:
	rm -rf bin/*

vendor:
	dep ensure

FILES = bin/$(BINARY) _run_config.yml
copy: build-easy
	#scp -i ~/.ssh/id_rsa bin/$(BINARY) ccloud@10.47.41.39:/home/ccloud/ldap2slack/
	scp -i ~/.ssh/id_rsa $(FILES) ccloud@10.47.41.39:/home/ccloud/pagerduty2slack/