// Package e2e finds the editors the end-to-end tests run in: Vim (with
// +job, +channel and +timers) and Neovim, whichever are installed.
package e2e

import (
	"bytes"
	"os/exec"
	"testing"
)

// Editor is one installed editor.
type Editor struct {
	Name string // "vim" or "nvim"
	Bin  string
}

// Headless returns the arguments that run script without a UI, without a
// vimrc, and exit when the script does.
func (e Editor) Headless(script string) []string {
	if e.Name == "nvim" {
		return []string{"--headless", "-u", "NONE", "-i", "NONE", "-n", "-S", script}
	}
	return []string{"-u", "NONE", "-i", "NONE", "-N", "-es", "-S", script}
}

// Editors returns the installed editors. The test is skipped when there is
// none.
func Editors(t testing.TB) []Editor {
	t.Helper()
	var eds []Editor
	if bin, err := exec.LookPath("vim"); err == nil {
		ver, _ := exec.Command(bin, "--version").Output()
		if bytes.Contains(ver, []byte("+job")) && bytes.Contains(ver, []byte("+channel")) && bytes.Contains(ver, []byte("+timers")) {
			eds = append(eds, Editor{"vim", bin})
		}
	}
	if bin, err := exec.LookPath("nvim"); err == nil {
		eds = append(eds, Editor{"nvim", bin})
	}
	if len(eds) == 0 {
		t.Skip("neither vim (with +job, +channel and +timers) nor nvim is installed")
	}
	return eds
}

// Run runs fn as a subtest for every installed editor.
func Run(t *testing.T, fn func(t *testing.T, ed Editor)) {
	t.Helper()
	for _, ed := range Editors(t) {
		t.Run(ed.Name, func(t *testing.T) { fn(t, ed) })
	}
}
