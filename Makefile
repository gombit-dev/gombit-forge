.PHONY: all test lint fmt fmt-check vet tidy clean

all: fmt-check vet test

test:
	go test ./... -count=1

race:
	go test ./... -race -count=1

cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -1

fmt:
	gofmt -w .

# CI gate: fail when anything is unformatted.
fmt-check:
	@unformatted=$$(gofmt -l .); \
	if [ -n "$$unformatted" ]; then \
		echo "unformatted files:"; echo "$$unformatted"; exit 1; \
	fi

vet:
	go vet ./...

tidy:
	go mod tidy

# Rewrite the canonical-JSON golden file after a deliberate schema change.
golden:
	UPDATE_GOLDEN=1 go test ./internal/spec/ -run TestGoldenCanonicalJSON

clean:
	rm -f coverage.out
