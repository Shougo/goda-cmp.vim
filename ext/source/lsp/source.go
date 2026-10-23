// Package lsp is a completion source backed by a language server (gopls,
// clangd, ...), which it starts itself as a child process: one process per
// root directory, talking the Language Server Protocol over stdio.
//
// The server's settings are Go values in your config.go, like any other
// source parameters. Add the source once per server, each under its own
// name (the package is github.com/Shougo/goda-cmp.vim/ext/source/lsp):
//
//	b.AddSource(lsp.New(lsp.Params{
//		Name:        "gopls",
//		Command:     []string{"gopls"},
//		Filetypes:   []string{"go", "gomod"},
//		RootMarkers: []string{"go.work", "go.mod", ".git"},
//	}))
//
// and give it these options: forceCompletionPattern for the places where
// the server completes without a word ("\\." for members), no matchers (the
// server filters itself) and a sorter that honours the server's order
// (sortertext).
//
// Documents are synchronized in full: the whole buffer is sent when its
// changedtick moved. Entering a buffer of the source's filetypes starts its
// server and opens the document; deleting the buffer closes it
// (cmp.EventHandler).
package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params configure one language server.
type Params struct {
	// Name is the source name (default "lsp"). Use one per server.
	Name string
	// Command starts the server, which must speak LSP on stdin/stdout.
	Command []string
	// Env is added to the environment of the server ("KEY=value").
	Env []string
	// Filetypes the source works for; empty means every filetype.
	Filetypes []string
	// RootMarkers are file or directory names whose presence marks the
	// project root, searched upwards from the file. The first directory
	// that contains any of them wins. Without a match (or markers) the
	// file's own directory is the root.
	RootMarkers []string
	// LanguageID maps a Vim filetype to an LSP languageId (default: the
	// filetype itself).
	LanguageID func(filetype string) string
	// InitializationOptions are sent with initialize.
	InitializationOptions any
	// Settings answer workspace/configuration (by dotted section) and are
	// sent with workspace/didChangeConfiguration after initialize.
	Settings any
	// InitTimeout bounds starting and initializing a server (default 30s).
	// It is independent of sourceTimeout: requests made meanwhile just
	// come back empty.
	InitTimeout time.Duration
	// Stderr receives the server's standard error (default: the plugin's,
	// which goda#log() shows).
	Stderr io.Writer
	// ConfirmBehavior is what an accepted item does to the text after the
	// cursor when the server offers both (an InsertReplaceEdit): "insert"
	// (default) keeps it, "replace" replaces the part the server names,
	// typically the rest of the word: accepting "Println" in "fmt.Pr|int"
	// gives "fmt.Println" instead of "fmt.Printlnint". A plain edit that
	// reaches past the cursor always replaces.
	ConfirmBehavior string
}

// Source is the language server source. It implements cmp.Source,
// cmp.StreamSource, cmp.Initializer, cmp.Resolver, cmp.CompleteDoner,
// cmp.Closer and cmp.EventHandler.
type Source struct {
	p Params

	mu      sync.Mutex
	clients map[string]*client // by root
	roots   map[string]string  // directory -> root
	closed  bool
}

// New returns the source.
func New(p Params) *Source {
	if p.Name == "" {
		p.Name = "lsp"
	}
	if p.InitTimeout <= 0 {
		p.InitTimeout = 30 * time.Second
	}
	if p.Stderr == nil {
		p.Stderr = os.Stderr
	}
	if p.ConfirmBehavior == "" {
		p.ConfirmBehavior = "insert"
	}
	return &Source{p: p, clients: map[string]*client{}, roots: map[string]string{}}
}

func (s *Source) Name() string { return s.p.Name }

