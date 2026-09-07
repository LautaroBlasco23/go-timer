# go-timer

Personal project developed to track the different activities I do in my day. Just a bunch of timers.

A small self-hosted timer web app written in Go with no external dependencies
(only the standard library and [htmx](https://htmx.org), embedded in the
binary). It supports two kinds of timers:

- **Chronometer (stopwatch)** — counts up, supports laps.
- **Countdown** — a stack of named activities, each with a duration. Steps run
  in order and the timer finishes when the last one expires.

All state is persisted to `timers.json` in the working directory.

## Running

Requires Go 1.22+.

```sh
go run .
```

Then open http://localhost:8080. To build a static binary:

```sh
go build -o go-timer .
```

Assets and templates are embedded with `go:embed`, so the binary is fully
self-contained.

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
timers.json   data file (created at runtime)
```

Note: `timers.json` stores durations in nanoseconds and keeps a single
authoritative copy on disk — concurrent writes are serialized by the store
mutex, but there is only one instance expected.
