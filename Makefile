# skills-check uses process substitution, which /bin/sh does not provide.
SHELL := /bin/bash

.PHONY: all test lint fmt fmt-check vet tidy clean skills-check

all: fmt-check vet test skills-check

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

# The Claude and Cursor skill trees are duplicated because Cursor does not
# reliably follow symlinks. Duplication without enforcement is just drift with
# extra steps, so fail when they diverge. The only sanctioned difference is
# each review/SKILL.md's one-line pointer to its counterpart.
skills-check:
	@status=0; \
	for name in feature bugfix review; do \
		if ! diff -r -x SKILL.md ".claude/skills/$$name" ".cursor/skills/$$name" >/dev/null 2>&1; then \
			echo "skills drift in $$name references:"; \
			diff -r -x SKILL.md ".claude/skills/$$name" ".cursor/skills/$$name"; \
			status=1; \
		fi; \
		if ! diff <(grep -v 'counterpart is' ".claude/skills/$$name/SKILL.md") \
		          <(grep -v 'counterpart is' ".cursor/skills/$$name/SKILL.md") >/dev/null 2>&1; then \
			echo "skills drift in $$name/SKILL.md:"; \
			diff <(grep -v 'counterpart is' ".claude/skills/$$name/SKILL.md") \
			     <(grep -v 'counterpart is' ".cursor/skills/$$name/SKILL.md"); \
			status=1; \
		fi; \
	done; \
	if [ $$status -eq 0 ]; then echo "skills: .claude and .cursor in sync"; fi; \
	exit $$status

tidy:
	go mod tidy

# Rewrite the canonical-JSON golden file after a deliberate schema change.
golden:
	UPDATE_GOLDEN=1 go test ./internal/spec/ -run TestGoldenCanonicalJSON

clean:
	rm -f coverage.out
