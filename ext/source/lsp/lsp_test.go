package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/textproto"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
	"github.com/Shougo/goda.vim/sdk"
)

// The test binary doubles as a fake language server: with GODA_LSP_FAKE set
// it serves LSP on stdin/stdout instead of running the tests.
func TestMain(m *testing.M) {
	if os.Getenv("GODA_LSP_FAKE") != "" {
		fakeServer(os.Stdin, os.Stdout)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeServer speaks just enough LSP: UTF-16 positions, trigger character
// ".", partial results, resolve that adds documentation and an import edit,
// and a workspace/configuration request whose answer it reports back as a
// completion item.
func fakeServer(r io.Reader, w io.Writer) {
	br := bufio.NewReader(r)
	tp := textproto.NewReader(br)
	var wmu sync.Mutex
	send := func(m map[string]any) {
		m["jsonrpc"] = "2.0"
		b, _ := json.Marshal(m)
		wmu.Lock()
		fmt.Fprintf(w, "Content-Length: %d\r\n\r\n%s", len(b), b)
		wmu.Unlock()
	}
	var (
		mu      sync.Mutex
		text    string
		version float64
		setting any
		opens   int // didOpen notifications
		closes  int // didClose notifications
		// A completion that came before the configuration answer waits
		// for it, so that the test does not depend on timing.
		waiting func()
	)
	for {
		body, err := readFrame(tp, br)
		if err != nil {
			return
		}
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		_ = json.Unmarshal(body, &m)
		var p map[string]any
		_ = json.Unmarshal(m.Params, &p)
		reply := func(v any) { send(map[string]any{"id": m.ID, "result": v}) }

		switch m.Method {
		case "":
			// The answer to our workspace/configuration request.
			var res []any
			_ = json.Unmarshal(m.Result, &res)
			mu.Lock()
			if len(res) > 0 {
				setting = res[0]
			}
			f := waiting
			waiting = nil
			mu.Unlock()
			if f != nil {
				f()
			}
		case "initialize":
			reply(map[string]any{"capabilities": map[string]any{
				"positionEncoding": "utf-16",
				"textDocumentSync": 1,
				"completionProvider": map[string]any{
					"triggerCharacters": []string{"."},
					"resolveProvider":   true,
				},
			}})
		case "initialized":
			send(map[string]any{"id": 99, "method": "workspace/configuration",
				"params": map[string]any{"items": []any{map[string]any{"section": "fake.flavour"}}}})
		case "textDocument/didOpen":
			td := p["textDocument"].(map[string]any)
			mu.Lock()
			text, version = td["text"].(string), td["version"].(float64)
			opens++
			mu.Unlock()
		case "textDocument/didClose":
			mu.Lock()
			closes++
			mu.Unlock()
		case "textDocument/didChange":
			td := p["textDocument"].(map[string]any)
			ch := p["contentChanges"].([]any)[0].(map[string]any)
			mu.Lock()
			text, version = ch["text"].(string), td["version"].(float64)
			mu.Unlock()
		case "textDocument/completion":
			complete := func() {
				pos := p["position"].(map[string]any)
				line, char := int(pos["line"].(float64)), int(pos["character"].(float64))
				mu.Lock()
				lines := strings.Split(text, "\n")
				cfg := fmt.Sprint(setting)
				ver := version
				docs := fmt.Sprintf("d%d_%d", opens, closes)
				mu.Unlock()
				// The word before the cursor, in UTF-16 units.
				cur := []rune(lines[line])
				u, i := 0, 0
				for ; i < len(cur) && u < char; i++ {
					u += len(string(utf16Len(cur[i])))
				}
				ws := i
				for ws > 0 && (cur[ws-1] == '_' || cur[ws-1] >= 'a' && cur[ws-1] <= 'z') {
					ws--
				}
				startU := 0
				for _, r := range cur[:ws] {
					startU += len(string(utf16Len(r)))
				}
				rng := map[string]any{
					"start": map[string]any{"line": line, "character": startU},
					"end":   map[string]any{"line": line, "character": char},
				}
				// The rest of the word after the cursor, for an
				// insert/replace edit.
				we, weU := i, char
				for we < len(cur) && cur[we] >= 'a' && cur[we] <= 'z' {
					weU += len(string(utf16Len(cur[we])))
					we++
				}
				replace := map[string]any{
					"start": map[string]any{"line": line, "character": startU},
					"end":   map[string]any{"line": line, "character": weU},
				}
				trig := fmt.Sprint(p["context"].(map[string]any)["triggerKind"])
				if tok, ok := p["partialResultToken"]; ok {
					send(map[string]any{"method": "$/progress", "params": map[string]any{
						"token": tok, "value": []any{map[string]any{"label": "early", "sortText": "0"}},
					}})
				}
				reply(map[string]any{
					"isIncomplete": false,
					"itemDefaults": map[string]any{"editRange": rng, "data": "default-data"},
					"items": []any{
						map[string]any{"label": "alpha", "kind": 3, "detail": "func()", "sortText": "1"},
						map[string]any{"label": "beta", "sortText": "2", "insertTextFormat": 2, "textEditText": "beta(${1:x})$0"},
						map[string]any{"label": "cfg", "textEditText": "cfg_" + cfg, "sortText": "3"},
						map[string]any{"label": "version", "textEditText": fmt.Sprintf("v%d", int(ver)), "sortText": "4"},
						map[string]any{"label": "trigger", "textEditText": "k" + trig, "sortText": "5"},
						map[string]any{"label": "gamma", "sortText": "6", "textEdit": map[string]any{
							"newText": "gamma", "insert": rng, "replace": replace,
						}},
						map[string]any{"label": "docs", "textEditText": docs, "sortText": "7"},
					},
				})
			}
			mu.Lock()
			if setting == nil {
				waiting = complete
				mu.Unlock()
				continue
			}
			mu.Unlock()
			complete()
		case "completionItem/resolve":
			p["documentation"] = map[string]any{"kind": "markdown", "value": "docs of " + p["label"].(string) + " " + fmt.Sprint(p["data"])}
			p["additionalTextEdits"] = []any{map[string]any{
				"range":   map[string]any{"start": map[string]any{"line": 0, "character": 0}, "end": map[string]any{"line": 0, "character": 0}},
				"newText": "import \"x\"\n",
			}}
			reply(p)
		case "shutdown":
			reply(nil)
		case "exit":
			return
		}
	}
}

// utf16Len returns a string whose byte length is the rune's UTF-16 length.
func utf16Len(r rune) []byte {
	if r >= 0x10000 {
		return []byte{0, 0}
	}
	return []byte{0}
}

// newFake starts the fake server for a file in a directory under the root
// (marked by "marker"), and an editor showing that file.
func newFake(t *testing.T, p Params) (*Source, *cmptest.Editor) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "marker"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	p.Name = "fake"
	p.Command = []string{exe}
	p.Env = []string{"GODA_LSP_FAKE=1"}
	p.RootMarkers = []string{"marker"}
	p.Settings = map[string]any{"fake": map[string]any{"flavour": "mint"}}
	p.InitTimeout = 10 * time.Second
	s := New(p)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Close(ctx)
	})
	if err := s.Init(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	return s, cmptest.NewEditor(filepath.Join(dir, "sub", "f.txt"))
}

