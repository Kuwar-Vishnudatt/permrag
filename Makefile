.PHONY: build test fmt vet clean demo

build:
	go build -o bin/permrag ./cmd/permrag

test:
	go test ./... -count=1

test-race:
	go test ./... -race -count=1

fmt:
	gofmt -l .

vet:
	go vet ./...

demo: build
	./scripts/run-demo.sh

clean:
	rm -rf bin/ *.db
