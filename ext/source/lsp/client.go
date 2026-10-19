package lsp

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// client is one language server process serving one root directory.
type client struct {
	p    *Params
	root string

	ready   chan struct{} // closed when initialize finished (or failed)
	initErr error
	caps    serverCapabilities
	enc     string

	cmd    *exec.Cmd
	conn   *conn
	exited chan struct{} // closed when the process is gone

	syncMu sync.Mutex // orders didOpen/didChange
	docs   map[string]*docState

	mu       sync.Mutex
	partials map[string]func(json.RawMessage)
	nextTok  int64
}

type docState struct {
	bufnr, tick int
	version     int
}

// startClient starts the server in the background. Initialization is not
// tied to any completion request: a request that is superseded must not
// abort it, and the first ones may simply time out while the server starts.
func startClient(p *Params, root string) *client {
	cl := &client{
		p:        p,
		root:     root,
		ready:    make(chan struct{}),
		exited:   make(chan struct{}),
		docs:     map[string]*docState{},
		partials: map[string]func(json.RawMessage){},
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), p.InitTimeout)
		defer cancel()
		cl.initErr = cl.start(ctx)
		if cl.initErr != nil {
			log.Printf("lsp %s: %s: %v", p.Name, root, cl.initErr)
			cl.kill()
		}
		close(cl.ready)
	}()
	return cl
}

// wait blocks until the server is initialized.
func (cl *client) wait(ctx context.Context) error {
	select {
	case <-cl.ready:
		return cl.initErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

// alive reports whether the process can still be used (or is starting).
func (cl *client) alive() bool {
	select {
	case <-cl.exited:
		return false
	default:
	}
	select {
	case <-cl.ready:
		return cl.initErr == nil
	default:
		return true
	}
}

func (cl *client) start(ctx context.Context) error {
	cmd := exec.Command(cl.p.Command[0], cl.p.Command[1:]...)
	cmd.Dir = cl.root
	cmd.Env = append(os.Environ(), cl.p.Env...)
	cmd.Stderr = cl.p.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		close(cl.exited)
		return err
	}
	cl.cmd = cmd
	cl.conn = newConn(stdin)
	cl.conn.handleRequest = cl.serverRequest
	cl.conn.handleNotification = cl.serverNotification
	go cl.conn.readLoop(stdout)
	go func() {
		_ = cmd.Wait()
		cl.conn.close(fmt.Errorf("lsp %s: server exited", cl.p.Name))
		close(cl.exited)
	}()

	rootURI := fileURI(cl.root)
	params := map[string]any{
		"processId":  os.Getpid(),
		"clientInfo": map[string]any{"name": "goda-cmp"},
		"rootUri":    rootURI,
		"rootPath":   cl.root,
		"workspaceFolders": []any{
			map[string]any{"uri": rootURI, "name": filepath.Base(cl.root)},
		},
		"capabilities": map[string]any{
			"general": map[string]any{"positionEncodings": []string{encUTF8, encUTF16}},
			"workspace": map[string]any{
				"configuration":    true,
				"workspaceFolders": true,
			},
			"textDocument": map[string]any{
				"synchronization": map[string]any{"didSave": false},
				"completion": map[string]any{
					"contextSupport": true,
					"completionItem": map[string]any{
						"snippetSupport":          false,
						"insertReplaceSupport":    true,
						"labelDetailsSupport":     true,
						"documentationFormat":     []string{"plaintext", "markdown"},
						"resolveSupport":          map[string]any{"properties": []string{"documentation", "detail", "additionalTextEdits"}},
						"insertTextModeSupport":   map[string]any{"valueSet": []int{1}},
						"deprecatedSupport":       true,
						"commitCharactersSupport": false,
					},
					"completionList": map[string]any{
						"itemDefaults": []string{"editRange", "insertTextFormat", "data"},
					},
				},
			},
		},
	}
	if cl.p.InitializationOptions != nil {
		params["initializationOptions"] = cl.p.InitializationOptions
	}
	var res struct {
		Capabilities serverCapabilities `json:"capabilities"`
	}
	if err := cl.conn.call(ctx, "initialize", params, &res); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	cl.caps = res.Capabilities
	switch cl.caps.PositionEncoding {
	case encUTF8, encUTF32:
		cl.enc = cl.caps.PositionEncoding
	default:
		cl.enc = encUTF16
	}
	if err := cl.conn.notify("initialized", map[string]any{}); err != nil {
		return err
	}
	if cl.p.Settings != nil {
		_ = cl.conn.notify("workspace/didChangeConfiguration", map[string]any{"settings": cl.p.Settings})
	}
	return nil
}

// serverRequest answers what the server asks. Nothing here needs the editor.
func (cl *client) serverRequest(method string, params json.RawMessage) (any, error) {
	switch method {
	case "workspace/configuration":
		var p struct {
			Items []struct {
				Section string `json:"section"`
			} `json:"items"`
		}
		_ = json.Unmarshal(params, &p)
		out := make([]any, len(p.Items))
		for i, it := range p.Items {
			out[i] = cl.setting(it.Section)
		}
		return out, nil
	case "workspace/workspaceFolders":
		return []any{map[string]any{"uri": fileURI(cl.root), "name": filepath.Base(cl.root)}}, nil
	case "window/workDoneProgress/create", "client/registerCapability", "client/unregisterCapability":
		return nil, nil
	case "window/showMessageRequest":
		cl.logMessage(params)
		return nil, nil
	}
	return nil, &rpcError{Code: codeMethodNotFound, Message: "unsupported: " + method}
}

// setting looks up a dotted section of Params.Settings.
func (cl *client) setting(section string) any {
	var v any
	b, err := json.Marshal(cl.p.Settings)
	if err != nil || json.Unmarshal(b, &v) != nil {
		return nil
	}
	if section == "" {
		return v
	}
	for _, part := range strings.Split(section, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[part]
	}
	return v
}

func (cl *client) serverNotification(method string, params json.RawMessage) {
	switch method {
	case "$/progress":
		var p struct {
			Token json.RawMessage `json:"token"`
			Value json.RawMessage `json:"value"`
		}
		if json.Unmarshal(params, &p) != nil {
			return
		}
		cl.mu.Lock()
		f := cl.partials[string(p.Token)]
		cl.mu.Unlock()
		if f != nil {
			f(p.Value)
		}
	case "window/showMessage", "window/logMessage":
		cl.logMessage(params)
	}
}

// logMessage puts errors and warnings of the server in the plugin log
// (goda#log()); the rest is noise there.
func (cl *client) logMessage(params json.RawMessage) {
	var p struct {
		Type    int    `json:"type"`
		Message string `json:"message"`
	}
	if json.Unmarshal(params, &p) == nil && p.Type >= 1 && p.Type <= 2 {
		log.Printf("lsp %s: %s", cl.p.Name, p.Message)
	}
}

// sync makes the server's copy of the document current. Vim gives no
// changes, only the whole text, so it is full synchronization; a buffer whose
// changedtick did not move is not sent again.
func (cl *client) sync(uri, languageID string, bufnr, tick int, text func() string) error {
	kind := cl.caps.syncKind()
	if kind == 0 {
		return nil
	}
	cl.syncMu.Lock()
	defer cl.syncMu.Unlock()
	d := cl.docs[uri]
	if d != nil && d.bufnr == bufnr && d.tick == tick {
		return nil
	}
	if d == nil {
		d = &docState{}
		cl.docs[uri] = d
		d.bufnr, d.tick, d.version = bufnr, tick, 1
		return cl.conn.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": languageID, "version": d.version, "text": text(),
			},
		})
	}
	d.bufnr, d.tick = bufnr, tick
	d.version++
	return cl.conn.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": d.version},
		"contentChanges": []any{map[string]any{"text": text()}},
	})
}