func gather(t *testing.T, s *Source, h sdk.Host, c *cmp.Context) (cmp.Gathered, int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	emits := 0
	var last cmp.Gathered
	err := s.GatherStream(ctx, h, c, func(g cmp.Gathered) error {
		emits++
		last = g
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return last, emits
}

func words(g cmp.Gathered) []string {
	var w []string
	for _, it := range g.Items {
		w = append(w, it.Word)
	}
	return w
}

func item(t *testing.T, g cmp.Gathered, word string) cmp.Item {
	t.Helper()
	for _, it := range g.Items {
		if it.Word == word {
			return it
		}
	}
	t.Fatalf("no %q in %q", word, words(g))
	return cmp.Item{}
}

func TestFakeServer(t *testing.T) {
	s, ed := newFake(t, Params{})
	h := ed.Host()
	// "😀" is 4 bytes and 2 UTF-16 units: positions must be converted.
	ed.SetLines(1, "😀 x.al", "second")
	c := &cmp.Context{Bufnr: 1, Tick: 3, Lnum: 1, Col: 12, Input: "😀 x.al", Filetype: "text"}

	g, emits := gather(t, s, h, c)
	if emits != 2 {
		t.Errorf("emits = %d, want 2 (partial result, then all)", emits)
	}
	if g.Start != len("😀 x.") {
		t.Errorf("Start = %d, want %d", g.Start, len("😀 x."))
	}
	want := []string{"early", "alpha", "beta(x)", "cfg_mint", "v1", "k1", "gamma", "d1_0"}
	if !reflect.DeepEqual(words(g), want) {
		t.Errorf("words = %q, want %q", words(g), want)
	}
	alpha := item(t, g, "alpha")
	if alpha.Kind != "Function" || alpha.Info != "func()" || alpha.SortText != "1" {
		t.Errorf("alpha = %+v", alpha)
	}
	if b := item(t, g, "beta(x)"); b.Abbr != "beta" {
		t.Errorf("beta abbr = %q", b.Abbr)
	}

	// The same changedtick sends nothing; a new one sends version 2.
	ed.SetLines(1, "😀 x.al", "changed")
	if g, _ := gather(t, s, h, c); words(g)[4] != "v1" {
		t.Errorf("same tick: %q", words(g))
	}
	c.Tick = 4
	if g, _ := gather(t, s, h, c); words(g)[4] != "v2" {
		t.Errorf("new tick: %q", words(g))
	}

	// Resolve: documentation, with the item's default data sent back.
	ctx := context.Background()
	it, err := s.Resolve(ctx, h, cmp.DoneArgs{Context: c, Item: alpha})
	if err != nil {
		t.Fatal(err)
	}
	if it.Info != "func()\n\ndocs of alpha default-data" {
		t.Errorf("resolved info = %q", it.Info)
	}

	// Accepting an unresolved item resolves it and applies its import edit
	// above the cursor; the cursor stays on its text.
	ed.SetLines(1, "😀 x.beta(x)", "second")
	ed.SetCursor(1, len("😀 x.beta(x)")+1)
	if err := s.OnCompleteDone(ctx, h, cmp.DoneArgs{Context: c, Item: item(t, g, "beta(x)")}); err != nil {
		t.Fatal(err)
	}
	if want := []string{`import "x"`, "😀 x.beta(x)", "second"}; !reflect.DeepEqual(ed.Lines(1), want) {
		t.Errorf("lines = %q, want %q", ed.Lines(1), want)
	}
	if l, col := ed.Cursor(); l != 2 || col != len("😀 x.beta(x)")+1 {
		t.Errorf("cursor = %d, %d", l, col)
	}
}

// Entering a buffer starts the server and opens the document before any
// completion; deleting it closes the document. Buffers that are not files are
// left alone.
func TestEvents(t *testing.T) {
	s, ed := newFake(t, Params{})
	h := ed.Host()
	path, _ := h.Call(context.Background(), "bufname", 1)
	ev := cmp.Event{Name: "BufEnter", Bufnr: 1, Tick: 1, Filetype: "text", Path: path.(string)}
	send := func(ev cmp.Event) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.OnEvent(ctx, h, ev); err != nil {
			t.Fatal(err)
		}
	}

	help := ev
	help.Buftype, help.Path = "help", "/doc/help.txt"
	send(help)
	if n := len(s.clients); n != 0 {
		t.Fatalf("a help buffer started %d servers", n)
	}

	ed.SetLines(1, "x.al")
	send(ev)
	// Same changedtick: completion does not open it again.
	g, _ := gather(t, s, h, &cmp.Context{Bufnr: 1, Tick: 1, Lnum: 1, Input: "x.al"})
	if !slices.Contains(words(g), "d1_0") {
		t.Errorf("after BufEnter: %q, want d1_0", words(g))
	}
	ev.Name = "BufDelete"
	send(ev)
	ev.Name = "BufWipeout" // a second close is not sent
	send(ev)
	g, _ = gather(t, s, h, &cmp.Context{Bufnr: 1, Tick: 1, Lnum: 1, Input: "x.al"})
	if !slices.Contains(words(g), "d2_1") {
		t.Errorf("after BufDelete: %q, want d2_1 (closed once, opened again)", words(g))
	}
	if specs := s.Events(); len(specs) != 1 || len(specs[0].Filetypes) != 0 {
		t.Errorf("Events() = %+v", specs)
	}
}

// Whether to ask is the engine's business (forceCompletionPattern); the
// server learns that it was asked because of its trigger character.
func TestTriggerKind(t *testing.T) {
	s, ed := newFake(t, Params{})
	ed.SetLines(1, "x.")
	g, _ := gather(t, s, ed.Host(), &cmp.Context{Bufnr: 1, Tick: 1, Lnum: 1, Input: "x."})
	if !slices.Contains(words(g), "k2") {
		t.Errorf("after the trigger character: %q, want k2 (TriggerCharacter)", words(g))
	}
	g, _ = gather(t, s, ed.Host(), &cmp.Context{Bufnr: 1, Tick: 1, Lnum: 1, Input: "x.", Manual: true})
	if !slices.Contains(words(g), "k1") {
		t.Errorf("manual: %q, want k1 (Invoked)", words(g))
	}
	s.p.Filetypes = []string{"go"}
	if g, _ := gather(t, s, ed.Host(), &cmp.Context{Bufnr: 1, Tick: 1, Lnum: 1, Input: "x.", Filetype: "text"}); len(g.Items) != 0 {
		t.Errorf("%d items for another filetype", len(g.Items))
	}
}

// An insert/replace edit: "replace" removes the rest of the word after the
// cursor when the item is accepted, "insert" keeps it.
func TestConfirmBehavior(t *testing.T) {
	for _, c := range []struct {
		behavior, want string
	}{
		{"", "x.gammaxyz"},
		{"insert", "x.gammaxyz"},
		{"replace", "x.gamma"},
	} {
		t.Run(c.behavior, func(t *testing.T) {
			s, ed := newFake(t, Params{ConfirmBehavior: c.behavior})
			h := ed.Host()
			ed.SetLines(1, "x.gaxyz")
			ctx := &cmp.Context{Bufnr: 1, Tick: 1, Lnum: 1, Input: "x.ga"}
			g, _ := gather(t, s, h, ctx)
			// What Vim does on accept: the word replaces the typed text.
			ed.SetLines(1, "x.gammaxyz")
			ed.SetCursor(1, len("x.gamma")+1)
			if err := s.OnCompleteDone(context.Background(), h, cmp.DoneArgs{Context: ctx, Item: item(t, g, "gamma")}); err != nil {
				t.Fatal(err)
			}
			if lines := ed.Lines(1); lines[len(lines)-1] != c.want {
				t.Errorf("line = %q, want %q", lines[len(lines)-1], c.want)
			}
			if l, col := ed.Cursor(); col != len("x.gamma")+1 || ed.Lines(1)[l-1] != c.want {
				t.Errorf("cursor = %d, %d", l, col)
			}
		})
	}
	s := New(Params{Command: []string{os.Args[0]}, ConfirmBehavior: "merge"})
	if err := s.Init(context.Background(), nil); err == nil {
		t.Error("Init accepted an unknown ConfirmBehavior")
	}
}

// Items that start left of the others: every word gets the text in between,
// and the popup shows the word without it.
func TestAlignedStarts(t *testing.T) {
	cl := &client{enc: encUTF8}
	s := New(Params{})
	c := &cmp.Context{Lnum: 1, Input: "x.pr"}
	raws := []json.RawMessage{
		json.RawMessage(`{"label":"print!","textEdit":{"range":{"start":{"line":0,"character":0},"end":{"line":0,"character":4}},"newText":"print(x)"}}`),
		json.RawMessage(`{"label":"Println"}`),
	}
	g := s.gathered(cl, c, 2, "", raws, nil)
	if g.Start != 0 || !reflect.DeepEqual(words(g), []string{"print(x)", "x.Println"}) {
		t.Fatalf("start %d, words %q", g.Start, words(g))
	}
	if g.Items[0].Abbr != "print!" || g.Items[1].Abbr != "Println" {
		t.Errorf("abbr = %q, %q", g.Items[0].Abbr, g.Items[1].Abbr)
	}
}

func TestInitMissingServer(t *testing.T) {
	s := New(Params{Command: []string{"no-such-language-server-xyz"}})
	if err := s.Init(context.Background(), nil); err == nil {
		t.Error("Init succeeded without a server")
	}
}

func TestText(t *testing.T) {
	line := "a😀b"
	for _, c := range []struct {
		enc   string
		units int
		char  int
		byte_ int
	}{
		{encUTF8, 6, 5, 5},
		{encUTF16, 4, 3, 5},
		{encUTF32, 3, 2, 5},
	} {
		if u := units(line, c.enc); u != c.units {
			t.Errorf("units(%s) = %d, want %d", c.enc, u, c.units)
		}
		if b := byteOffset(line, c.char, c.enc); b != c.byte_ {
			t.Errorf("byteOffset(%s, %d) = %d, want %d", c.enc, c.char, b, c.byte_)
		}
	}
	for in, want := range map[string]string{
		"f($1)$0":            "f()",
		"f(${1:x}, ${2:y})":  "f(x, y)",
		"${1|one,two|} end":  "one end",
		`a\$b`:               "a$b",
		"${1:outer ${2:in}}": "outer in",
		"${TM_FILENAME:f}":   "f",
	} {
		if got := stripSnippet(in); got != want {
			t.Errorf("stripSnippet(%q) = %q, want %q", in, got, want)
		}
	}
	if u := fileURI("/a b/c.go"); u != "file:///a%20b/c.go" {
		t.Errorf("fileURI = %q", u)
	}
}

func TestApplyEdits(t *testing.T) {
	ed := cmptest.NewEditor("f.go", "package p", "", "func f() { fmt.P }")
	ed.SetCursor(3, 17)
	edits := []textEdit{
		{Range: lspRange{Start: position{0, 9}, End: position{0, 9}}, NewText: "\n\nimport \"fmt\""},
		{Range: lspRange{Start: position{2, 0}, End: position{2, 4}}, NewText: "func"},
		{Range: lspRange{Start: position{3, 0}, End: position{3, 0}}, NewText: "// end\n"},
	}
	if err := applyEdits(context.Background(), ed.Host(), 1, encUTF8, edits); err != nil {
		t.Fatal(err)
	}
	want := []string{"package p", "", `import "fmt"`, "", "func f() { fmt.P }", "// end"}
	if !reflect.DeepEqual(ed.Lines(1), want) {
		t.Errorf("lines = %q, want %q", ed.Lines(1), want)
	}
	if l, col := ed.Cursor(); l != 5 || col != 17 {
		t.Errorf("cursor = %d, %d, want 5, 17", l, col)
	}
}
