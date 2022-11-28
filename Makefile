export GO111MODULE=on
export GOFLAGS=-mod=vendor

.PHONY: build
build: cmd/spotsh

cmd/spotsh: FORCE
	go build -o spotsh cmd/spotsh/main.go

TESTPKGS=github.com/mikeb26/spotsh/internal github.com/mikeb26/spotsh/internal/aws

.PHONY: test
test:
	go test $(TESTPKGS)

unit-tests.xml: FORCE
	aws ec2 delete-key-pair --key-name $(AWS_REGION)
	gotestsum --junitfile unit-tests.xml $(TESTPKGS)

vendor: go.mod
	go mod download
	go mod vendor

version.txt:
	git describe --tags > version.txt
	truncate -s -1 version.txt

.PHONY: clean
clean:
	rm -f spotsh

.PHONY: deps
deps:
	rm -rf go.mod go.sum vendor
	go mod init github.com/mikeb26/spotsh
	GOPROXY=direct go mod tidy
	go mod vendor

FORCE:
