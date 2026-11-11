package e2e

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// files are the driver (drive.py), the vimrc and the Vim script of the test
// UI, written out for each run.
//
//go:embed files
var files embed.FS

// Roots returns the directories of goda-cmp.vim and of goda.vim: those of
// the modules the test's module requires (its own directory for
// goda-cmp.vim's tests), or a goda.vim checked out next to goda-cmp.vim (its
// latest version, as the CI of goda-cmp.vim has it).
func Roots(t testing.TB) (repo, godaRoot string) {
	t.Helper()
	repo = ModuleDir(t, "github.com/Shougo/goda-cmp.vim")
	godaRoot = filepath.Join(filepath.Dir(repo), "goda.vim")
	if _, err := os.Stat(godaRoot); err != nil {
		godaRoot = ModuleDir(t, "github.com/Shougo/goda.vim")
	}
	return repo, godaRoot
}

// ModuleDir returns the directory of the module path, as the go command finds
// it from the test's directory: a UI's, to add to Options.RTP.
func ModuleDir(t testing.TB, path string) string {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", path).Output()
	if err != nil {
		t.Fatalf("go list -m %s: %v", path, err)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		t.Fatalf("go list -m %s: no directory", path)
	}
	return dir
}

// BuildConfig builds a configuration (the files of srcDir, a main package)
// and returns the binary. Its module requires what the test's module
// requires, and the test's module itself (its packages: a source of the
// test's repository, say), with the same replacements; goda-cmp.vim and
// goda.vim are those of Roots.
func BuildConfig(t testing.TB, srcDir string) string {
	t.Helper()
	repo, godaRoot := Roots(t)
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	if err := os.Mkdir(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	ents, err := os.ReadDir(srcDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		if e.IsDir() || e.Name() == "go.mod" || e.Name() == "go.sum" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(srcDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(cfgDir, e.Name()), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m := mainModule(t)
	var b strings.Builder
	b.WriteString("module goda-cmp-config\n\ngo 1.26\n\n")
	fmt.Fprintf(&b, "require %s v0.0.0\n", m.Path)
	for _, r := range m.Require {
		if r.Path != m.Path {
			fmt.Fprintf(&b, "require %s %s\n", r.Path, r.Version)
		}
	}
	replaced := map[string]string{
		"github.com/Shougo/goda-cmp.vim": repo,
		"github.com/Shougo/goda.vim":     godaRoot,
	}
	replaced[m.Path] = m.Dir
	if m.Path != "github.com/Shougo/goda-cmp.vim" {
		fmt.Fprintf(&b, "require github.com/Shougo/goda-cmp.vim v0.0.0\n")
	}
	for _, r := range m.Replace {
		if _, ok := replaced[r.Old.Path]; ok {
			continue
		}
		to := r.New.Path
		if r.New.Version != "" {
			to += " " + r.New.Version
		} else if !filepath.IsAbs(to) {
			to = filepath.Join(m.Dir, to)
		}
		fmt.Fprintf(&b, "replace %s => %s\n", r.Old.Path, to)
	}
	for _, p := range slices.Sorted(maps.Keys(replaced)) {
		fmt.Fprintf(&b, "replace %s => %s\n", p, filepath.ToSlash(replaced[p]))
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "go.mod"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	// The sums of the test's module: "go mod tidy" needs no others when
	// the configuration imports what it does.
	if sum, err := os.ReadFile(filepath.Join(m.Dir, "go.sum")); err == nil {
		if err := os.WriteFile(filepath.Join(cfgDir, "go.sum"), sum, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "goda-cmp")
	// "go mod tidy" settles the requirements (the replaced modules are
	// required at v0.0.0) so that go build does not refuse.
	for _, args := range [][]string{{"mod", "tidy"}, {"build", "-o", bin, "."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = cfgDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOWORK=off")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s in %s: %v\n%s", strings.Join(args, " "), cfgDir, err, b)
		}
	}
	return bin
}

// module is the main module of the test: go list -m -json, and the
// requirements and replacements of its go.mod.
type module struct {
	Path, Dir string
	Require   []struct{ Path, Version string }
	Replace   []struct {
		Old struct{ Path string }
		New struct{ Path, Version string }
	}
}

func mainModule(t testing.TB) module {
	t.Helper()
	var m module
	out, err := exec.Command("go", "list", "-m", "-json").Output()
	if err != nil {
		t.Fatalf("go list -m: %v", err)
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "mod", "edit", "-json")
	cmd.Dir = m.Dir
	out, err = cmd.Output()
	if err != nil {
		t.Fatalf("go mod edit -json: %v", err)
	}
	if err := json.Unmarshal(out, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// Dump is a record the editor wrote: DumpPopup() (Ctrl-] in insert mode, on
// the command line and in a terminal) writes Visible, Words, Line and
// Cmdline, and what a GodaDumpUI() of the test adds; the other fields are
// for records a test writes itself.
type Dump struct {
	Visible bool     `json:"visible"`
	Words   []string `json:"words"`
	Line    string   `json:"line"`
	Cmdline string   `json:"cmdline"`
	Left    bool     `json:"left"`
	Error   string   `json:"error"`
	Buffer  any      `json:"buffer"`
	Preview string   `json:"preview"`
	Inline  bool     `json:"inline"`
	Current *struct {
		Mark    string `json:"mark"`
		Visible bool   `json:"visible"`
	} `json:"current"`
}

// Options say how Drive runs the editor.
type Options struct {
	Bin   string   // the goda-cmp binary (BuildConfig)
	File  string   // the file opened
	Dir   string   // the working directory ("": the test's)
	Steps string   // JSON: [[keys, seconds to wait after them], ...]
	Vimrc string   // a Vim script of the test, sourced before goda_cmp#enable()
	RTP   []string // more 'runtimepath' directories
	// Env is more environment (NAME=value). The vimrc reads
	// GODA_DUMP_UI=test (record the test UI rather than Vim's popup),
	// GODA_PATCH_MAPS=1 (the run-time patch mappings) and
	// GODA_COMPLETEOPT.
	Env []string
}

// Drive starts ed in a pseudo terminal (with python3; the test is skipped
// without it), types the steps, and returns the records written to
// $GODA_OUT.
func Drive(t testing.TB, ed Editor, o Options) []Dump {
	t.Helper()
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is needed to drive the pty")
	}
	repo, godaRoot := Roots(t)
	dir := t.TempDir()
	sub, _ := fs.Sub(files, "files")
	if err := os.CopyFS(dir, sub); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "out.jsonl")
	cmd := exec.Command("python3", "-I", filepath.Join(dir, "drive.py"), ed.Bin, filepath.Join(dir, "vimrc.vim"), o.File, o.Steps)
	cmd.Dir = o.Dir
	cmd.Env = append(append(os.Environ(),
		"GODA_RTP="+godaRoot, "GODA_CMP_RTP="+repo, "GODA_TESTUI_RTP="+filepath.Join(dir, "testui"),
		"GODA_EXTRA_RTP="+strings.Join(o.RTP, ","), "GODA_VIMRC="+o.Vimrc,
		"GODA_CMP_BIN="+o.Bin, "GODA_OUT="+out), o.Env...)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("driver: %v\n%s", err, b)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("nothing recorded: %v", err)
	}
	var ds []Dump
	for l := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		var d Dump
		if err := json.Unmarshal([]byte(l), &d); err != nil {
			t.Fatalf("bad record %q: %v", l, err)
		}
		ds = append(ds, d)
	}
	for i, d := range ds {
		if d.Current != nil {
			t.Logf("record %d: %+v current %+v", i, d, *d.Current)
		} else {
			t.Logf("record %d: %+v", i, d)
		}
	}
	return ds
}

// Has reports whether the Vim expression expr is true in ed (without a
// vimrc), as has('patch-9.1.0448') or exists('##TextChangedT').
func (e Editor) Has(expr string) bool {
	f, err := os.CreateTemp("", "goda-cmp-e2e-*.vim")
	if err != nil {
		return false
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString("if " + expr + "\n  qa!\nendif\ncq!\n")
	if f.Close() != nil || err != nil {
		return false
	}
	return exec.Command(e.Bin, e.Headless(f.Name())...).Run() == nil
}
