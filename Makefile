.PHONY: build lint test clean run bench all coverage

BINARY := build/maze-of-wumpus
CMD    := ./cmd/maze-of-wumpus

all: lint test build

## build: compile the binary at build/maze-of-wumpus
build:
	@mkdir -p build
	go build -o $(BINARY) $(CMD)

## lint: verbose `go vet`
lint:
	go vet -v ./...

## test: full suite (unit + integration + e2e) in verbose mode
test:
	go test -failfast -v ./...

## coverage: produce a cross-package coverage profile and print the per-function summary
coverage:
	@mkdir -p build
	go test -coverpkg=./... -coverprofile=build/coverage.out ./...
	go tool cover -func=build/coverage.out | tail -20

## clean: delete + recreate the build/ directory
clean:
	rm -rf build
	mkdir -p build

## run: live TUI with agents navigating in parallel (one worker per agent)
run: build
	./$(BINARY) --parallel

## bench: headless serial-vs-parallel throughput benchmark, 5s, random seed
bench: build
	./$(BINARY) --bench --duration=5s