// Init only checks that the server can be found. The server itself is
// started per root, by the first request for a file under it: Init has no
// Context, so it cannot know the root, and a failed Init disables the source
// for good, which a slow server start must not do.
func (s *Source) Init(context.Context, sdk.Host) error {
	if len(s.p.Command) == 0 {
		return errors.New("lsp: Params.Command is empty")
	}
	if s.p.ConfirmBehavior != "insert" && s.p.ConfirmBehavior != "replace" {
		return fmt.Errorf("lsp %s: ConfirmBehavior %q: want \"insert\" or \"replace\"", s.p.Name, s.p.ConfirmBehavior)
	}
	if _, err := exec.LookPath(s.p.Command[0]); err != nil {
		return fmt.Errorf("lsp %s: %w", s.p.Name, err)
	}
	return nil
}

func (s *Source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	return cmp.Collect(ctx, h, c, s)
}

// itemData is the UserData of an item: what Resolve and OnCompleteDone need.
type itemData struct {
	cl       *client
	raw      json.RawMessage // the item as the server sent it (+ default data)
	resolved bool
	// tail is the text after the cursor that accepting the item replaces
	// (the end of its edit range).
	tail string
}

// buffer is what the source asks the editor about the buffer: one call per
// request.
type buffer struct {
	path    string
	buftype string
	lines   []string
}

func (s *Source) buffer(ctx context.Context, h sdk.Host, bufnr int) (buffer, error) {
	var b buffer
	v, err := h.Eval(ctx, fmt.Sprintf("[fnamemodify(bufname(%d), ':p'), getbufvar(%d, '&buftype'), getbufline(%d, 1, '$')]", bufnr, bufnr, bufnr))
	if err != nil {
		return b, err
	}
	arr, ok := v.([]any)
	if !ok || len(arr) != 3 {
		return b, fmt.Errorf("lsp: unexpected buffer answer %T", v)
	}
	b.path, _ = arr[0].(string)
	b.buftype, _ = arr[1].(string)
	lines, _ := arr[2].([]any)
	for _, l := range lines {
		str, _ := l.(string)
		b.lines = append(b.lines, str)
	}
	return b, nil
}

