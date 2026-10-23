// Package buffer is a completion source for the words of buffers: the
// current one and those shown in the tab page, kept up to date on buffer
// events. It is a port of ddc-source-buffer
// (https://github.com/matsui54/ddc-source-buffer, MIT license, Copyright
// matsui54).
package buffer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the source.
type Params struct {
	// RequireSameFiletype offers the words of buffers of the current
	// filetype only, and of the buffers in the tab page (default true).
	RequireSameFiletype *bool `json:"requireSameFiletype"`
	// LimitBytes: larger buffers are not read (default 1000000).
	LimitBytes int `json:"limitBytes"`
	// FromAltBuf offers the words of the alternate buffer too.
	FromAltBuf bool `json:"fromAltBuf"`
	// ForceCollect reads the buffers of the tab page whatever the size of
	// their files.
	ForceCollect bool `json:"forceCollect"`
	// BufNameStyle shows the buffer name in the menu: "none" (default),
	// "full" or "basename".
	BufNameStyle string `json:"bufNameStyle"`
}

type bufCache struct {
	filetype string
	name     string
	tick     int64
	words    []string
}

type source struct {
	p  Params
	mu *sync.Mutex
	// buffers is shared by the values WithParams makes: the words do not
	// depend on the parameters, only what is offered does.
	buffers map[int]*bufCache
}

// New returns the source, named "buffer".
func New(p Params) cmp.Source {
	return source{p: fill(p), mu: &sync.Mutex{}, buffers: map[int]*bufCache{}}
}

func fill(p Params) Params {
	if p.RequireSameFiletype == nil {
		p.RequireSameFiletype = new(true)
	}
	if p.LimitBytes <= 0 {
		p.LimitBytes = 1_000_000
	}
	return p
}

func (source) Name() string { return "buffer" }

// WithParams sets the parameters from Vim script (sourceParams).
func (s source) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(s.p, p)
	if err != nil {
		return nil, err
	}
	s.p = fill(np)
	return s, nil
}

func (source) Events() []cmp.EventSpec {
	return []cmp.EventSpec{{Events: []string{"BufWinEnter", "BufWritePost", "InsertEnter", "InsertLeave", "BufEnter"}}}
}

func (s source) OnEvent(ctx context.Context, h sdk.Host, ev cmp.Event) error {
	s.mu.Lock()
	_, cached := s.buffers[ev.Bufnr]
	s.mu.Unlock()
	if ev.Name == "BufEnter" && cached {
		return nil
	}
	if err := s.read(ctx, h, ev.Bufnr, ev.Filetype, true); err != nil {
		return err
	}
	return s.checkTab(ctx, h)
}

// read caches the words of a buffer. current: the buffer of the event,
// limited by its own size; the others by the size of their files (unless
// ForceCollect).
func (s source) read(ctx context.Context, h sdk.Host, bufnr int, filetype string, current bool) error {
	v, err := h.Eval(ctx, fmt.Sprintf("[getbufline(%d, 1, '$'), bufname(%d), getbufvar(%d, 'changedtick', 0), getbufvar(%d, '&filetype')]", bufnr, bufnr, bufnr, bufnr))
	if err != nil {
		return err
	}
	l, _ := v.([]any)
	if len(l) != 4 {
		return nil
	}
	rawLines, _ := l[0].([]any)
	name, _ := l[1].(string)
	if ft, ok := l[3].(string); ok && filetype == "" {
		filetype = ft
	}
	if current {
		size := 0
		for _, x := range rawLines {
			line, _ := x.(string)
			size += len(line) + 1
		}
		if size > s.p.LimitBytes {
			return nil
		}
	} else if !s.p.ForceCollect {
		st, err := os.Stat(name)
		if err != nil || st.Size() > int64(s.p.LimitBytes) {
			return nil
		}
	}
	seen := map[string]bool{}
	var words []string
	for _, x := range rawLines {
		line, _ := x.(string)
		for _, w := range keywords(line) {
			if !seen[w] {
				seen[w] = true
				words = append(words, w)
			}
		}
	}
	s.mu.Lock()
	s.buffers[bufnr] = &bufCache{filetype: filetype, name: name, tick: toInt64(l[2]), words: words}
	s.mu.Unlock()
	return nil
}

// checkTab reads the changed buffers of the tab page and forgets the
// buffers that are neither there nor listed.
func (s source) checkTab(ctx context.Context, h sdk.Host) error {
	v, err := h.Eval(ctx, "map(tabpagebuflist(), {_, b -> [b, getbufvar(b, 'changedtick', 0)]})")
	if err != nil {
		return err
	}
	tab := map[int]bool{}
	pairs, _ := v.([]any)
	for _, p := range pairs {
		bt, _ := p.([]any)
		if len(bt) != 2 {
			continue
		}
		b := int(toInt64(bt[0]))
		tab[b] = true
		s.mu.Lock()
		c := s.buffers[b]
		s.mu.Unlock()
		if c == nil || c.tick != toInt64(bt[1]) {
			if err := s.read(ctx, h, b, "", false); err != nil {
				return err
			}
		}
	}
	s.mu.Lock()
	var others []int
	for b := range s.buffers {
		if !tab[b] {
			others = append(others, b)
		}
	}
	s.mu.Unlock()
	for _, b := range others {
		v, err := h.Call(ctx, "buflisted", b)
		if err != nil {
			return err
		}
		if toInt64(v) == 0 {
			s.mu.Lock()
			delete(s.buffers, b)
			s.mu.Unlock()
		}
	}
	return nil
}

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	v, err := h.Eval(ctx, "[tabpagebuflist(), bufnr('#')]")
	if err != nil {
		return cmp.Gathered{}, err
	}
	l, _ := v.([]any)
	tab := map[int]bool{}
	alt := -1
	if len(l) == 2 {
		bs, _ := l[0].([]any)
		for _, b := range bs {
			tab[int(toInt64(b))] = true
		}
		alt = int(toInt64(l[1]))
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	bufnrs := make([]int, 0, len(s.buffers))
	for b := range s.buffers {
		bufnrs = append(bufnrs, b)
	}
	slices.Sort(bufnrs)
	var items []cmp.Item
	for _, b := range bufnrs {
		buf := s.buffers[b]
		if *s.p.RequireSameFiletype && buf.filetype != c.Filetype && !tab[b] && !(s.p.FromAltBuf && b == alt) {
			continue
		}
		menu := ""
		switch s.p.BufNameStyle {
		case "full":
			menu = buf.name
		case "basename":
			menu = filepath.Base(buf.name)
		}
		for _, w := range buf.words {
			items = append(items, cmp.Item{Word: w, Menu: menu})
		}
	}
	return cmp.Gathered{Start: c.KeywordStart(), Items: items}, nil
}

func keywords(line string) []string {
	return strings.FieldsFunc(line, func(r rune) bool {
		return !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r))
	})
}

func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case uint64:
		return int64(n)
	case int:
		return int64(n)
	case int8:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}

var _ cmp.EventHandler = source{}
