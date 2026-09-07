# go-timer

Personal project developed to track the different activities I do in my day. Just a bunch of timers.

A small self-hosted timer web app written in Go with no external dependencies
(only the standard library and [htmx](https://htmx.org), embedded in the
binary). It supports two kinds of timers:

- **Chronometer (stopwatch)** — counts up, supports laps.
- **Countdown** — a stack of named activities, each with a duration. Steps run
  in order and the timer finishes when the last one expires.

All state is persisted to `timers.json` in the working directory.

## Installing

Requires Go 1.22+ and git on the target machine.

```sh
curl -fsSL https://raw.githubusercontent.com/LautaroBlasco23/go-timer/main/install.sh | sh
```

The script clones this repo, builds a static binary and installs it to
`~/.local/bin/go-timer` (override with `PREFIX=/path sh install.sh`). Assets
and templates are embedded with `go:embed`, so the binary is fully
self-contained.

## Running

Run `go-timer` anywhere: it starts the server detached in the background and
opens the browser on http://localhost:18080. Running it again while the
server is up just opens the browser — no second instance.

Flags:

| Flag           | Default                                    | Purpose                                                |
| -------------- | ------------------------------------------ | ------------------------------------------------------ |
| `-port`        | `18080`                                    | HTTP port                                              |
| `-data`        | `$XDG_DATA_HOME/go-timer/timers.json`      | Data file (fallback `~/.local/share/go-timer/`)        |
| `-foreground`  | off                                        | Run the server in the foreground (dev, logs to stdout) |

The detached server logs to `$XDG_STATE_HOME/go-timer/server.log`
(fallback `~/.local/state/go-timer/`).

Data used to be stored in `timers.json` in the working directory; migrate it
once with:

```sh
mkdir -p ~/.local/share/go-timer && mv timers.json ~/.local/share/go-timer/
```

To build manually:

```sh
go build -o go-timer .
```

## How it works

### Lazily computed time

Timers don't tick. There are no background goroutines or per-second updates on
the server. Each `Timer` stores status, start timestamps and accumulated
time, and elapsed/remaining values are **computed on read** from
`time.Since(...)`. This means:

- Timers keep running (server-side) even with no browser open.
- A running countdown advances past expired steps lazily on read
  (`Store.normalizeAt`), carrying any overflow into the next step.

### Durations

Durations are entered as Go duration strings, e.g. `45m`, `90s`, `1h30m`.

### Countdown stacks

A countdown is a sequence of steps (`name` + `duration`). While a countdown is
idle or done, the stack can be edited: activities can be added, removed, and
reordered. A `done` timer that gets edited resets to `idle`, since its
recorded result no longer matches the stack. While running, the current step
can be skipped.

### Frontend

The UI is server-rendered HTML templates with htmx for partial updates:

- The client polls `GET /timers/{id}/display` once per second; the response
  contains the big time display plus out-of-band (OOB) swaps for the sidebar
  cards, keeping every visible timer in sync.
- Mutations (create, start, pause, reset, skip, step edits) return just the
  affected fragments (sidebar / panel) with OOB swaps where needed.

## HTTP API

| Method | Path                                    | Purpose                                                         |
| ------ | --------------------------------------- | --------------------------------------------------------------- |
| GET    | `/`                                     | Full page (`?id=` selects a timer)                              |
| POST   | `/timers`                               | Create a timer (`name`, `kind`, `duration`)                     |
| GET    | `/timers/{id}`                          | Select a timer (returns panel + OOB sidebar)                    |
| DELETE | `/timers/{id}?sel=`                     | Delete a timer (`sel` = currently selected, for panel fallback) |
| GET    | `/timers/{id}/display`                  | Poll endpoint: time display + OOB card times                    |
| POST   | `/timers/{id}/{action}`                 | `start`, `pause`, `reset`, `skip`, `lap`                        |
| POST   | `/timers/{id}/steps`                    | Add an activity (`name`, `duration`)                            |
| DELETE | `/timers/{id}/steps/{index}`            | Remove an activity                                              |
| POST   | `/timers/{id}/steps/{index}/move/{dir}` | Reorder an activity (`up`/`down`)                               |

## Project layout

```
main.go       entry point, routes, template parsing
store.go      Timer model, Store (mutex + JSON persistence), state transitions
handlers.go   HTTP handlers, htmx fragment rendering
templates/    HTML templates (embedded)
assets/       style.css, htmx.min.js (embedded)
timers.json   data file, in ~/.local/share/go-timer/ (created at runtime)
```

Note: `timers.json` stores durations in nanoseconds and keeps a single
authoritative copy on disk — concurrent writes are serialized by the store
mutex, but there is only one instance expected.
