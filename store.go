package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	KindStopwatch = "stopwatch"
	KindCountdown = "countdown"

	StatusIdle    = "idle"
	StatusRunning = "running"
	StatusPaused  = "paused"
	StatusDone    = "done"
)

// ErrNotFound is returned when a timer id does not exist.
var ErrNotFound = errors.New("timer not found")

// defaultDataPath returns the default timers.json location:
// $XDG_DATA_HOME/go-timer/timers.json, falling back to
// ~/.local/share/go-timer/timers.json.
func defaultDataPath() (string, error) {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "go-timer", "timers.json"), nil
}

// Step is one activity in a countdown routine stack.
type Step struct {
	Name     string        `json:"name"`
	Duration time.Duration `json:"duration_ns"`
}

// Lap is a recorded split of a stopwatch.
type Lap struct {
	N     int           `json:"n"`
	Split time.Duration `json:"split_ns"`
	Total time.Duration `json:"total_ns"`
}

// Timer is a server-side chronometer or countdown. Elapsed time is always
// computed on read, so timers keep running without any goroutine or ticking.
// Countdown stacks advance lazily on read (see Store.normalizeAt).
type Timer struct {
	ID          string        `json:"id"`
	Name        string        `json:"name"`
	Kind        string        `json:"kind"` // "" → stopwatch (legacy data)
	Status      string        `json:"status"`
	StartedAt   time.Time     `json:"started_at"`
	Accumulated time.Duration `json:"accumulated_ns"`
	CreatedAt   time.Time     `json:"created_at"`

	// Countdown stack.
	Steps         []Step        `json:"steps,omitempty"`
	CurrentStep   int           `json:"current_step"`
	StepStartedAt time.Time     `json:"step_started_at"`
	StepAccum     time.Duration `json:"step_accum_ns"`
	// Stopwatch laps.
	Laps []Lap `json:"laps,omitempty"`
}

func (t Timer) IsCountdown() bool { return t.Kind == KindCountdown }

// Editable reports whether the activity stack may be modified.
func (t Timer) Editable() bool {
	return t.Status == StatusIdle || t.Status == StatusDone
}

func (t Timer) stepElapsed() time.Duration {
	if t.Status == StatusRunning {
		return t.StepAccum + time.Since(t.StepStartedAt)
	}
	return t.StepAccum
}

// Elapsed returns the total time counted so far.
func (t Timer) Elapsed() time.Duration {
	if t.IsCountdown() {
		var done time.Duration
		for i := 0; i < t.CurrentStep && i < len(t.Steps); i++ {
			done += t.Steps[i].Duration
		}
		return done + t.stepElapsed()
	}
	if t.Status == StatusRunning {
		return t.Accumulated + time.Since(t.StartedAt)
	}
	return t.Accumulated
}

// Remaining returns the total time left in a countdown stack.
func (t Timer) Remaining() time.Duration {
	if !t.IsCountdown() {
		return 0
	}
	var rem time.Duration
	for i := t.CurrentStep; i < len(t.Steps); i++ {
		rem += t.Steps[i].Duration
	}
	if rem -= t.stepElapsed(); rem < 0 {
		rem = 0
	}
	return rem
}

// StepRemaining returns the time left in the current step.
func (t Timer) StepRemaining() time.Duration {
	if !t.IsCountdown() || t.CurrentStep >= len(t.Steps) {
		return 0
	}
	if r := t.Steps[t.CurrentStep].Duration - t.stepElapsed(); r > 0 {
		return r
	}
	return 0
}

// CurrentStepName returns the active step's name, or "" when finished.
func (t Timer) CurrentStepName() string {
	if t.CurrentStep < len(t.Steps) {
		return t.Steps[t.CurrentStep].Name
	}
	return ""
}

// RingOffset is the SVG stroke-dashoffset (0–100) for the progress ring:
// the elapsed fraction of the current step.
func (t Timer) RingOffset() int {
	if !t.IsCountdown() {
		return 0
	}
	if t.Status == StatusDone || t.CurrentStep >= len(t.Steps) {
		return 100
	}
	d := t.Steps[t.CurrentStep].Duration
	if d <= 0 {
		return 0
	}
	p := int(t.stepElapsed() * 100 / d)
	switch {
	case p < 0:
		return 0
	case p > 100:
		return 100
	}
	return p
}

