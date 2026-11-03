package main

import (
	"fmt"
	"sort"
	"strings"
)

type Comment struct {
	Kind   string // comment | review-comment | review
	ID     int64
	Author string
	IsBot  bool
	Body   string
	Path   string
	URL    string
	State  string // review state, reviews only
}

type Check struct {
	ID         string
	Name       string
	Completed  bool
	Conclusion string
}

type Snapshot struct {
	State    string // open | closed | merged
	HeadSHA  string
	Comments []Comment
	Checks   []Check
}

type Event struct{ Kind, Text string }

func (e Event) String() string { return e.Kind + " " + e.Text }

type State struct {
	Initialized    bool
	LastID         map[string]int64
	Emitted        map[string]bool
	SummaryDone    string
	SummaryPending string
}

type Options struct {
	Bots      string // none | copilot | all
	FromStart bool
}

func NewState() *State {
	return &State{LastID: map[string]int64{}, Emitted: map[string]bool{}}
}

const maxText = 200

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > maxText {
		return string(r[:maxText]) + "..."
	}
	return s
}

func (o Options) allowed(c Comment) bool {
	switch o.Bots {
	case "all":
		return true
	case "none":
		return !c.IsBot
	default:
		return !c.IsBot || strings.Contains(strings.ToLower(c.Author), "copilot")
	}
}

func failed(conclusion string) bool {
	switch conclusion {
	case "failure", "timed_out", "cancelled", "action_required", "error", "startup_failure":
		return true
	}
	return false
}

func summaryKey(sha string, checks []Check) string {
	ids := make([]string, 0, len(checks))
	for _, c := range checks {
		if !c.Completed {
			return ""
		}
		ids = append(ids, c.ID)
	}
	if len(ids) == 0 {
		return ""
	}
	sort.Strings(ids)
	return sha + ":" + strings.Join(ids, ",")
}

func commentEvent(c Comment) (Event, bool) {
	if c.Kind == "review" && c.State == "COMMENTED" && strings.TrimSpace(c.Body) == "" {
		return Event{}, false
	}
	who := c.Author
	kind := c.Kind
	if c.Kind == "review" {
		who = fmt.Sprintf("%s %s", c.Author, c.State)
	}
	if c.Path != "" {
		who = fmt.Sprintf("%s %s", who, c.Path)
	}
	text := who
	if b := oneLine(c.Body); b != "" {
		text += ": " + b
	}
	if c.URL != "" {
		text += " | " + c.URL
	}
	return Event{kind, text}, true
}

// Diff returns events new since st and updates st in place.
func Diff(st *State, snap Snapshot, opt Options) []Event {
	var events []Event
	baseline := !st.Initialized && !opt.FromStart
	st.Initialized = true

	sort.SliceStable(snap.Comments, func(i, j int) bool { return snap.Comments[i].ID < snap.Comments[j].ID })
	for _, c := range snap.Comments {
		if c.ID <= st.LastID[c.Kind] {
			continue
		}
		st.LastID[c.Kind] = c.ID
		if baseline || !opt.allowed(c) {
			continue
		}
		if ev, ok := commentEvent(c); ok {
			events = append(events, ev)
		}
	}

	for _, c := range snap.Checks {
		if !c.Completed || st.Emitted[c.ID] {
			continue
		}
		st.Emitted[c.ID] = true
		if baseline {
			continue
		}
		if failed(c.Conclusion) {
			events = append(events, Event{"ci", fmt.Sprintf("%s %s", c.Name, c.Conclusion)})
		}
	}

	events = append(events, summary(st, snap, baseline)...)

	if snap.State != "open" {
		events = append(events, Event{"state", snap.State})
	}
	return events
}

func summary(st *State, snap Snapshot, baseline bool) []Event {
	key := summaryKey(snap.HeadSHA, snap.Checks)
	if key == "" || key == st.SummaryDone {
		st.SummaryPending = ""
		return nil
	}
	if baseline {
		st.SummaryDone = key
		return nil
	}
	// require two consecutive identical complete polls to avoid early summaries
	if key != st.SummaryPending {
		st.SummaryPending = key
		return nil
	}
	st.SummaryDone, st.SummaryPending = key, ""
	bad := 0
	for _, c := range snap.Checks {
		if failed(c.Conclusion) {
			bad++
		}
	}
	sha := snap.HeadSHA
	if len(sha) > 7 {
		sha = sha[:7]
	}
	if bad > 0 {
		return []Event{{"ci-summary", fmt.Sprintf("%s failure (%d failed, %d total)", sha, bad, len(snap.Checks))}}
	}
	return []Event{{"ci-summary", fmt.Sprintf("%s success (%d total)", sha, len(snap.Checks))}}
}
