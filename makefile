IMAGE := keppel.eu-de-1.cloud.sap/ccloud/pagerduty2slack
VERSION_LATEST := latest
VERSION := v0.0.1

GOOS    ?= $(shell go env | grep GOOS | cut -d'"' -f2)
BINARY  := pagerduty2slack

LDFLAGS := -X github.com/sapcc/pagerduty2slack/pagerduty2slack.VERSION=$(VERSION)
GOFLAGS := -ldflags "$(LDFLAGS)"

#SRCDIRS  := cmd internal
#PACKAGES := $(shell find $(SRCDIRS) -type d)
#GOFILES  := $(addsuffix /*.go,$(PACKAGES))
#GOFILES  := $(wildcard $(GOFILES))
#GOPROJ := $(shell pwd)

#GOBASE := /Users/d072896/_git_dev/go
#GOPATH := $(GOPROJ)/internal/:$(GOPROJ)/internal/clients/:$(GOBASE)/pkg/:$(GOBASE)/src/:$(GOBASE):$(GOROOT)/pkg/:$(GOROOT)/src/
#GOBIN  := $(GOBASE)/bin

build:
	env GOOS=${GOOS} GOARCH=amd64 go build $(GOFLAGS) -v -o bin/$(BINARY) ./cmd

build-linux:
	env GOOS=linux GOARCH=amd64 go build $(GOFLAGS) -v -o bin/linux/$(BINARY) ./cmd

run:
	go -o $(BINARY_NAME) -v ./...
	./$(BINARY_NAME)make co

static-check:
	@if s="$$(gofmt -s -l *.go pkg 2>/dev/null)"                            && test -n "$$s"; then printf ' => %s\n%s\n' gofmt  "$$s"; false; fi
	@if s="$$(golint . && find pkg -type d -exec golint {} \; 2>/dev/null)" && test -n "$$s"; then printf ' => %s\n%s\n' golint "$$s"; false; fi

tests: build static-check
	go test -v github.com/sapcc/pagerduty2slack/...


docker-build:
	docker build -t $(IMAGE):$(VERSION) .

docker-push: docker-build
	docker push $(IMAGE):$(VERSION)
	docker push $(IMAGE):$(VERSION_LATEST)

clean:
	rm -rf bin/*

vendor:
	dep ensure

FILES = bin/linux/$(BINARY) _run_config.yml
copy: build-linux
	#scp -i ~/.ssh/id_rsa bin/$(BINARY) ccloud@10.47.41.39:/home/ccloud/ldap2slack/
	scp -i ~/.ssh/id_rsa $(FILES) ccloud@10.47.41.39:/home/ccloud/pagerduty2slack