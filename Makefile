.PHONY: build test run clean

build:
	go build -o bin/devmem ./cmd/devmem

test:
	go test ./... -v -count=1

run: build
	./bin/devmem

clean:
	rm -rf bin/
