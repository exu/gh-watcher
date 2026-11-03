# gh-watch

Poll one GitHub PR and print one line per new event: comments, reviews, CI results, merge/close. Built to be run by a Claude Code session through the Monitor tool, so each line wakes the agent working on that PR.

Uses the `gh` CLI for auth and API access. No tokens, no public endpoint, no webhooks.

## Install

```
make install          # tests, then builds to ~/.local/bin/gh-watch
make install-skill    # copies skill/SKILL.md to ~/.claude/skills/gh-watch
```

Needs Go and an authenticated `gh` (`gh auth status`).

## Use

```
gh-watch                      # PR of the current branch
gh-watch 1234 --repo org/repo # explicit PR
gh-watch https://github.com/org/repo/pull/1234
```

Flags: `--interval 60s`, `--bots copilot|none|all`, `--from-start`, `--once`, `--repo`.

Existing comments and finished checks are baselined silently on the first run. State lives in the user cache dir (`gh-watch/<owner>-<repo>-<n>.json`), so a restart does not repeat events. A file lock prevents two watchers on the same PR.

## Output

```
watching org/repo#1234 every 1m0s
comment alice: can you rename this? | https://github.com/...#issuecomment-1
review-comment bob internal/x.go: nit, extract | https://github.com/...#discussion_r2
review carol CHANGES_REQUESTED: see inline | https://github.com/...#pullrequestreview-3
ci build failure   (only failing checks are reported individually)
ci-summary abc1234 failure (1 failed, 12 total)
state merged
```

`ci-summary` fires once every check on the head commit is complete and unchanged for two consecutive polls. The process exits 0 when the PR is merged or closed.

## Claude Code wiring

The skill in `skill/SKILL.md` tells a session how to start the watcher with the Monitor tool and how to react to each line. Start it from the PR's worktree so the branch resolves the PR.

## Rate limits

Each poll is about 6 REST calls. At 60s that is roughly 360 calls/hour per watched PR, against a 5000/hour budget.
