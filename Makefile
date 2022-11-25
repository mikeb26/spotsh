export GO111MODULE=on
export GOFLAGS=-mod=vendor

.PHONY: build
build: cmd/spotsh

cmd/spotsh: FORCE
	go build -o spotsh cmd/spotsh/main.go

.PHONY: test
test:
	cd internal; go test
	cd internal/aws; go test

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
