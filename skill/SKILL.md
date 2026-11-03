---
name: gh-watch
description: Get pinged inside this Claude session when a GitHub PR gets a comment, review, Copilot comment, CI result, or is merged/closed. Use when you open or work on a PR, when waiting on CI or review, or when the user asks to watch/monitor/babysit a PR.
---

# gh-watch - PR events into this session

`gh-watch` polls GitHub for one PR and prints one line per new event. Run it with the Monitor tool: each stdout line wakes this session.

One session = one PR = one watcher. The PR is resolved from the current branch, so run it from the PR's worktree.

## Start

1. Confirm a PR exists for this branch: `gh pr view --json number,url,state`. No PR yet means open it first (draft is fine).
2. Check no watcher is running for it already: `pgrep -fl 'gh-watch'`. A second one for the same PR exits with `already watching` (lock), so a duplicate is harmless but useless.
3. Start it with the Monitor tool (persistent), command:

```
gh-watch
```

Useful variants:
- `gh-watch 1234` or `gh-watch https://github.com/org/repo/pull/1234` - explicit PR, any cwd (add `--repo org/repo` for a bare number outside a checkout).
- `--interval 30s` - faster polling. Default 60s. Each poll is about 6 API calls, so keep many watchers at 60s or more.
- `--bots all|none|copilot` - which bot comments to report. Default `copilot` (Copilot review comments only; Sonar and other bots are dropped, but their failing checks still come through as `ci` lines).
- `--from-start` - also report comments and checks that already exist. Default is to baseline them silently.

The first line is `watching org/repo#N every 60s`. Existing comments and finished CI at start are baselined, so check the current state yourself with `gh pr view` and `gh pr checks` once after starting.

## Event lines

| Line | Meaning | Do |
|---|---|---|
| `comment <author>: <text> \| <url>` | new PR conversation comment | read it, answer or fix if it asks something |
| `review-comment <author> <path>: <text> \| <url>` | new inline review comment | fix the code at that path, reply on the thread |
| `review <author> <STATE>: <text> \| <url>` | review submitted (APPROVED, CHANGES_REQUESTED, COMMENTED with body, DISMISSED) | handle CHANGES_REQUESTED like review comments |
| `ci <check> <conclusion>` | one check FAILED (failure, cancelled, timed_out, action_required); passing and skipped checks are silent | fetch logs and fix: `gh run view --log-failed` or `gh pr checks` |
| `ci-summary <sha7> success\|failure (...)` | every check on the head commit is done and stable for two polls (the only success signal) | success: continue the flow; failure: fix |
| `state merged\|closed` | PR finished; the watcher exits | stop, report |

Notes:
- Comments you post yourself via `gh` come back as events too (same account). Ignore lines whose author is you and whose text you just wrote.
- `ci-summary` can lag one poll interval behind the last `ci` line by design.
- A push creates new check runs, so you get fresh `ci` (failures) and `ci-summary` lines per head commit.
- Lines are truncated to 200 chars. Open the `|` URL or run `gh pr view --comments` for the full text.

## Stop

The watcher exits by itself on merge or close. To stop earlier, stop the Monitor task (TaskStop) or `pkill -f 'gh-watch'`.

## Failures

- `gh-watch: already watching this PR (pid N)` - another process holds it; reuse that one or kill pid N.
- `poll failed (k/10)` on stderr - transient `gh` or network error; after 10 in a row it exits 1. Check `gh auth status`, then restart.
- State is per PR in `~/Library/Caches/gh-watch/` (macOS). Delete the file to re-baseline.
