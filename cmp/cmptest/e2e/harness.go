package e2e

import (
	"embed"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// files are the driver (drive.py), the vimrc and the Vim script of the test
// UI, written out for each run.
//
//go:embed files
var files embed.FS

// Roots returns the directories of goda-cmp.vim (the module, as the go
// command finds it from the test's directory) and of goda.vim, which must be
// next to it. The test is skipped without goda.vim.
func Roots(t testing.TB) (repo, godaRoot string) {
	t.Helper()
	out, err := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/Shougo/goda-cmp.vim").Output()
	if err != nil {
		t.Fatalf("go list: %v", err)
	}
	repo = strings.TrimSpace(string(out))
	godaRoot = filepath.Join(filepath.Dir(repo), "goda.vim")
	if _, err := os.Stat(godaRoot); err != nil {
		t.Skip("goda.vim is not next to goda-cmp.vim")
	}
	return repo, godaRoot
}

// BuildConfig builds a configuration (the files of srcDir, a main package)
// against goda-cmp.vim and goda.vim of Roots, and returns the binary.
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
	gomod := "module goda-cmp-config\n\ngo 1.26\n\n" +
		"require (\n\tgithub.com/Shougo/goda-cmp.vim v0.0.0\n\tgithub.com/Shougo/goda.vim v0.0.0\n)\n\n" +
		"replace github.com/Shougo/goda-cmp.vim => " + repo + "\n" +
		"replace github.com/Shougo/goda.vim => " + godaRoot + "\n"
	if err := os.WriteFile(filepath.Join(cfgDir, "go.mod"), []byte(gomod), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "goda-cmp")
	// The go.mod requires both modules at v0.0.0, while goda-cmp.vim's own
	// go.mod asks for a newer goda.vim; "go mod tidy" settles the
	// requirements (offline: both are replaced by local directories) so
	// that go build does not refuse.
	for _, args := range [][]string{{"mod", "tidy"}, {"build", "-o", bin, "."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = cfgDir
		cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s in %s: %v\n%s", strings.Join(args, " "), cfgDir, err, b)
		}
	}
	return bin
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