func (s *Source) GatherStream(ctx context.Context, h sdk.Host, c *cmp.Context, emit func(cmp.Gathered) error) error {
	if len(s.p.Filetypes) > 0 && !slices.ContainsFunc(strings.Split(c.Filetype, "."), func(ft string) bool {
		return slices.Contains(s.p.Filetypes, ft)
	}) {
		return nil
	}
	buf, err := s.buffer(ctx, h, c.Bufnr)
	if err != nil {
		return err
	}
	// Only real files: a server cannot do much with an unnamed buffer, and
	// help or terminal buffers are not source code.
	if buf.buftype != "" || buf.path == "" || strings.HasSuffix(buf.path, "/") || strings.HasSuffix(buf.path, `\`) {
		return nil
	}
	cl := s.client(buf.path)
	if cl == nil {
		return nil
	}
	if err := cl.wait(ctx); err != nil {
		return err
	}
	cp := cl.caps.CompletionProvider
	if cp == nil {
		return nil
	}

	// Whether to ask at all is the engine's decision (minAutoCompleteLength,
	// forceCompletionPattern). The server is told when it is asked because
	// of one of its trigger characters.
	ks := c.KeywordStart()
	triggerKind, trigger := 1, ""
	if ks == len(c.Input) && !c.Manual {
		for _, t := range cp.TriggerCharacters {
			if t != "" && strings.HasSuffix(c.Input, t) {
				triggerKind, trigger = 2, t
				break
			}
		}
	}
	// The cursor line, for edits that reach past the cursor; the buffer may
	// be newer than the request, so only if it still starts with Input.
	after := ""
	if c.Lnum >= 1 && c.Lnum <= len(buf.lines) && strings.HasPrefix(buf.lines[c.Lnum-1], c.Input) {
		after = buf.lines[c.Lnum-1][len(c.Input):]
	}

	uri := fileURI(buf.path)
	langID := c.Filetype
	if s.p.LanguageID != nil {
		langID = s.p.LanguageID(c.Filetype)
	}
	text := func() (string, error) { return joinLines(buf.lines), nil }
	if err := cl.sync(uri, langID, c.Bufnr, int(c.Tick), text); err != nil {
		return err
	}

	cctx := map[string]any{"triggerKind": triggerKind}
	if trigger != "" {
		cctx["triggerCharacter"] = trigger
	}
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     position{Line: c.Lnum - 1, Character: units(c.Input, cl.enc)},
		"context":      cctx,
	}
	var (
		pmu     sync.Mutex
		partial []json.RawMessage
		defs    *itemDefaults
	)
	list, err := cl.complete(ctx, params, func(l completionList) {
		pmu.Lock()
		partial = append(partial, l.Items...)
		if l.ItemDefaults != nil {
			defs = l.ItemDefaults
		}
		items := slices.Clone(partial)
		d := defs
		pmu.Unlock()
		_ = emit(s.gathered(cl, c, ks, after, items, d))
	})
	if err != nil {
		return err
	}
	pmu.Lock()
	items := append(slices.Clone(partial), list.Items...)
	if list.ItemDefaults != nil {
		defs = list.ItemDefaults
	}
	pmu.Unlock()
	return emit(s.gathered(cl, c, ks, after, items, defs))
}

// gathered converts server items into candidates. complete() replaces one
// range for all items, so every item is expressed relative to the leftmost
// start among them: text between that start and the item's own start is
// put in front of its word (and left out of what the popup shows). after is
// the text after the cursor, for edits that reach past it.
func (s *Source) gathered(cl *client, c *cmp.Context, ks int, after string, raws []json.RawMessage, defs *itemDefaults) cmp.Gathered {
	var defRange editRange
	defFormat := 0
	var defData json.RawMessage
	if defs != nil {
		defRange = decodeEditRange(defs.EditRange)
		defFormat = defs.InsertTextFormat
		defData = defs.Data
	}
	// tail is the text after the cursor that an edit ending at end
	// replaces: always for a plain edit, for an insert/replace edit only
	// with ConfirmBehavior "replace".
	tail := func(e editRange) string {
		r := e.Range
		if e.Insert != nil {
			if s.p.ConfirmBehavior != "replace" {
				return ""
			}
			r = e.Replace
		}
		if r == nil || r.End.Line != c.Lnum-1 {
			return ""
		}
		line := c.Input + after
		end := byteOffset(line, r.End.Character, cl.enc)
		if end <= len(c.Input) {
			return ""
		}
		return line[len(c.Input):end]
	}
	line := c.Lnum - 1

	type conv struct {
		it    cmp.Item
		start int
	}
	convs := make([]conv, 0, len(raws))
	minStart := ks
	for _, raw := range raws {
		var ci completionItem
		if json.Unmarshal(raw, &ci) != nil {
			continue
		}
		format := ci.InsertTextFormat
		if format == 0 {
			format = defFormat
		}
		start := ks
		var text, rest string
		switch {
		case ci.TextEdit != nil && ci.TextEdit.insertRange() != nil:
			r := ci.TextEdit.insertRange()
			if r.Start.Line != line {
				continue // an edit that starts on another line cannot be offered
			}
			start = byteOffset(c.Input, r.Start.Character, cl.enc)
			text = ci.TextEdit.NewText
			rest = tail(*ci.TextEdit)
		case defRange.insertRange() != nil && defRange.insertRange().Start.Line == line:
			start = byteOffset(c.Input, defRange.insertRange().Start.Character, cl.enc)
			text = ci.TextEditText
			if text == "" {
				text = ci.Label
			}
			rest = tail(defRange)
		default:
			text = ci.InsertText
			if text == "" {
				text = ci.Label
			}
		}
		if format == snippetFormat {
			text = stripSnippet(text)
		}
		// complete() inserts one line.
		if i := strings.IndexAny(text, "\r\n"); i >= 0 {
			text = text[:i]
		}
		if text == "" {
			continue
		}
		it := cmp.Item{
			Word:       text,
			Kind:       kindName(ci.Kind),
			Info:       info(ci.Detail, documentation(ci.Documentation)),
			FilterText: ci.FilterText,
			SortText:   ci.SortText,
			Dup:        1,
			UserData:   &itemData{cl: cl, raw: withDefaultData(raw, defData), tail: rest},
		}
		if abbr := ci.Label; abbr != text || ci.LabelDetails != nil {
			if ci.LabelDetails != nil {
				abbr += ci.LabelDetails.Detail
			}
			it.Abbr = abbr
		}
		convs = append(convs, conv{it, start})
		minStart = min(minStart, start)
	}
	items := make([]cmp.Item, len(convs))
	for i, cv := range convs {
		it := cv.it
		if pre := c.Input[minStart:cv.start]; pre != "" {
			if it.Abbr == "" {
				it.Abbr = it.Word
			}
			it.Word = pre + it.Word
			if it.FilterText != "" {
				it.FilterText = pre + it.FilterText
			}
		}
		items[i] = it
	}
	// The server filters by itself (fuzzy, often with isIncomplete): it is
	// asked again for every keystroke instead of the engine filtering these
	// items again.
	return cmp.Gathered{Start: minStart, Items: items, Incomplete: true}
}

// joinLines is the text of a buffer: every line ends with a newline.
func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}

// Events asks for the buffers of the source's filetypes (all, when
// Params.Filetypes is empty): entering one starts its server and opens the
// document, deleting or wiping it out closes the document.
func (s *Source) Events() []cmp.EventSpec {
	return []cmp.EventSpec{{
		Events:    []string{"BufEnter", "BufDelete", "BufWipeout"},
		Filetypes: s.p.Filetypes,
	}}
}

// OnEvent starts the server for a file as soon as it is entered, so that the
// first completion does not wait for it, and keeps the server's set of open
// documents in step with the buffers.
func (s *Source) OnEvent(ctx context.Context, h sdk.Host, ev cmp.Event) error {
	if ev.Buftype != "" || ev.Path == "" || strings.HasSuffix(ev.Path, "/") || strings.HasSuffix(ev.Path, `\`) {
		return nil
	}
	uri := fileURI(ev.Path)
	switch ev.Name {
	case "BufEnter":
		cl := s.client(ev.Path)
		if cl == nil {
			return nil
		}
		// A slow server may still be starting when ctx ends; the first
		// completion then opens the document instead.
		if err := cl.wait(ctx); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		langID := ev.Filetype
		if s.p.LanguageID != nil {
			langID = s.p.LanguageID(ev.Filetype)
		}
		return cl.sync(uri, langID, ev.Bufnr, int(ev.Tick), func() (string, error) {
			v, err := h.Call(ctx, "getbufline", ev.Bufnr, 1, "$")
			if err != nil {
				return "", err
			}
			raw, _ := v.([]any)
			lines := make([]string, len(raw))
			for i, l := range raw {
				lines[i], _ = l.(string)
			}
			return joinLines(lines), nil
		})
	case "BufDelete", "BufWipeout":
		cl := s.existing(ev.Path)
		if cl == nil {
			return nil
		}
		select {
		case <-cl.ready:
		default:
			return nil // still starting: it was given nothing yet
		}
		if cl.initErr != nil {
			return nil
		}
		return cl.closeDoc(uri)
	}
	return nil
}

func info(detail, doc string) string {
	switch {
	case detail == "":
		return doc
	case doc == "":
		return detail
	}
	return detail + "\n\n" + doc
}

// Resolve asks the server for the details of the selected item
// (completionItem/resolve), typically its documentation.
func (s *Source) Resolve(ctx context.Context, _ sdk.Host, a cmp.DoneArgs) (cmp.Item, error) {
	d, ok := a.Item.UserData.(*itemData)
	if !ok || d.resolved || !d.cl.canResolve() {
		return a.Item, nil
	}
	raw, err := d.cl.resolve(ctx, d.raw)
	if err != nil {
		return a.Item, err
	}
	var ci completionItem
	if err := json.Unmarshal(raw, &ci); err != nil {
		return a.Item, err
	}
	it := a.Item
	if i := info(ci.Detail, documentation(ci.Documentation)); i != "" {
		it.Info = i
	}
	it.UserData = &itemData{cl: d.cl, raw: raw, resolved: true, tail: d.tail}
	return it, nil
}

// OnCompleteDone replaces the text after the cursor that the item's edit
// covers (see ConfirmBehavior), then applies its additionalTextEdits (an
// import, for instance), resolving the item first when the server left them
// out.
func (s *Source) OnCompleteDone(ctx context.Context, h sdk.Host, a cmp.DoneArgs) error {
	d, ok := a.Item.UserData.(*itemData)
	if !ok {
		return nil
	}
	if d.tail != "" {
		if err := deleteAfterCursor(ctx, h, a.Context.Bufnr, d.tail); err != nil {
			return err
		}
	}
	raw := d.raw
	var ci completionItem
	if err := json.Unmarshal(raw, &ci); err != nil {
		return err
	}
	if len(ci.AdditionalTextEdits) == 0 && !d.resolved && d.cl.canResolve() {
		r, err := d.cl.resolve(ctx, raw)
		if err != nil {
			return err
		}
		ci = completionItem{}
		if err := json.Unmarshal(r, &ci); err != nil {
			return err
		}
	}
	if len(ci.AdditionalTextEdits) == 0 {
		return nil
	}
	return applyEdits(ctx, h, a.Context.Bufnr, d.cl.enc, ci.AdditionalTextEdits)
}

// Close stops every server.
func (s *Source) Close(ctx context.Context) error {
	s.mu.Lock()
	s.closed = true
	cls := make([]*client, 0, len(s.clients))
	for _, cl := range s.clients {
		cls = append(cls, cl)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, cl := range cls {
		wg.Go(func() {
			cl.shutdown(ctx)
		})
	}
	wg.Wait()
	return nil
}

// client returns the server for the file at path, starting it (or starting
// it again after it died) when needed.
func (s *Source) client(path string) *client {
	root := s.root(filepath.Dir(path))
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	cl := s.clients[root]
	if cl == nil || !cl.alive() {
		cl = startClient(&s.p, root)
		s.clients[root] = cl
	}
	return cl
}

// existing returns the server for the file at path if one runs; it does not
// start one.
func (s *Source) existing(path string) *client {
	root := s.root(filepath.Dir(path))
	s.mu.Lock()
	defer s.mu.Unlock()
	if cl := s.clients[root]; cl != nil && cl.alive() {
		return cl
	}
	return nil
}

func (s *Source) root(dir string) string {
	s.mu.Lock()
	r, ok := s.roots[dir]
	s.mu.Unlock()
	if ok {
		return r
	}
	r = dir
	for d := dir; len(s.p.RootMarkers) > 0; {
		if slices.ContainsFunc(s.p.RootMarkers, func(m string) bool {
			_, err := os.Stat(filepath.Join(d, m))
			return err == nil
		}) {
			r = d
			break
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	s.mu.Lock()
	s.roots[dir] = r
	s.mu.Unlock()
	return r
}

// Check the interfaces at compile time.
var (
	_ cmp.StreamSource  = (*Source)(nil)
	_ cmp.Initializer   = (*Source)(nil)
	_ cmp.Resolver      = (*Source)(nil)
	_ cmp.CompleteDoner = (*Source)(nil)
	_ cmp.Closer        = (*Source)(nil)
	_ cmp.EventHandler  = (*Source)(nil)
)