// LapsDesc returns laps newest-first for display.
func (t Timer) LapsDesc() []Lap {
	out := make([]Lap, len(t.Laps))
	for i, l := range t.Laps {
		out[len(t.Laps)-1-i] = l
	}
	return out
}

// Store is a thread-safe collection of timers persisted to a JSON file.
type Store struct {
	mu     sync.Mutex
	path   string
	timers []Timer
}

// NewStore loads the timers from path, creating parent directories as
// needed. An empty path selects defaultDataPath.
func NewStore(path string) (*Store, error) {
	if path == "" {
		var err error
		if path, err = defaultDataPath(); err != nil {
			return nil, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.timers); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// save persists the timers. Callers must hold s.mu.
func (s *Store) save() error {
	b, err := json.MarshalIndent(s.timers, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
}

// normalizeAt advances a running countdown past any expired steps, carrying
// the overflow into the next step's start timestamp. Returns true if the
// timer changed. Callers must hold s.mu.
func (s *Store) normalizeAt(t *Timer, now time.Time) bool {
	if !t.IsCountdown() || t.Status != StatusRunning {
		return false
	}
	changed := false
	for t.CurrentStep < len(t.Steps) {
		elapsed := t.StepAccum + now.Sub(t.StepStartedAt)
		if elapsed < t.Steps[t.CurrentStep].Duration {
			break
		}
		t.StepStartedAt = t.StepStartedAt.Add(t.Steps[t.CurrentStep].Duration)
		t.CurrentStep++
		changed = true
	}
	if t.CurrentStep >= len(t.Steps) {
		s.finishCountdown(t)
		changed = true
	}
	return changed
}

// finishCountdown freezes a completed stack. Callers must hold s.mu.
func (s *Store) finishCountdown(t *Timer) {
	var total time.Duration
	for _, st := range t.Steps {
		total += st.Duration
	}
	t.Accumulated = total
	t.StartedAt = time.Time{}
	t.StepAccum = 0
	t.StepStartedAt = time.Time{}
	t.Status = StatusDone
}

// normalizeLocked normalizes a timer by id and persists if it changed.
// Callers must hold s.mu.
func (s *Store) normalizeLocked(id string) {
	for i := range s.timers {
		if s.timers[i].ID == id && s.normalizeAt(&s.timers[i], time.Now()) {
			if err := s.save(); err != nil {
				log.Printf("save: %v", err)
			}
			return
		}
	}
}

func (s *Store) List() []Timer {
	s.mu.Lock()
	defer s.mu.Unlock()
	changed := false
	for i := range s.timers {
		changed = s.normalizeAt(&s.timers[i], time.Now()) || changed
	}
	if changed {
		if err := s.save(); err != nil {
			log.Printf("save: %v", err)
		}
	}
	out := make([]Timer, len(s.timers))
	copy(out, s.timers)
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) Get(id string) (Timer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLocked(id)
	for _, t := range s.timers {
		if t.ID == id {
			return t, true
		}
	}
	return Timer{}, false
}

// Create adds a timer. For countdowns, first > 0 seeds the stack with a
// single step named after the timer (e.g. a plain 45m countdown).
func (s *Store) Create(name, kind string, first time.Duration) (Timer, error) {
	if kind != KindStopwatch && kind != KindCountdown {
		return Timer{}, fmt.Errorf("unknown kind %q", kind)
	}
	id := make([]byte, 4)
	if _, err := rand.Read(id); err != nil {
		return Timer{}, err
	}
	t := Timer{
		ID:        hex.EncodeToString(id),
		Name:      name,
		Kind:      kind,
		Status:    StatusIdle,
		CreatedAt: time.Now(),
	}
	if kind == KindCountdown && first > 0 {
		t.Steps = []Step{{Name: name, Duration: first}}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.timers = append(s.timers, t)
	return t, s.save()
}

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.timers {
		if t.ID == id {
			s.timers = append(s.timers[:i], s.timers[i+1:]...)
			return s.save()
		}
	}
	return ErrNotFound
}

// Start begins counting. A done countdown restarts from step 0.
func (s *Store) Start(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.timers {
		t := &s.timers[i]
		if t.ID != id {
			continue
		}
		s.normalizeLocked(id)
		if t.Status == StatusRunning {
			return nil
		}
		if t.IsCountdown() && len(t.Steps) == 0 {
			return fmt.Errorf("add at least one activity before starting")
		}
		if t.Status == StatusDone {
			t.CurrentStep = 0
			t.Accumulated = 0
			t.Laps = nil
		}
		now := time.Now()
		t.Status = StatusRunning
		t.StartedAt = now
		t.StepStartedAt = now
		return s.save()
	}
	return ErrNotFound
}

// Pause stops counting and banks the elapsed time.
func (s *Store) Pause(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLocked(id)
	for i := range s.timers {
		t := &s.timers[i]
		if t.ID != id || t.Status != StatusRunning {
			continue
		}
		t.Accumulated = t.Elapsed()
		t.StepAccum = t.stepElapsed()
		t.Status = StatusPaused
		return s.save()
	}
	return ErrNotFound
}

// Reset returns the timer to its initial state, clearing laps.
func (s *Store) Reset(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.timers {
		t := &s.timers[i]
		if t.ID != id {
			continue
		}
		t.Status = StatusIdle
		t.StartedAt = time.Time{}
		t.StepStartedAt = time.Time{}
		t.Accumulated = 0
		t.StepAccum = 0
		t.CurrentStep = 0
		t.Laps = nil
		return s.save()
	}
	return ErrNotFound
}

// Lap records a stopwatch split. Only while running.
func (s *Store) Lap(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLocked(id)
	for i := range s.timers {
		t := &s.timers[i]
		if t.ID != id {
			continue
		}
		if t.IsCountdown() {
			return fmt.Errorf("laps are only available on chronometers")
		}
		if t.Status != StatusRunning {
			return fmt.Errorf("laps can only be recorded while running")
		}
		total := t.Elapsed()
		split := total
		n := len(t.Laps)
		if n > 0 {
			split = total - t.Laps[n-1].Total
		}
		t.Laps = append(t.Laps, Lap{N: n + 1, Split: split, Total: total})
		return s.save()
	}
	return ErrNotFound
}

// Skip advances a running countdown to its next step immediately.
func (s *Store) Skip(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.normalizeLocked(id)
	for i := range s.timers {
		t := &s.timers[i]
		if t.ID != id {
			continue
		}
		if !t.IsCountdown() || t.Status != StatusRunning {
			return fmt.Errorf("skip is only available while a countdown is running")
		}
		t.CurrentStep++
		if t.CurrentStep >= len(t.Steps) {
			s.finishCountdown(t)
		} else {
			t.StepAccum = 0
			t.StepStartedAt = time.Now()
		}
		return s.save()
	}
	return ErrNotFound
}

// stepGuard rejects stack edits on a live timer and resets a done timer,
// since its recorded result no longer matches the edited stack.
// Callers must hold s.mu. Returns the timer index.
func (s *Store) stepGuard(id string) (int, error) {
	for i := range s.timers {
		if s.timers[i].ID != id {
			continue
		}
		t := &s.timers[i]
		if !t.Editable() {
			return i, fmt.Errorf("stop the timer before editing activities")
		}
		if t.Status == StatusDone {
			t.Status = StatusIdle
			t.Accumulated = 0
			t.CurrentStep = 0
		}
		return i, nil
	}
	return 0, ErrNotFound
}

func (s *Store) AddStep(id, name string, d time.Duration) error {
	if name == "" || d <= 0 {
		return fmt.Errorf("activity needs a name and a duration")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.stepGuard(id)
	if err != nil {
		return err
	}
	s.timers[i].Steps = append(s.timers[i].Steps, Step{Name: name, Duration: d})
	return s.save()
}

func (s *Store) DeleteStep(id string, index int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.stepGuard(id)
	if err != nil {
		return err
	}
	steps := s.timers[i].Steps
	if index < 0 || index >= len(steps) {
		return fmt.Errorf("activity %d not found", index+1)
	}
	s.timers[i].Steps = append(steps[:index], steps[index+1:]...)
	return s.save()
}

func (s *Store) MoveStep(id string, index int, up bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i, err := s.stepGuard(id)
	if err != nil {
		return err
	}
	steps := s.timers[i].Steps
	if index < 0 || index >= len(steps) {
		return fmt.Errorf("activity %d not found", index+1)
	}
	j := index + 1
	if up {
		j = index - 1
	}
	if j < 0 || j >= len(steps) {
		return nil // already at the edge
	}
	steps[index], steps[j] = steps[j], steps[index]
	return s.save()
}
