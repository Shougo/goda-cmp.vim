// Package shellnative is a completion source that asks a real shell what it
// would complete: zsh (through zpty), fish or xonsh, kept running in the
// background. It works in terminals and shell buffers, and on the command
// line after ":!" and ":terminal". It is a port of ddc-source-shell_native
// (https://github.com/Shougo/ddc-source-shell_native); its capture scripts
// are in capture/, under their own license.
//
//	b.AddSource(shellnative.New(shellnative.Params{Shell: "zsh"}))
package shellnative

import (
	"bufio"
	"context"
	"embed"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/source/shellhistory"
	"github.com/Shougo/goda.vim/sdk"
)

//go:embed capture/capture.* capture/zptyrc.zsh
var scripts embed.FS

// Params are the parameters of the source.
type Params struct {
	// Shell is "zsh", "fish" or "xonsh"; it must be installed.
	Shell string `json:"shell"`
	// Envs are added to the environment of the shell (which is Vim's).
	Envs map[string]string `json:"envs"`
}

type source struct {
	p  Params
	st *state
}

type state struct {
	mu   sync.Mutex // one completion at a time
	cmd  *exec.Cmd
	dir  string // the scripts
	in   io.WriteCloser
	out  chan []string
	cwd  string
	dead chan struct{}
	// stale counts the answers still to come for requests that gave up
	// waiting (superseded, or too slow): each answer is for the oldest
	// question, so they are read and dropped first.
	stale int
}

// New returns the source, named "shellnative".
func New(p Params) cmp.Source { return source{p: p, st: &state{}} }

func (source) Name() string { return "shellnative" }

// WithParams sets the parameters from Vim script (sourceParams); another
// shell is another process.
func (s source) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(s.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

// Init starts the shell with its capture script, in Vim's working directory
// and environment.
func (s source) Init(ctx context.Context, h sdk.Host) error {
	name := "capture." + s.p.Shell
	data, err := scripts.ReadFile("capture/" + name)
	if s.p.Shell == "" || err != nil {
		return fmt.Errorf("shell %q is not supported (zsh, fish or xonsh)", s.p.Shell)
	}
	dir, err := os.MkdirTemp("", "goda-cmp-shellnative")
	if err != nil {
		return err
	}
	files := map[string][]byte{name: data}
	if s.p.Shell == "zsh" {
		rc, _ := scripts.ReadFile("capture/zptyrc.zsh")
		files["zptyrc.zsh"] = rc
	}
	for n, d := range files {
		if err := os.WriteFile(filepath.Join(dir, n), d, 0o755); err != nil {
			os.RemoveAll(dir)
			return err
		}
	}
	v, err := h.Eval(ctx, "[getcwd(), environ()]")
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	l, _ := v.([]any)
	cwd, env := "", os.Environ()
	if len(l) == 2 {
		cwd, _ = l[0].(string)
		if m, ok := l[1].(map[string]any); ok {
			env = env[:0]
			for k, x := range m {
				env = append(env, k+"="+fmt.Sprint(x))
			}
		}
	}
	for k, x := range s.p.Envs {
		env = append(env, k+"="+x)
	}
	cmd := exec.Command(s.p.Shell, filepath.Join(dir, name))
	cmd.Dir, cmd.Env = cwd, env
	in, err := cmd.StdinPipe()
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("cannot start %s: %w", s.p.Shell, err)
	}
	out := make(chan []string, 16)
	dead := make(chan struct{})
	go func() {
		defer close(dead)
		sc := bufio.NewScanner(stdout)
		sc.Buffer(make([]byte, 64*1024), 16*1024*1024)
		var lines []string
		for sc.Scan() {
			if sc.Text() == "\x01EOC\x01" {
				out <- lines
				lines = nil
				continue
			}
			lines = append(lines, sc.Text())
		}
		_ = cmd.Wait()
	}()
	go func() {
		sc := bufio.NewScanner(stderr)
		for sc.Scan() {
			log.Printf("shellnative: %s: %s", s.p.Shell, sc.Text())
		}
	}()
	s.st.mu.Lock()
	s.st.cmd, s.st.dir, s.st.in, s.st.out, s.st.cwd, s.st.dead = cmd, dir, in, out, cwd, dead
	s.st.mu.Unlock()
	return nil
}

