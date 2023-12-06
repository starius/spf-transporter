COMMIT=$(shell git rev-parse HEAD)
DATE=$(shell date)
VERSION=$(shell git describe --tags --abbrev=0)

vendor:
	go mod vendor

lint:
	go fmt -mod=vendor ./...
	go vet -mod=vendor ./...
	GOGC=1 golangci-lint run --timeout=10m ./...

unit_test:
	go test -v -mod=vendor ./...
