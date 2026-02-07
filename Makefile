.PHONY: build test-unit test-integration test-e2e test-all lint clean

build:
	go build -o bin/blueflame ./cmd/blueflame/

test-unit:
	go test -short -race ./internal/...

test-integration:
	go test -run Integration -race ./internal/...

test-e2e:
	go test -run E2E -count=1 ./test/e2e/...

test-all: test-unit test-integration

lint:
	golangci-lint run ./...

clean:
	rm -rf bin/
