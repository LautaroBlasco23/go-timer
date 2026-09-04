package main

import (
	"embed"
	"html/template"
	"io/fs"
	"log"
	"net/http"
)

//go:embed assets templates
var assets embed.FS

func main() {
	store, err := NewStore()
	if err != nil {
		log.Fatal(err)
	}
	tpl, err := parseTemplates(assets)
	if err != nil {
		log.Fatal(err)
	}
	h := &Handler{store: store, tpl: tpl}

	mux := http.NewServeMux()
	mux.Handle("GET /assets/", http.FileServer(http.FS(assets)))
	mux.HandleFunc("GET /{$}", h.index)
	mux.HandleFunc("POST /timers", h.create)
	mux.HandleFunc("GET /timers/{id}", h.selectTimer)
	mux.HandleFunc("DELETE /timers/{id}", h.deleteTimer)
	mux.HandleFunc("GET /timers/{id}/display", h.display)
	mux.HandleFunc("POST /timers/{id}/{action}", h.action)
	mux.HandleFunc("POST /timers/{id}/steps", h.addStep)
	mux.HandleFunc("DELETE /timers/{id}/steps/{index}", h.deleteStep)
	mux.HandleFunc("POST /timers/{id}/steps/{index}/move/{dir}", h.moveStep)

	addr := ":8080"
	log.Printf("go-timer listening on http://localhost%s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}

func parseTemplates(assets fs.FS) (*template.Template, error) {
	sub, err := fs.Sub(assets, "templates")
	if err != nil {
		return nil, err
	}
	tpl := template.New("").Funcs(template.FuncMap{"formatDuration": formatDuration})
	return tpl.ParseFS(sub, "*.html")
}
