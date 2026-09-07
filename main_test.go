package main

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestPickOpenerWSL(t *testing.T) {
	argv := pickOpener("http://localhost:18080", true)
	if len(argv) < 1 {
		t.Fatal("expected at least one opener candidate")
	}
	if last := argv[len(argv)-1]; !reflect.DeepEqual(last, []string{"xdg-open", "http://localhost:18080"}) {
		t.Errorf("xdg-open should be the last fallback, got %v", last)
	}
	allowed := map[string]bool{"wslview": true, "cmd.exe": true, "xdg-open": true}
	for _, a := range argv {
		if !allowed[a[0]] {
			t.Errorf("unexpected opener %q in %v", a[0], a)
		}
	}
	if _, err := exec.LookPath("wslview"); err == nil && argv[0][0] != "wslview" {
		t.Errorf("wslview installed but first candidate is %q", argv[0][0])
	}
}

func TestPickOpenerPlainLinux(t *testing.T) {
	argv := pickOpener("http://localhost:18080", false)
	want := [][]string{{"xdg-open", "http://localhost:18080"}}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("pickOpener(wsl=false) = %v, want %v", argv, want)
	}
}

// TestIsWSLMatchesOSRelease checks isWSL against the kernel-provided osrelease
// on Linux.
func TestIsWSLMatchesOSRelease(t *testing.T) {
	osrelease, err := os.ReadFile("/proc/sys/kernel/osrelease")
	if err != nil {
		t.Skip("no /proc/sys/kernel/osrelease (not Linux)")
	}
	want := strings.Contains(strings.ToLower(string(osrelease)), "microsoft")
	if got := isWSL(); got != want {
		t.Errorf("isWSL() = %v, want %v", got, want)
	}
}

func TestIsWSLEnvVar(t *testing.T) {
	t.Setenv("WSL_DISTRO_NAME", "Ubuntu")
	if !isWSL() {
		t.Error("isWSL() = false with WSL_DISTRO_NAME set")
	}
}
