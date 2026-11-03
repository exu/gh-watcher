package main

import (
	"strings"
	"testing"
)

func lines(evs []Event) []string {
	var out []string
	for _, e := range evs {
		out = append(out, e.String())
	}
	return out
}

func open(sha string, cs []Comment, ch []Check) Snapshot {
	return Snapshot{State: "open", HeadSHA: sha, Comments: cs, Checks: ch}
}

func TestBaselineIsSilentThenNewCommentReported(t *testing.T) {
	st := NewState()
	old := Comment{Kind: "comment", ID: 1, Author: "alice", Body: "old"}
	if evs := Diff(st, open("abc", []Comment{old}, nil), Options{Bots: "copilot"}); len(evs) != 0 {
		t.Fatalf("baseline emitted %v", lines(evs))
	}
	nw := Comment{Kind: "comment", ID: 2, Author: "bob", Body: "fix\n this", URL: "http://x"}
	got := lines(Diff(st, open("abc", []Comment{old, nw}, nil), Options{Bots: "copilot"}))
	want := "comment bob: fix this | http://x"
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v want %q", got, want)
	}
	if evs := Diff(st, open("abc", []Comment{old, nw}, nil), Options{Bots: "copilot"}); len(evs) != 0 {
		t.Fatalf("duplicate emitted %v", lines(evs))
	}
}

func TestFromStartReportsExisting(t *testing.T) {
	st := NewState()
	c := Comment{Kind: "comment", ID: 1, Author: "alice", Body: "hi"}
	if evs := Diff(st, open("abc", []Comment{c}, nil), Options{Bots: "all", FromStart: true}); len(evs) != 1 {
		t.Fatalf("got %v", lines(evs))
	}
}

func TestBotFilter(t *testing.T) {
	cases := []struct {
		bots   string
		author string
		isBot  bool
		want   int
	}{
		{"copilot", "sonarcloud[bot]", true, 0},
		{"copilot", "copilot-pull-request-reviewer[bot]", true, 1},
		{"copilot", "alice", false, 1},
		{"none", "copilot-pull-request-reviewer[bot]", true, 0},
		{"all", "sonarcloud[bot]", true, 1},
	}
	for _, tc := range cases {
		st := NewState()
		Diff(st, open("a", nil, nil), Options{Bots: tc.bots})
		c := Comment{Kind: "comment", ID: 5, Author: tc.author, IsBot: tc.isBot, Body: "x"}
		if got := len(Diff(st, open("a", []Comment{c}, nil), Options{Bots: tc.bots})); got != tc.want {
			t.Errorf("bots=%s author=%s got %d want %d", tc.bots, tc.author, got, tc.want)
		}
	}
}

func TestEmptyCommentedReviewSkippedButStateReviewsReported(t *testing.T) {
	st := NewState()
	Diff(st, open("a", nil, nil), Options{Bots: "all"})
	cs := []Comment{
		{Kind: "review", ID: 1, Author: "a", State: "COMMENTED"},
		{Kind: "review", ID: 2, Author: "b", State: "CHANGES_REQUESTED", Body: "no"},
	}
	got := lines(Diff(st, open("a", cs, nil), Options{Bots: "all"}))
	if len(got) != 1 || !strings.HasPrefix(got[0], "review b CHANGES_REQUESTED: no") {
		t.Fatalf("got %v", got)
	}
}

func TestReviewCommentIncludesPath(t *testing.T) {
	st := NewState()
	Diff(st, open("a", nil, nil), Options{Bots: "all"})
	c := Comment{Kind: "review-comment", ID: 9, Author: "a", Path: "x.go", Body: "nit"}
	got := lines(Diff(st, open("a", []Comment{c}, nil), Options{Bots: "all"}))
	if len(got) != 1 || got[0] != "review-comment a x.go: nit" {
		t.Fatalf("got %v", got)
	}
}

func TestLongBodyTruncated(t *testing.T) {
	if got := oneLine(strings.Repeat("a", 500)); len([]rune(got)) != maxText+3 {
		t.Fatalf("len %d", len(got))
	}
}

