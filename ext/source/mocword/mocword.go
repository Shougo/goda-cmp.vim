// Package mocword is a completion source for the next English word, from
// mocword (https://github.com/high-moctane/mocword), which must be installed
// with its data ($MOCWORD_DATA). It is a port of ddc-source-mocword
// (https://github.com/Shougo/ddc-source-mocword).
//
// The words typed before the cursor are the query: camelCase, snake_case and
// UPPER_CASE words are split, so that the next word fits the identifier.
package mocword

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the source.
type Params struct {
	// CommandPath is the mocword command (default "mocword").
	CommandPath string `json:"commandPath"`
}

type source struct {
	p Params
	// The process, started by Init; one query at a time.
	st *state
}

type state struct {
	mu  sync.Mutex
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// New returns the source, named "mocword".
func New(p Params) cmp.Source {
	if p.CommandPath == "" {
		p.CommandPath = "mocword"
	}
	return source{p: p, st: &state{}}
}

func (source) Name() string { return "mocword" }

// WithParams sets the parameters from Vim script (sourceParams); another
// command is another process.
func (s source) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(s.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

// Init starts mocword.
func (s source) Init(context.Context, sdk.Host) error {
	cmd := exec.Command(s.p.CommandPath, "--limit", "100")
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("cannot start %s (is it installed and in $PATH?): %w", s.p.CommandPath, err)
	}
	s.st.mu.Lock()
	s.st.cmd, s.st.in, s.st.out = cmd, in, bufio.NewReader(out)
	s.st.mu.Unlock()
	return nil
}

// Close stops mocword.
func (s source) Close(context.Context) error {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.cmd == nil {
		return nil
	}
	s.st.in.Close()
	_ = s.st.cmd.Process.Kill()
	_ = s.st.cmd.Wait()
	s.st.cmd = nil
	return nil
}

func (s source) Gather(ctx context.Context, _ sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	start := c.KeywordStart()
	complete := c.Input[start:]
	sentence, offset := extractWords(complete)
	query := c.Input
	if offset > 0 {
		query = sentence
	}
	line, err := s.ask(ctx, query)
	if err != nil {
		return cmp.Gathered{}, err
	}
	preceding := complete[:offset]
	var items []cmp.Item
	for _, w := range strings.Fields(line) {
		items = append(items, cmp.Item{Word: preceding + w})
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

// ask sends a query and reads the answer, one line of words.
func (s source) ask(ctx context.Context, query string) (string, error) {
	s.st.mu.Lock()
	defer s.st.mu.Unlock()
	if s.st.cmd == nil {
		return "", fmt.Errorf("mocword is not running")
	}
	if _, err := io.WriteString(s.st.in, strings.ReplaceAll(query, "\n", " ")+"\n"); err != nil {
		return "", err
	}
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		l, err := s.st.out.ReadString('\n')
		ch <- res{l, err}
	}()
	select {
	case r := <-ch:
		return r.line, r.err
	case <-ctx.Done():
		// The answer comes later and would be read as the next one: start
		// over on the next request.
		_ = s.st.cmd.Process.Kill()
		s.st.cmd = nil
		return "", ctx.Err()
	}
}

var (
	upperCase = regexp.MustCompile(`[A-Z][A-Z]+`)
	camelCase = regexp.MustCompile(`([A-Z]?[a-z]+|[A-Z][a-z]*)`)
	snakeCase = regexp.MustCompile(`[a-z][a-z]*`)
	nonAlpha  = regexp.MustCompile(`[^a-zA-Z]+$`)
)

// extractWords splits an identifier into words, and returns them as a
// sentence and where the last, unfinished word starts.
func extractWords(s string) (string, int) {
	m := upperCase.FindAllString(s, -1)
	if m == nil {
		m = camelCase.FindAllString(s, -1)
	}
	if m == nil {
		m = snakeCase.FindAllString(s, -1)
	}
	if m == nil {
		return s, 0
	}
	sentence := strings.Join(m, " ")
	if nonAlpha.MatchString(s) {
		return sentence + " ", len(s)
	}
	return sentence, strings.LastIndex(s, m[len(m)-1])
}

var (
	_ cmp.Initializer = source{}
	_ cmp.Closer      = source{}
)
