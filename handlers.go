package main

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// viewData is the model shared by the sidebar and panel templates.
type viewData struct {
	Timers     []Timer
	SelectedID string
	Selected   *Timer // nil when there are no timers
}

type Handler struct {
	store *Store
	tpl   *template.Template
}

// viewData builds the template model, auto-selecting the first timer
// when no (or an unknown) id is given.
func (h *Handler) viewData(selectedID string) viewData {
	timers := h.store.List()
	data := viewData{Timers: timers}
	if selectedID == "" && len(timers) > 0 {
		selectedID = timers[0].ID
	}
	data.SelectedID = selectedID
	for i := range timers {
		if timers[i].ID == selectedID {
			data.Selected = &timers[i]
			break
		}
	}
	return data
}

func (h *Handler) render(w io.Writer, data viewData, names ...string) {
	for _, name := range names {
		if err := h.tpl.ExecuteTemplate(w, name, data); err != nil {
			log.Printf("render %s: %v", name, err)
		}
	}
}

// index serves the full page; ?id= picks the selected timer.
func (h *Handler) index(w http.ResponseWriter, r *http.Request) {
	h.render(w, h.viewData(r.URL.Query().Get("id")), "index")
}

// create adds a timer and selects it. Response: sidebar + out-of-band panel.
func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		h.render(w, h.viewData(""), "sidebar")
		return
	}
	var first time.Duration
	if v := strings.TrimSpace(r.FormValue("duration")); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			http.Error(w, "invalid duration, use e.g. 45m or 90s", http.StatusBadRequest)
			return
		}
		first = d
	}
	if r.FormValue("kind") == KindCountdown && first == 0 {
		http.Error(w, "countdowns need a duration (e.g. 45m)", http.StatusBadRequest)
		return
	}
	t, err := h.store.Create(name, r.FormValue("kind"), first)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	h.render(w, h.viewData(t.ID), "sidebar", "panelOOB")
}

// selectTimer returns the panel for a timer + out-of-band sidebar (highlight).
func (h *Handler) selectTimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if _, ok := h.store.Get(id); !ok {
		http.NotFound(w, r)
		return
	}
	h.render(w, h.viewData(id), "panel", "sidebarOOB")
}

// deleteTimer removes a timer. ?sel= is the currently selected timer so the
// panel can fall back to the next remaining one. Response: sidebar + OOB panel.
func (h *Handler) deleteTimer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := h.store.Delete(id); err != nil {
		http.NotFound(w, r)
		return
	}
	sel := r.URL.Query().Get("sel")
	if sel == id {
		sel = ""
	}
	h.render(w, h.viewData(sel), "sidebar", "panelOOB")
}

// action handles start/pause/reset/skip. Response: panel + out-of-band sidebar.
func (h *Handler) action(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var err error
	switch r.PathValue("action") {
	case "start":
		err = h.store.Start(id)
	case "pause":
		err = h.store.Pause(id)
	case "reset":
		err = h.store.Reset(id)
	case "skip":
		err = h.store.Skip(id)
	case "lap":
		err = h.store.Lap(id)
	default:
		http.NotFound(w, r)
		return
	}
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, err.Error(), http.StatusConflict)
		return
	}
	h.render(w, h.viewData(id), "panel", "sidebarOOB")
}

// addStep appends an activity to a countdown stack. Response: panel + OOB sidebar.
func (h *Handler) addStep(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	name := strings.TrimSpace(r.FormValue("name"))
	d, err := time.ParseDuration(strings.TrimSpace(r.FormValue("duration")))
	if name == "" || err != nil || d <= 0 {
		http.Error(w, "activity needs a name and a duration (e.g. 10m)", http.StatusBadRequest)
		return
	}
	if err := h.store.AddStep(id, name, d); err != nil {
		h.stepError(w, r, err)
		return
	}
	h.render(w, h.viewData(id), "panel", "sidebarOOB")
}

// deleteStep removes an activity. Response: panel + OOB sidebar.
func (h *Handler) deleteStep(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.store.DeleteStep(id, index); err != nil {
		h.stepError(w, r, err)
		return
	}
	h.render(w, h.viewData(id), "panel", "sidebarOOB")
}

// moveStep reorders an activity (up/down). Response: panel + OOB sidebar.
func (h *Handler) moveStep(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := h.store.MoveStep(id, index, r.PathValue("dir") == "up"); err != nil {
		h.stepError(w, r, err)
		return
	}
	h.render(w, h.viewData(id), "panel", "sidebarOOB")
}

// stepError maps store errors to HTTP statuses.
func (h *Handler) stepError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	http.Error(w, err.Error(), http.StatusConflict)
}

// display is polled every second: the big time text + out-of-band card times.
func (h *Handler) display(w http.ResponseWriter, r *http.Request) {
	t, ok := h.store.Get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := h.tpl.ExecuteTemplate(w, "display", &t); err != nil {
		log.Printf("render display: %v", err)
	}
	if err := h.tpl.ExecuteTemplate(w, "cardTimesOOB", h.store.List()); err != nil {
		log.Printf("render cardTimesOOB: %v", err)
	}
}

// formatDuration renders "M:SS" or "H:MM:SS".
func formatDuration(d time.Duration) string {
	s := int64(d.Seconds())
	if h := s / 3600; h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, s%3600/60, s%60)
	}
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}
