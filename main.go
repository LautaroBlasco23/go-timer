package main

import (
	"embed"
	"flag"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

//go:embed assets templates
var assets embed.FS

// defaultPort is go-timer's own dedicated port, chosen to rarely collide with
// anything else.
const defaultPort = 18080

func main() {
	port := flag.Int("port", defaultPort, "port to listen on")
	data := flag.String("data", "", "path to timers.json (default: $XDG_DATA_HOME/go-timer/timers.json)")
	foreground := flag.Bool("foreground", false, "run the server in the foreground instead of backgrounding and opening a browser")
	flag.Parse()

	if *foreground {
		serve(*port, *data)
		return
	}
	launch(*port, *data)
}

// launch starts the server detached (unless it is already listening) and
// opens the browser on the app URL.
func launch(port int, data string) {
	url := fmt.Sprintf("http://localhost:%d", port)
	if !isServing(port) {
		if err := startDetached(port, data); err != nil {
			log.Fatalf("start server: %v", err)
		}
		waitForServer(port, 5*time.Second)
	}
	openBrowser(url)
}

// serve runs the HTTP server in the foreground.
func serve(port int, data string) {
	store, err := NewStore(data)
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

	addr := fmt.Sprintf(":%d", port)
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

// isServing reports whether something is listening on the port.
func isServing(port int) bool {
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 300*time.Millisecond)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func waitForServer(port int, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if isServing(port) {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// startDetached re-executes the current binary with -foreground in its own
// session, logging to a file, so the parent can exit immediately.
func startDetached(port int, data string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	logPath, err := serverLogPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(logPath), 0o755); err != nil {
		return err
	}
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer logFile.Close()

	args := []string{"-foreground", "-port", strconv.Itoa(port)}
	if data != "" {
		args = append(args, "-data", data)
	}
	cmd := exec.Command(exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}

// serverLogPath returns the detached server's log file location:
// $XDG_STATE_HOME/go-timer/server.log, falling back to
// ~/.local/state/go-timer/server.log.
func serverLogPath() (string, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "go-timer", "server.log"), nil
}

// openBrowser opens url in the system browser. Failure is logged, never
// fatal (e.g. headless machines).
func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("could not open browser: %v (open %s manually)", err, url)
		return
	}
	cmd.Process.Release()
}
