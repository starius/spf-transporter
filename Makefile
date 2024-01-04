.EXPORT_ALL_VARIABLES:

COMMIT=$(shell git rev-parse HEAD)
DATE=$(shell date)
VERSION=$(shell git describe --tags --abbrev=0)

POSTGRES_CONFIG=$(shell realpath ./transporterdb/integration_test/postgres.toml)

vendor:
	go mod vendor

lint:
	go fmt -mod=vendor ./...
	go vet -mod=vendor ./...
	GOGC=1 golangci-lint run --timeout=10m ./...

unit_tests:
	go test -v -mod=vendor ./...

integration_tests:
	go test -v -mod=vendor -run Integration -timeout=20m -tags='integration_test' ./... -p 1
