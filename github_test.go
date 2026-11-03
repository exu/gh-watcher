package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func fakeRunner(responses map[string]string) Runner {
	return func(_ context.Context, args ...string) ([]byte, error) {
		path := args[len(args)-1]
		best := ""
		for k := range responses {
			if strings.Contains(path, k) && len(k) > len(best) {
				best = k
			}
		}
		if best == "" {
			return nil, fmt.Errorf("unexpected call %v", args)
		}
		return []byte(responses[best]), nil
	}
}

func TestResolvePRFromURL(t *testing.T) {
	o, r, n, err := ResolvePR(context.Background(), nil, "https://github.com/exu/gh-watcher/pull/12", "")
	if err != nil || o != "exu" || r != "gh-watcher" || n != 12 {
		t.Fatalf("%s %s %d %v", o, r, n, err)
	}
}

func TestResolvePRFromBranch(t *testing.T) {
	run := func(_ context.Context, args ...string) ([]byte, error) {
		return []byte(`{"url":"https://github.com/perfectscale/psc-x/pull/7"}`), nil
	}
	o, r, n, err := ResolvePR(context.Background(), run, "", "")
	if err != nil || o != "perfectscale" || r != "psc-x" || n != 7 {
		t.Fatalf("%s %s %d %v", o, r, n, err)
	}
}

func TestResolvePRError(t *testing.T) {
	run := func(_ context.Context, args ...string) ([]byte, error) { return nil, fmt.Errorf("no PR") }
	if _, _, _, err := ResolvePR(context.Background(), run, "", ""); err == nil {
		t.Fatal("want error")
	}
}

func TestSnapshotParsesAllSources(t *testing.T) {
	c := NewClient(fakeRunner(map[string]string{
		"/pulls/3/comments":  `[[{"id":11,"user":{"login":"bob","type":"User"},"body":"nit","path":"a.go","html_url":"u2"}]]`,
		"/pulls/3/reviews":   `[[{"id":21,"user":{"login":"copilot-pull-request-reviewer[bot]","type":"Bot"},"body":"r","state":"COMMENTED"}]]`,
		"/pulls/3":           `{"state":"closed","merged":true,"head":{"sha":"deadbeef"}}`,
		"/issues/3/comments": `[[{"id":1,"user":{"login":"a","type":"User"},"body":"hi","html_url":"u1"}],[{"id":2,"user":{"login":"b[bot]","type":"Bot"},"body":"yo"}]]`,
		"/check-runs":        `[{"check_runs":[{"id":5,"name":"build","status":"completed","conclusion":"success"}]},{"check_runs":[{"id":6,"name":"lint","status":"in_progress","conclusion":null}]}]`,
		"/status":            `{"statuses":[{"id":7,"context":"sonar","state":"pending"},{"id":8,"context":"jenkins","state":"failure"}]}`,
	}), "o", "r", 3)

	snap, err := c.Snapshot(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if snap.State != "merged" || snap.HeadSHA != "deadbeef" {
		t.Fatalf("pr: %+v", snap)
	}
	if len(snap.Comments) != 4 {
		t.Fatalf("comments: %d", len(snap.Comments))
	}
	bots := 0
	for _, cm := range snap.Comments {
		if cm.IsBot {
			bots++
		}
	}
	if bots != 2 {
		t.Fatalf("bots: %d", bots)
	}
	done := 0
	for _, ch := range snap.Checks {
		if ch.Completed {
			done++
		}
	}
	if len(snap.Checks) != 4 || done != 2 {
		t.Fatalf("checks: %+v", snap.Checks)
	}
}
