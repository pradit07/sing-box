NAME = sing-box
COMMIT = $(shell git rev-parse --short HEAD)
TAGS ?= with_gvisor,with_quic,with_wireguard,with_utls,with_clash_api
TAGS_TEST ?= with_gvisor,with_quic,with_wireguard,with_grpc,with_ech,with_utls,with_shadowsocksr
PARAMS = -v -trimpath -tags "$(TAGS)" -ldflags "-s -w -buildid="
MAIN = ./cmd/sing-box
DIST = ./dist

.PHONY: test release

build:
	go build $(PARAMS) $(MAIN)

linux-amd64:
	GOOS=linux GOARCH=amd64 go build $(PARAMS) -o $(DIST)/$(NAME)-linux-amd64 $(MAIN)

linux-arm64:
	GOOS=linux GOARCH=arm64 go build $(PARAMS) -o $(DIST)/$(NAME)-linux-arm64 $(MAIN)

darwin-amd64:
	GOOS=darwin GOARCH=amd64 go build $(PARAMS) -o $(DIST)/$(NAME)-darwin-amd64 $(MAIN)

darwin-arm64:
	GOOS=darwin GOARCH=arm64 go build $(PARAMS) -o $(DIST)/$(NAME)-darwin-arm64 $(MAIN)

windows-amd64:
	GOOS=windows GOARCH=amd64 go build $(PARAMS) -o $(DIST)/$(NAME)-windows-amd64.exe $(MAIN)

install:
	go install $(PARAMS) $(MAIN)

fmt:
	@gofumpt -l -w .
	@gofmt -s -w .
	@gci write -s "standard,prefix(github.com/sagernet/),default" .

fmt_install:
	go install -v mvdan.cc/gofumpt@latest
	go install -v github.com/daixiang0/gci@v0.4.0

lint:
	GOOS=linux golangci-lint run ./...
	GOOS=android golangci-lint run ./...
	GOOS=windows golangci-lint run ./...
	GOOS=darwin golangci-lint run ./...
	GOOS=freebsd golangci-lint run ./...

lint_install:
	go install -v github.com/golangci/golangci-lint/cmd/golangci-lint@latest

proto:
	@go run ./cmd/internal/protogen
	@gofumpt -l -w .
	@gofumpt -l -w .

proto_install:
	go install -v google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install -v google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

snapshot:
	go run ./cmd/internal/build goreleaser release --rm-dist --snapshot || exit 1
	mkdir dist/release
	mv dist/*.tar.gz dist/*.zip dist/*.deb dist/*.rpm dist/release
	ghr --delete --draft --prerelease -p 1 nightly dist/release
	rm -r dist

release:
	go run ./cmd/internal/build goreleaser release --rm-dist --skip-publish || exit 1
	mkdir dist/release
	mv dist/*.tar.gz dist/*.zip dist/*.deb dist/*.rpm dist/release
	ghr --delete --draft --prerelease -p 3 $(shell git describe --tags) dist/release
	rm -r dist

release_install:
	go install -v github.com/goreleaser/goreleaser@latest
	go install -v github.com/tcnksm/ghr@latest

test:
	@go test -v ./... && \
	cd test && \
	go mod tidy && \
	go test -v -tags "$(TAGS_TEST)" .

test_stdio:
	@go test -v ./... && \
	cd test && \
	go mod tidy && \
	go test -v -tags "$(TAGS_TEST),force_stdio" .

clean:
	rm -rf bin dist sing-box
	rm -f $(shell go env GOPATH)/sing-box

update:
	git fetch
	git reset FETCH_HEAD --hard
	git clean -fdx