func TestCheckCompletionEmittedOnceAndRerunReported(t *testing.T) {
	st := NewState()
	Diff(st, open("a", nil, []Check{{ID: "c1", Name: "build"}}), Options{Bots: "all"})
	done := []Check{{ID: "c1", Name: "build", Completed: true, Conclusion: "failure"}}
	got := lines(Diff(st, open("a", nil, done), Options{Bots: "all"}))
	if len(got) != 1 || got[0] != "ci build failure" {
		t.Fatalf("got %v", got)
	}
	if evs := Diff(st, open("a", nil, done), Options{Bots: "all"}); len(filterKind(evs, "ci")) != 0 {
		t.Fatalf("repeat emitted %v", lines(evs))
	}
	rerun := append(done, Check{ID: "c2", Name: "build", Completed: true, Conclusion: "timed_out"})
	got = lines(filterKind(Diff(st, open("a", nil, rerun), Options{Bots: "all"}), "ci"))
	if len(got) != 1 || got[0] != "ci build timed_out" {
		t.Fatalf("rerun got %v", got)
	}
}

func filterKind(evs []Event, kind string) []Event {
	var out []Event
	for _, e := range evs {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

func TestSummaryNeedsTwoStablePolls(t *testing.T) {
	st := NewState()
	Diff(st, open("abcdef0123", nil, []Check{{ID: "c1", Name: "a"}}), Options{Bots: "all"})
	all := []Check{{ID: "c1", Name: "a", Completed: true, Conclusion: "success"}, {ID: "c2", Name: "b", Completed: true, Conclusion: "failure"}}
	if evs := filterKind(Diff(st, open("abcdef0123", nil, all), Options{Bots: "all"}), "ci-summary"); len(evs) != 0 {
		t.Fatalf("summary on first poll: %v", lines(evs))
	}
	got := lines(filterKind(Diff(st, open("abcdef0123", nil, all), Options{Bots: "all"}), "ci-summary"))
	if len(got) != 1 || got[0] != "ci-summary abcdef0 failure (1 failed, 2 total)" {
		t.Fatalf("got %v", got)
	}
	if evs := filterKind(Diff(st, open("abcdef0123", nil, all), Options{Bots: "all"}), "ci-summary"); len(evs) != 0 {
		t.Fatalf("summary repeated: %v", lines(evs))
	}
}

func TestSummarySuccessAndPendingResets(t *testing.T) {
	st := NewState()
	Diff(st, open("sha1234567", nil, []Check{{ID: "c1", Name: "a"}}), Options{Bots: "all"})
	done := []Check{{ID: "c1", Name: "a", Completed: true, Conclusion: "success"}}
	Diff(st, open("sha1234567", nil, done), Options{Bots: "all"})
	running := []Check{{ID: "c1", Name: "a", Completed: true, Conclusion: "success"}, {ID: "c2", Name: "b"}}
	Diff(st, open("sha1234567", nil, running), Options{Bots: "all"})
	if st.SummaryPending != "" {
		t.Fatalf("pending not reset: %q", st.SummaryPending)
	}
}

func TestBaselineSwallowsFinishedCI(t *testing.T) {
	st := NewState()
	done := []Check{{ID: "c1", Name: "a", Completed: true, Conclusion: "success"}}
	for i := range 3 {
		if evs := Diff(st, open("s", nil, done), Options{Bots: "all"}); len(evs) != 0 {
			t.Fatalf("poll %d emitted %v", i, lines(evs))
		}
	}
}

func TestStateEvent(t *testing.T) {
	st := NewState()
	got := lines(Diff(st, Snapshot{State: "merged", HeadSHA: "a"}, Options{Bots: "all"}))
	if len(got) != 1 || got[0] != "state merged" {
		t.Fatalf("got %v", got)
	}
}

func TestPassingAndSkippedChecksAreSilent(t *testing.T) {
	st := NewState()
	Diff(st, open("a", nil, []Check{{ID: "c1", Name: "a"}, {ID: "c2", Name: "b"}}), Options{Bots: "all"})
	done := []Check{{ID: "c1", Name: "a", Completed: true, Conclusion: "success"}, {ID: "c2", Name: "b", Completed: true, Conclusion: "skipped"}}
	if evs := filterKind(Diff(st, open("a", nil, done), Options{Bots: "all"}), "ci"); len(evs) != 0 {
		t.Fatalf("got %v", lines(evs))
	}
}
