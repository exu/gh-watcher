BIN ?= $(HOME)/.local/bin
SKILL_DIR ?= $(HOME)/.claude/skills/gh-watch

.PHONY: build test install install-skill clean

build:
	go build -o bin/gh-watch .

test:
	go vet ./... && go test ./...

install: test
	go build -o $(BIN)/gh-watch .

install-skill:
	mkdir -p $(SKILL_DIR)
	cp skill/SKILL.md $(SKILL_DIR)/SKILL.md

clean:
	rm -rf bin
