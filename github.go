package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

type Runner func(ctx context.Context, args ...string) ([]byte, error)

func ghRun(ctx context.Context, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out, nil
}

type Client struct {
	run     Runner
	Owner   string
	Repo    string
	Number  int
	apiBase string
}

func NewClient(run Runner, owner, repo string, n int) *Client {
	return &Client{run: run, Owner: owner, Repo: repo, Number: n, apiBase: fmt.Sprintf("repos/%s/%s", owner, repo)}
}

var prURL = regexp.MustCompile(`github\.com/([^/]+)/([^/]+)/pull/(\d+)`)

// ResolvePR finds owner/repo/number from a PR URL, number, or the current branch.
func ResolvePR(ctx context.Context, run Runner, arg, repoFlag string) (string, string, int, error) {
	if m := prURL.FindStringSubmatch(arg); m != nil {
		n, _ := strconv.Atoi(m[3])
		return m[1], m[2], n, nil
	}
	args := []string{"pr", "view"}
	if arg != "" {
		args = append(args, arg)
	}
	if repoFlag != "" {
		args = append(args, "-R", repoFlag)
	}
	args = append(args, "--json", "url")
	out, err := run(ctx, args...)
	if err != nil {
		return "", "", 0, fmt.Errorf("resolve PR (pass a number or URL?): %w", err)
	}
	var v struct{ URL string }
	if err := json.Unmarshal(out, &v); err != nil {
		return "", "", 0, err
	}
	m := prURL.FindStringSubmatch(v.URL)
	if m == nil {
		return "", "", 0, fmt.Errorf("unexpected PR url %q", v.URL)
	}
	n, _ := strconv.Atoi(m[3])
	return m[1], m[2], n, nil
}

type user struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

func (u user) bot() bool { return u.Type == "Bot" || strings.HasSuffix(u.Login, "[bot]") }

type rawComment struct {
	ID      int64  `json:"id"`
	User    user   `json:"user"`
	Body    string `json:"body"`
	Path    string `json:"path"`
	HTMLURL string `json:"html_url"`
	State   string `json:"state"`
}

func (c *Client) get(ctx context.Context, path string, slurp bool) ([]byte, error) {
	args := []string{"api"}
	if slurp {
		args = append(args, "--paginate", "--slurp")
	}
	return c.run(ctx, append(args, path)...)
}

// pages decodes the --slurp output ([page, page, ...]) into one slice of pages.
func pages[T any](raw []byte) ([]T, error) {
	var ps []T
	if err := json.Unmarshal(raw, &ps); err != nil {
		return nil, err
	}
	return ps, nil
}

func (c *Client) comments(ctx context.Context, kind, path string) ([]Comment, error) {
	raw, err := c.get(ctx, path+"?per_page=100", true)
	if err != nil {
		return nil, err
	}
	ps, err := pages[[]rawComment](raw)
	if err != nil {
		return nil, fmt.Errorf("decode %s: %w", kind, err)
	}
	var out []Comment
	for _, p := range ps {
		for _, r := range p {
			out = append(out, Comment{Kind: kind, ID: r.ID, Author: r.User.Login, IsBot: r.User.bot(), Body: r.Body, Path: r.Path, URL: r.HTMLURL, State: r.State})
		}
	}
	return out, nil
}

func (c *Client) checkRuns(ctx context.Context, sha string) ([]Check, error) {
	raw, err := c.get(ctx, fmt.Sprintf("%s/commits/%s/check-runs?per_page=100", c.apiBase, sha), true)
	if err != nil {
		return nil, err
	}
	type run struct {
		ID         int64  `json:"id"`
		Name       string `json:"name"`
		Status     string `json:"status"`
		Conclusion string `json:"conclusion"`
	}
	ps, err := pages[struct {
		CheckRuns []run `json:"check_runs"`
	}](raw)
	if err != nil {
		return nil, fmt.Errorf("decode check-runs: %w", err)
	}
	var out []Check
	for _, p := range ps {
		for _, r := range p.CheckRuns {
			out = append(out, Check{ID: "c" + strconv.FormatInt(r.ID, 10), Name: r.Name, Completed: r.Status == "completed", Conclusion: r.Conclusion})
		}
	}
	return out, nil
}

func (c *Client) statuses(ctx context.Context, sha string) ([]Check, error) {
	raw, err := c.get(ctx, fmt.Sprintf("%s/commits/%s/status?per_page=100", c.apiBase, sha), false)
	if err != nil {
		return nil, err
	}
	var v struct {
		Statuses []struct {
			ID      int64  `json:"id"`
			Context string `json:"context"`
			State   string `json:"state"`
		} `json:"statuses"`
	}
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, fmt.Errorf("decode status: %w", err)
	}
	var out []Check
	for _, s := range v.Statuses {
		out = append(out, Check{ID: "s" + strconv.FormatInt(s.ID, 10), Name: s.Context, Completed: s.State != "pending", Conclusion: s.State})
	}
	return out, nil
}

func (c *Client) Snapshot(ctx context.Context) (Snapshot, error) {
	var snap Snapshot
	raw, err := c.get(ctx, fmt.Sprintf("%s/pulls/%d", c.apiBase, c.Number), false)
	if err != nil {
		return snap, err
	}
	var pr struct {
		State  string `json:"state"`
		Merged bool   `json:"merged"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	}
	if err := json.Unmarshal(raw, &pr); err != nil {
		return snap, fmt.Errorf("decode pr: %w", err)
	}
	snap.State, snap.HeadSHA = pr.State, pr.Head.SHA
	if pr.Merged {
		snap.State = "merged"
	}

	sources := []struct{ kind, path string }{
		{"comment", fmt.Sprintf("%s/issues/%d/comments", c.apiBase, c.Number)},
		{"review-comment", fmt.Sprintf("%s/pulls/%d/comments", c.apiBase, c.Number)},
		{"review", fmt.Sprintf("%s/pulls/%d/reviews", c.apiBase, c.Number)},
	}
	for _, s := range sources {
		cs, err := c.comments(ctx, s.kind, s.path)
		if err != nil {
			return snap, err
		}
		snap.Comments = append(snap.Comments, cs...)
	}

	runs, err := c.checkRuns(ctx, snap.HeadSHA)
	if err != nil {
		return snap, err
	}
	sts, err := c.statuses(ctx, snap.HeadSHA)
	if err != nil {
		return snap, err
	}
	snap.Checks = append(runs, sts...)
	return snap, nil
}
