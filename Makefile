CGO_CFLAGS = -DSQLITE_ENABLE_FTS5
CGO_LDFLAGS = -lm

export CGO_CFLAGS CGO_LDFLAGS

.PHONY: build test run clean

build:
	go build -o bin/noteboard ./cmd/noteboard/

test:
	go test ./...

run: build
	./bin/noteboard

clean:
	rm -rf bin/
