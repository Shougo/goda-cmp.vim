package lsp

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp/cmptest/e2e"
)

// TestRealGopls types Go code into a real Vim with gopls behind the
// source: member completion after ".", accepting an item, and accepting an
// item of a package that is not imported yet, which must add the import.
func TestRealGopls(t *testing.T) {
	p := goplsParams(t)
	bin := e2e.BuildConfig(t, "testdata/config")

	e2e.Run(t, func(t *testing.T, ed e2e.Editor) {
		// No indentation: the driver reads the popup from the screen, and a tab
		// would shift it.
		path := writeModule(t, "package main\n\nimport \"fmt\"\n\nfunc main() {\n}\n")
		// 1. "fmt." then "Pri": members of fmt, from the trigger character on.
		//    (The first request also starts gopls, hence the longer wait.)
		// 2. Ctrl-N selects the first item, Ctrl-Y accepts it.
		// 3. "strings.ToU": a package that is not imported; accept ToUpper.
		// 4. Save, so that the test can read the file.
		steps := `[
	  ["5Go", 0.3], ["fmt.", 4.0], ["\u001d", 0.3],
	  ["Println", 1.5], ["\u001d", 0.3],
	  ["\u000e", 0.5], ["\u0019", 1.0], ["\u001d", 0.3],
	  ["(\"\")\u001b", 0.3],
	  ["o", 0.3], ["strings.ToUpp", 2.0], ["\u001d", 0.3],
	  ["\u000e", 0.5], ["\u0019", 2.0], ["\u001d", 0.3],
	  ["(\"\")\u001b", 0.3],
	  [":w\r", 0.5]
	]`
		ds := e2e.Drive(t, ed, e2e.Options{Bin: bin, File: path, Steps: steps,
			Env: []string{"GODA_GOPLS=" + p.Command[0]}})
		// The popup shows the label and, from newer gopls, its
		// labelDetails ("Println(a ...any)"): compare the names only.
		for i := range ds {
			for j, w := range ds[i].Words {
				ds[i].Words[j], _, _ = strings.Cut(w, "(")
			}
		}
		if len(ds) != 5 {
			t.Fatalf("got %d records, want 5", len(ds))
		}
		// fmt has more members than the popup has rows; the visible ones are
		// exported identifiers of fmt.
		if !ds[0].Visible || len(ds[0].Words) < 5 || !slices.ContainsFunc(ds[0].Words, func(w string) bool {
			return strings.HasPrefix(w, "Sprint") || strings.HasPrefix(w, "Print") || strings.HasPrefix(w, "Errorf")
		}) {
			t.Errorf(`after "fmt.": %+v`, ds[0])
		}
		if !ds[1].Visible || len(ds[1].Words) == 0 || ds[1].Words[0] != "Println" {
			t.Errorf(`after "fmt.Println": %+v`, ds[1])
		}
		if ds[2].Line != "fmt.Println" || ds[2].Visible {
			t.Errorf("after accepting: %+v", ds[2])
		}
		if !ds[3].Visible || len(ds[3].Words) == 0 || ds[3].Words[0] != "ToUpper" {
			t.Errorf(`after "strings.ToUpp": %+v`, ds[3])
		}
		if ds[4].Line != "strings.ToUpper" {
			t.Errorf("after accepting ToUpper: %+v", ds[4])
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want := "package main\n\nimport (\n\t\"fmt\"\n\t\"strings\"\n)\n\nfunc main() {\nfmt.Println(\"\")\nstrings.ToUpper(\"\")\n}\n"
		if string(got) != want {
			t.Errorf("file:\n%s\nwant:\n%s", got, want)
		}

		// When Vim exits, the plugin closes its sources: gopls must not stay.
		if runtime.GOOS == "linux" {
			deadline := time.Now().Add(5 * time.Second)
			for goplsRunning(path) {
				if time.Now().After(deadline) {
					t.Error("gopls is still running after Vim exited")
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	})
}

// goplsRunning reports whether a gopls process works in the directory of
// path (Linux only: it reads /proc).
func goplsRunning(path string) bool {
	root := filepath.Dir(path)
	ents, _ := os.ReadDir("/proc")
	for _, e := range ents {
		cwd, err := os.Readlink(filepath.Join("/proc", e.Name(), "cwd"))
		if err != nil || cwd != root {
			continue
		}
		cmdline, _ := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline"))
		if strings.Contains(string(cmdline), "gopls") {
			return true
		}
	}
	return false
}