// Close stops the shell.
func (s source) Close(context.Context) error {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.cmd == nil {
		return nil
	}
	s.st.in.Close()
	_ = s.st.cmd.Process.Kill()
	<-s.st.dead
	os.RemoveAll(s.st.dir)
	s.st.cmd = nil
	return nil
}

// complete asks the shell, in the directory cwd.
func (s source) complete(ctx context.Context, input, cwd string) ([]string, error) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.cmd == nil {
		return nil, fmt.Errorf("the shell is not running")
	}
	var msg strings.Builder
	if cwd != "" && cwd != s.st.cwd {
		s.st.cwd = cwd
		msg.WriteString("chdir:" + cwd + "\n")
	}
	msg.WriteString("input:" + strings.ReplaceAll(input, "\n", " ") + "\n")
	// The answers of earlier requests that gave up come first.
	for s.st.stale > 0 {
		select {
		case <-s.st.out:
			s.st.stale--
		case <-s.st.dead:
			s.st.cmd = nil
			return nil, fmt.Errorf("the shell exited")
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if _, err := io.WriteString(s.st.in, msg.String()); err != nil {
		return nil, err
	}
	select {
	case lines := <-s.st.out:
		return lines, nil
	case <-s.st.dead:
		s.st.cmd = nil
		return nil, fmt.Errorf("the shell exited")
	case <-ctx.Done():
		s.st.stale++
		return nil, ctx.Err()
	}
}

var (
	rangeRe    = `(?:[0-9.$%, :]+|'.|\\[/?&]|/(?:[^\\/]+|\\.)*/|\?(?:[^\\?]+|\\.)*\?)*`
	silentRe   = `(?:sil(?:e?|ent?)\b!?)?`
	terminalRe = `(?:ter(?:m?|min?|minal?)\b!?(?:\s+\+\+\S*)*)`
	// cmdPrefix is ":!" or ":terminal", with a range and :silent.
	cmdPrefix = regexp.MustCompile(`^[:\s]*` + silentRe + `[:\s]*` + rangeRe + `[:\s]*(?:!|` + terminalRe + `)`)
	target    = regexp.MustCompile(`\S*$`)
)

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	none := cmp.Gathered{Start: len(c.Input), Incomplete: true}
	start := target.FindStringIndex(c.Input)[0]
	input := c.Input
	if c.Mode == "c" {
		if !shellCommand(ctx, h, c.Input) {
			return none, nil
		}
		m := cmdPrefix.FindString(c.Input)
		if m == "" {
			return none, nil
		}
		input = c.Input[len(m):]
		start = len(m) + target.FindStringIndex(input)[0]
	} else {
		var err error
		if input, err = shellhistory.ShellInput(ctx, h, c); err != nil {
			return cmp.Gathered{}, err
		}
	}
	input = strings.TrimLeft(input, " \t")
	if input == "" {
		return none, nil
	}
	cwd := ""
	if v, err := h.Call(ctx, "getcwd"); err == nil {
		cwd, _ = v.(string)
	}
	lines, err := s.complete(ctx, input, cwd)
	if err != nil {
		return cmp.Gathered{}, err
	}
	var items []cmp.Item
	for _, l := range lines {
		if l == "" {
			continue
		}
		if strings.HasSuffix(l, "//") {
			l = strings.TrimSuffix(l, "/")
		}
		word, info, _ := strings.Cut(l, "\t")
		items = append(items, cmp.Item{Word: word, Info: info})
	}
	// The shell completes from all of the line.
	return cmp.Gathered{Start: start, Items: items, Incomplete: true}, nil
}

// shellCommand reports whether the command line completes a shell command.
func shellCommand(ctx context.Context, h sdk.Host, input string) bool {
	if v, err := h.Call(ctx, "getcmdcompltype"); err == nil {
		if t, _ := v.(string); t == "shellcmd" || t == "shellcmdline" {
			return true
		}
	}
	return cmdPrefix.MatchString(input)
}

var (
	_ cmp.Initializer = source{}
	_ cmp.Closer      = source{}
)