// complete asks for completion. onPartial receives items that arrive as
// partial results ($/progress) before the response; the response holds the
// rest.
func (cl *client) complete(ctx context.Context, params map[string]any, onPartial func(completionList)) (completionList, error) {
	cl.mu.Lock()
	cl.nextTok++
	tok := strconv.Quote("goda-cmp/" + strconv.FormatInt(cl.nextTok, 10))
	cl.partials[tok] = func(v json.RawMessage) {
		if l, err := decodeCompletion(v); err == nil {
			onPartial(l)
		}
	}
	cl.mu.Unlock()
	defer func() {
		cl.mu.Lock()
		delete(cl.partials, tok)
		cl.mu.Unlock()
	}()
	params["partialResultToken"] = json.RawMessage(tok)
	var raw json.RawMessage
	if err := cl.conn.call(ctx, "textDocument/completion", params, &raw); err != nil {
		return completionList{}, err
	}
	return decodeCompletion(raw)
}

func (cl *client) resolve(ctx context.Context, item json.RawMessage) (json.RawMessage, error) {
	var out json.RawMessage
	if err := cl.conn.call(ctx, "completionItem/resolve", item, &out); err != nil {
		return nil, err
	}
	if len(out) == 0 || string(out) == "null" {
		return item, nil
	}
	return out, nil
}

func (cl *client) canResolve() bool {
	return cl.caps.CompletionProvider != nil && cl.caps.CompletionProvider.ResolveProvider
}

// shutdown stops the server politely, then by force when ctx ends.
func (cl *client) shutdown(ctx context.Context) {
	select {
	case <-cl.ready:
	case <-ctx.Done():
		cl.kill()
		return
	}
	if cl.initErr == nil {
		sctx, cancel := context.WithTimeout(ctx, time.Second)
		if cl.conn.call(sctx, "shutdown", nil, nil) == nil {
			_ = cl.conn.notify("exit", nil)
		}
		cancel()
	}
	select {
	case <-cl.exited:
	case <-ctx.Done():
		cl.kill()
	case <-time.After(time.Second):
		cl.kill()
	}
}

func (cl *client) kill() {
	if cl.cmd != nil && cl.cmd.Process != nil {
		_ = cl.cmd.Process.Kill()
	}
}
