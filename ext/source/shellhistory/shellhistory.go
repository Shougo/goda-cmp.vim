// Package shellhistory is a completion source for the commands of shell
// history files (zsh's extended history format is understood), for terminals
// and shell buffers. It is a port of ddc-source-shell_history
// (https://github.com/Shougo/ddc-source-shell_history).
//
//	b.AddSource(shellhistory.New(shellhistory.Params{
//		Paths: []string{"~/.zsh-history"},
//	}))
//
// The commands that start with the text before the word under the cursor are
// offered, from that word on.
package shellhistory

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the source.
type Params struct {
	// Paths are the history files; "~" and environment variables are
	// expanded.
	Paths []string `json:"paths"`
	// Limit is how many commands of each file, from the newest, are used
	// (default 500).
	Limit int `json:"limit"`
}

type source struct{ p Params }

// New returns the source, named "shellhistory".
func New(p Params) cmp.Source {
	if p.Limit <= 0 {
		p.Limit = 500
	}
	return source{p}
}

func (source) Name() string { return "shellhistory" }

// WithParams sets the parameters from Vim script (sourceParams).
func (s source) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(s.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	var hist []string
	for _, p := range s.p.Paths {
		hist = append(hist, History(Expand(p), s.p.Limit)...)
	}
	input, err := ShellInput(ctx, h, c)
	if err != nil {
		return cmp.Gathered{}, err
	}
	start := c.KeywordStart()
	// The text of the shell line before the word (the prompt left out when
	// the input comes from ddt.vim).
	before := input[:max(0, len(input)-(len(c.Input)-start))]
	items := make([]cmp.Item, 0, len(hist))
	for _, w := range hist {
		if strings.HasPrefix(w, before) {
			items = append(items, cmp.Item{Word: w[len(before):]})
		}
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

// ShellInput is the shell line before the cursor: Context.Input, or in a
// ddt.vim or deol.nvim buffer, what they say the input is.
func ShellInput(ctx context.Context, h sdk.Host, c *cmp.Context) (string, error) {
	if c.Mode == "c" {
		return c.Input, nil
	}
	v, err := h.Eval(ctx, "&l:filetype ==# 'deol' && exists('*deol#get_input') ? [deol#get_input()]"+
		" : get(b:, 'ddt_ui_name', '') !=# '' && exists('*ddt#get_input') ? [ddt#get_input(b:ddt_ui_name)] : []")
	if err != nil {
		return c.Input, nil // not worth failing for
	}
	if l, ok := v.([]any); ok && len(l) == 1 {
		if s, ok := l[0].(string); ok {
			return s, nil
		}
	}
	return c.Input, nil
}

var extended = regexp.MustCompile(`^: \d+:\d+;(.*)`)

// History returns the limit newest commands of the history file path, each
// once, the newest first. (ddc-source-shell_history keeps the limit oldest of
// the newest-first list, which looks like a slip.) A file that cannot
// be read has none.
func History(path string, limit int) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var cmds []string
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		l := string(line)
		if m := extended.FindStringSubmatch(l); m != nil {
			l = m[1]
		}
		if l != "" {
			cmds = append(cmds, l)
		}
	}
	// Each command once, from the end.
	seen := map[string]bool{}
	var uniq []string
	for i := len(cmds) - 1; i >= 0; i-- {
		if !seen[cmds[i]] {
			seen[cmds[i]] = true
			uniq = append(uniq, cmds[i])
		}
	}
	return uniq[:min(limit, len(uniq))]
}

// Expand expands "~" and environment variables in a path.
func Expand(p string) string {
	if p == "~" || strings.HasPrefix(p, "~/") || strings.HasPrefix(p, `~\`) {
		if home, err := os.UserHomeDir(); err == nil {
			p = filepath.Join(home, p[1:])
		}
	}
	return os.ExpandEnv(p)
}
