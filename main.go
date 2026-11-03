package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const maxFailures = 10

// version is set at release build time via -ldflags.
var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	interval := flag.Duration("interval", 60*time.Second, "poll interval")
	repo := flag.String("repo", "", "owner/repo, when resolving a bare PR number or branch outside a checkout")
	bots := flag.String("bots", "copilot", "bot comments to report: none|copilot|all")
	fromStart := flag.Bool("from-start", false, "report existing comments and checks instead of baselining them")
	once := flag.Bool("once", false, "poll once and exit")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: gh-watch [flags] [PR number | PR URL | branch]  (default: PR of current branch)")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *showVersion {
		fmt.Println("gh-watch", version)
		return 0
	}
	if *bots != "none" && *bots != "copilot" && *bots != "all" {
		fmt.Fprintln(os.Stderr, "gh-watch: --bots must be none, copilot or all")
		return 2
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	owner, name, n, err := ResolvePR(ctx, ghRun, flag.Arg(0), *repo)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-watch:", err)
		return 2
	}

	statePath, err := statePathFor(owner, name, n)
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-watch:", err)
		return 1
	}
	unlock, err := lock(statePath + ".lock")
	if err != nil {
		fmt.Fprintln(os.Stderr, "gh-watch:", err)
		return 3
	}
	defer unlock()

	st := loadState(statePath)
	client := NewClient(ghRun, owner, name, n)
	opt := Options{Bots: *bots, FromStart: *fromStart}

	fmt.Printf("watching %s/%s#%d every %s\n", owner, name, n, *interval)
	failures := 0
	for {
		snap, err := client.Snapshot(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return 0
			}
			failures++
			fmt.Fprintf(os.Stderr, "gh-watch: poll failed (%d/%d): %v\n", failures, maxFailures, err)
			if failures >= maxFailures {
				return 1
			}
		} else {
			failures = 0
			for _, ev := range Diff(st, snap, opt) {
				fmt.Println(ev)
			}
			if err := saveState(statePath, st); err != nil {
				fmt.Fprintln(os.Stderr, "gh-watch: save state:", err)
			}
			if snap.State != "open" || *once {
				return 0
			}
		}
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(*interval):
		}
	}
}

func statePathFor(owner, repo string, n int) (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "gh-watch")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return filepath.Join(dir, fmt.Sprintf("%s-%s-%d.json", owner, repo, n)), nil
}

func loadState(path string) *State {
	st := NewState()
	b, err := os.ReadFile(path)
	if err != nil {
		return st
	}
	if json.Unmarshal(b, st) != nil || st.LastID == nil || st.Emitted == nil {
		return NewState()
	}
	return st
}

func saveState(path string, st *State) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// lock takes an exclusive flock so two watchers never report the same PR.
func lock(path string) (func(), error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		pid := make([]byte, 16)
		k, _ := f.Read(pid)
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("already watching this PR (pid %s)", string(pid[:k]))
		}
		return nil, err
	}
	_ = f.Truncate(0)
	_, _ = f.WriteString(fmt.Sprint(os.Getpid()))
	return func() { _ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN); f.Close() }, nil
}
