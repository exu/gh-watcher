BIN ?= $(HOME)/.local/bin
VERSION ?= dev
LDFLAGS = -s -w -X main.version=$(VERSION)
SKILL_DIR ?= $(HOME)/.claude/skills/gh-watch

.PHONY: build test install install-skill clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/gh-watch .

test:
	go vet ./... && go test ./...

install: test
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BIN)/gh-watch .

install-skill:
	mkdir -p $(SKILL_DIR)
	cp skill/SKILL.md $(SKILL_DIR)/SKILL.md

clean:
	rm -rf bin
