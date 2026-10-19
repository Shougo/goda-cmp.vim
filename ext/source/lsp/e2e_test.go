package lsp

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

type dump struct {
	Visible bool     `json:"visible"`
	Words   []string `json:"words"`
	Line    string   `json:"line"`
}

// TestRealVimGopls types Go code into a real Vim with gopls behind the
// source: member completion after ".", accepting an item, and accepting an
// item of a package that is not imported yet, which must add the import.
// It uses the driver and vimrc of the cmp package's end-to-end tests.
func TestRealVimGopls(t *testing.T) {
	p := goplsParams(t)
	vimBin, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("vim is not installed")
	}
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	repo, _ := filepath.Abs("../../..")
	godaRoot, _ := filepath.Abs("../../../../goda.vim")
	if _, err := os.Stat(godaRoot); err != nil {
		t.Skip("goda.vim is not next to goda-cmp.vim")
	}
	testdata := filepath.Join(repo, "cmp", "testdata")

	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	if err := os.Mkdir(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile("testdata/config/main.go")
	if err != nil {
		t.Fatal(err)
	}
	gomod := "module goda-cmp-config\n\ngo 1.24\n\n" +
		"require github.com/Shougo/goda-cmp.vim v0.0.0\n\n" +
		"replace github.com/Shougo/goda-cmp.vim => " + repo + "\n" +
		"replace github.com/Shougo/goda.vim => " + godaRoot + "\n"
	for name, data := range map[string][]byte{"main.go": b, "go.mod": []byte(gomod)} {
		if err := os.WriteFile(filepath.Join(cfgDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "goda-cmp")
	for _, args := range [][]string{{"mod", "tidy"}, {"build", "-o", bin, "."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = cfgDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// No indentation: the driver reads the popup from the screen, and a tab
	// would shift it.
	path := writeModule(t, "package main\n\nimport \"fmt\"\n\nfunc main() {\n}\n")
	out := filepath.Join(dir, "out.jsonl")
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
	cmd := exec.Command("python3", "-I", filepath.Join(testdata, "drive.py"), vimBin, filepath.Join(testdata, "vimrc.vim"), path, steps)
	cmd.Env = append(os.Environ(),
		"GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo, "GODA_CMP_BIN="+bin, "GODA_OUT="+out,
		"GODA_NO_PATCH_MAPS=1", "PUM_RTP=", "GODA_GOPLS="+p.Command[0])
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("driver: %v\n%s", err, b)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("Vim recorded nothing: %v", err)
	}
	var ds []dump
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		var d dump
		if err := json.Unmarshal([]byte(l), &d); err != nil {
			t.Fatalf("bad record %q: %v", l, err)
		}
		ds = append(ds, d)
	}
	t.Logf("records: %+v", ds)
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
