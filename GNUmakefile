default: build

build:
	go build -v ./...

test:
	go test -v ./...

testacc:
	TF_ACC=1 go test -v ./...

lint:
	golangci-lint run

test-all: lint test testacc

release:
	goreleaser release --clean

docs:
	go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs@latest generate --provider-name grafanasilence

generate: docs

clean:
	rm -rf dist/

.PHONY: build test testacc lint test-all release docs generate clean
