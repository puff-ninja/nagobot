---
name: tmux
description: Remote-control tmux sessions for interactive CLIs by sending keystrokes and scraping pane output.
metadata: {"nagobot":{"requires":{"bins":["tmux"]}}}
---

# tmux Skill

Use tmux only when you need an interactive TTY. Prefer exec for non-interactive tasks.

## Quickstart

```bash
SOCKET="${TMPDIR:-/tmp}/nagobot-tmux.sock"
SESSION=nagobot-work

tmux -S "$SOCKET" new -d -s "$SESSION" -n shell
tmux -S "$SOCKET" send-keys -t "$SESSION":0.0 -- 'echo hello' Enter
tmux -S "$SOCKET" capture-pane -p -J -t "$SESSION":0.0 -S -200
```

After starting a session, print monitor commands:
```
To monitor:
  tmux -S "$SOCKET" attach -t "$SESSION"
  tmux -S "$SOCKET" capture-pane -p -J -t "$SESSION":0.0 -S -200
```

## Targeting panes

- Target format: `session:window.pane` (defaults to `:0.0`)
- Keep names short; avoid spaces
- Inspect: `tmux -S "$SOCKET" list-sessions`

## Sending input safely

- Prefer literal sends: `tmux -S "$SOCKET" send-keys -t target -l -- "$cmd"`
- Control keys: `tmux -S "$SOCKET" send-keys -t target C-c`

## Watching output

- Capture recent history: `tmux -S "$SOCKET" capture-pane -p -J -t target -S -200`

## Cleanup

- Kill a session: `tmux -S "$SOCKET" kill-session -t "$SESSION"`
- Kill all: `tmux -S "$SOCKET" kill-server`
