.PHONY: build install run test clean

BINARY := gh-zen

build:
	go build -o $(BINARY)

install: build
	gh extension install .

run:
	go run .

test:
	go test ./...

clean:
	rm -f $(BINARY)
