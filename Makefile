.PHONY: build test vet install uninstall clean

# The scripts own the build and install logic; the Makefile just calls them.
build:
	./build.sh

test:
	go test ./...

vet:
	@test -z "$$(gofmt -l .)" || { echo "unformatted:"; gofmt -l .; exit 1; }
	go vet ./...

install:
	./install.sh

uninstall:
	./uninstall.sh

clean:
	rm -rf psl dist
