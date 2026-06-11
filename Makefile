.PHONY: build install run test bench clean

BINARY := gh-zen

build:
	go build -o $(BINARY)

install: build
	gh extension install .

run:
	go run .

test:
	go test ./...

# Run the performance benchmarks (launch + screen-transition timings and the
# Update/View hot paths). Offline: the GitHub fetch is stubbed, so no network or
# `gh` is needed. -run='^$$' skips the normal tests; -benchmem reports allocs.
bench:
	go test -bench=. -benchmem -run='^$$' ./...

clean:
	rm -f $(BINARY)
