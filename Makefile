export GO111MODULE=on
export GOFLAGS=-mod=vendor

.PHONY: build
build: cmd/spotsh

cmd/spotsh: FORCE
	CGO_ENABLED=0 go build -o spotsh cmd/spotsh/*.go

TESTPKGS=github.com/mikeb26/spotsh/internal github.com/mikeb26/spotsh/internal/aws

.PHONY: test
test:
	go test $(TESTPKGS)

unit-tests.xml: FORCE
	aws ec2 delete-key-pair --key-name spotsh.$(AWS_REGION_NAME)
	gotestsum --junitfile unit-tests.xml $(TESTPKGS)

vendor: go.mod
	go mod download
	go mod vendor

cmd/spotsh/version.txt:
	git describe --tags > cmd/spotsh/version.txt
	truncate -s -1 cmd/spotsh/version.txt

.PHONY: clean
clean:
	rm -f spotsh unit-tests.xml

.PHONY: deps
deps:
	rm -rf go.mod go.sum vendor
	go mod init github.com/mikeb26/spotsh
	GOPROXY=direct go mod tidy
	go mod vendor

FORCE:
