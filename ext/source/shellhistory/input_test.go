package shellhistory

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shellInputExpr in a real Vim: the input of goda-term.vim, of ddt.vim, or
// none.
func TestShellInputExpr(t *testing.T) {
	// As the test of the source vim: -short is without editors (the vim
	// on the PATH of Windows CI is the one of Git, which does not run it).
	if testing.Short() {
		t.Skip("starts Vim")
	}
	vim, err := exec.LookPath("vim")
	if err != nil {
		t.Skip("no vim")
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	errmsg := filepath.Join(dir, "errmsg")
	script := filepath.Join(dir, "test.vim")
	// The functions of the plugins (loaded: they are, in their buffers).
	if err := os.MkdirAll(filepath.Join(dir, "autoload"), 0o755); err != nil {
		t.Fatal(err)
	}
	for name, ret := range map[string]string{"goda_term": "term:", "ddt": "ddt:"} {
		f := "function! " + name + "#get_input(name) abort\n  return '" + ret + "' . a:name\nendfunction\n"
		if err := os.WriteFile(filepath.Join(dir, "autoload", name+".vim"), []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	src := `
let &runtimepath = ` + vimQuote(dir) + ` . ',' . &runtimepath
runtime autoload/goda_term.vim
runtime autoload/ddt.vim
try
  let s:out = [string(eval(` + vimQuote(shellInputExpr) + `))]
  let b:ddt_ui_name = 'd'
  call add(s:out, string(eval(` + vimQuote(shellInputExpr) + `)))
  let b:goda_term_name = 'g'
  call add(s:out, string(eval(` + vimQuote(shellInputExpr) + `)))
catch
  let s:out = ['error', v:throwpoint, v:exception]
endtry
call writefile(s:out, ` + vimQuote(out) + `)
call writefile([v:errmsg], ` + vimQuote(errmsg) + `)
qall!
`
	if err := os.WriteFile(script, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// Ex mode (-es): Vim waits for no terminal (on Windows it did, forever).
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, vim, "-N", "-u", "NONE", "-i", "NONE", "-n", "-es", "-S", script)
	// What Vim says, for a failure: the status of Vim in Ex mode is 1
	// after any error message, so the lines written are what is checked.
	output, runErr := cmd.CombinedOutput()
	msg, _ := os.ReadFile(errmsg)
	b, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("no result: %v (vim: %v: %s, v:errmsg %q)", err, runErr, output, msg)
	}
	got := strings.TrimSpace(strings.ReplaceAll(string(b), "\r\n", "\n"))
	if got != "[]\n['ddt:d']\n['term:g']" {
		t.Errorf("%q (vim: %v: %s, v:errmsg %q)", got, runErr, output, msg)
	} else if runErr != nil {
		t.Logf("vim: %v: %s, v:errmsg %q", runErr, output, msg)
	}
}

func vimQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }
