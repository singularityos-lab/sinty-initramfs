BIN := sinty-initramfs

.PHONY: all build static test vet clean

all: build

build:
	go build -o $(BIN) ./cmd/sinty-initramfs

static:
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o $(BIN) ./cmd/sinty-initramfs

test:
	go test ./...

vet:
	go vet ./...

clean:
	rm -f $(BIN)
