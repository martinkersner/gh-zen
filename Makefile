.PHONY: build install run clean

BINARY := gh-zen

build:
	go build -o $(BINARY)

install: build
	gh extension install .

run:
	go run .

clean:
	rm -f $(BINARY)
