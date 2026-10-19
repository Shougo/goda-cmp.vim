package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/textproto"
	"strconv"
	"strings"
	"sync"
)

// errConnClosed is returned by calls on a connection whose server is gone.
var errConnClosed = errors.New("lsp: connection closed")

// message is any JSON-RPC 2.0 message: a request (ID and Method), a
// notification (Method only) or a response (ID, and Result or Error).
type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *rpcError        `json:"error,omitempty"`
}

type rpcError struct {
	Code    int64           `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return fmt.Sprintf("lsp: %s (code %d)", e.Message, e.Code) }

// Error codes of JSON-RPC and LSP used here.
const (
	codeMethodNotFound   = -32601
	codeRequestCancelled = -32800
)

type response struct {
	result json.RawMessage
	err    error
}

// conn is a JSON-RPC 2.0 connection with the LSP base protocol framing
// (Content-Length headers). Requests from the server and notifications are
// passed to the handlers; requests are answered with what handleRequest
// returns.
type conn struct {
	w   io.Writer
	wmu sync.Mutex

	handleRequest      func(method string, params json.RawMessage) (any, error)
	handleNotification func(method string, params json.RawMessage)

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan response
	closed  bool
	done    chan struct{}
	err     error // why the connection closed
}

func newConn(w io.Writer) *conn {
	return &conn{
		w:       w,
		pending: map[int64]chan response{},
		done:    make(chan struct{}),
		handleRequest: func(string, json.RawMessage) (any, error) {
			return nil, &rpcError{Code: codeMethodNotFound, Message: "method not found"}
		},
		handleNotification: func(string, json.RawMessage) {},
	}
}

// readLoop reads messages until r fails, then closes the connection. Set the
// handlers before starting it.
func (c *conn) readLoop(r io.Reader) {
	br := bufio.NewReader(r)
	tp := textproto.NewReader(br)
	var err error
	for {
		var body []byte
		body, err = readFrame(tp, br)
		if err != nil {
			break
		}
		var m message
		if jerr := json.Unmarshal(body, &m); jerr != nil {
			continue // a malformed message cannot be answered; skip it
		}
		switch {
		case m.Method != "" && m.ID != nil:
			go c.serveRequest(*m.ID, m.Method, m.Params)
		case m.Method != "":
			c.handleNotification(m.Method, m.Params)
		case m.ID != nil:
			c.deliver(*m.ID, m)
		}
	}
	c.close(err)
}

func readFrame(tp *textproto.Reader, br *bufio.Reader) ([]byte, error) {
	hdr, err := tp.ReadMIMEHeader()
	if err != nil {
		return nil, err
	}
	n, err := strconv.Atoi(strings.TrimSpace(hdr.Get("Content-Length")))
	if err != nil || n < 0 {
		return nil, fmt.Errorf("lsp: bad Content-Length %q", hdr.Get("Content-Length"))
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(br, body); err != nil {
		return nil, err
	}
	return body, nil
}

func (c *conn) send(m message) error {
	m.JSONRPC = "2.0"
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := fmt.Fprintf(c.w, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
		return err
	}
	_, err = c.w.Write(b)
	return err
}

func (c *conn) serveRequest(id json.RawMessage, method string, params json.RawMessage) {
	res, err := c.handleRequest(method, params)
	m := message{ID: &id}
	if err != nil {
		var re *rpcError
		if !errors.As(err, &re) {
			re = &rpcError{Code: -32603, Message: err.Error()}
		}
		m.Error = re
	} else {
		b, merr := json.Marshal(res)
		if merr != nil {
			m.Error = &rpcError{Code: -32603, Message: merr.Error()}
		} else {
			m.Result = b // "null" for a nil result: the field must be present
		}
	}
	_ = c.send(m)
}

func (c *conn) deliver(raw json.RawMessage, m message) {
	id, err := strconv.ParseInt(string(raw), 10, 64)
	if err != nil {
		return // not one of ours (we only use numbers)
	}
	c.mu.Lock()
	ch := c.pending[id]
	delete(c.pending, id)
	c.mu.Unlock()
	if ch == nil {
		return
	}
	if m.Error != nil {
		ch <- response{err: m.Error}
		return
	}
	ch <- response{result: m.Result}
}

// call sends a request and decodes its result into out (which may be nil).
// When ctx ends first, the request is cancelled with $/cancelRequest.
func (c *conn) call(ctx context.Context, method string, params, out any) error {
	p, err := marshalParams(params)
	if err != nil {
		return err
	}
	ch := make(chan response, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return c.closeErr()
	}
	c.nextID++
	id := c.nextID
	c.pending[id] = ch
	c.mu.Unlock()

	rawID := json.RawMessage(strconv.FormatInt(id, 10))
	if err := c.send(message{ID: &rawID, Method: method, Params: p}); err != nil {
		c.drop(id)
		return err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if out == nil || len(r.result) == 0 {
			return nil
		}
		return json.Unmarshal(r.result, out)
	case <-ctx.Done():
		c.drop(id)
		_ = c.notify("$/cancelRequest", map[string]any{"id": id})
		return ctx.Err()
	case <-c.done:
		return c.closeErr()
	}
}

func (c *conn) notify(method string, params any) error {
	p, err := marshalParams(params)
	if err != nil {
		return err
	}
	return c.send(message{Method: method, Params: p})
}

func (c *conn) drop(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *conn) close(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if err == nil || errors.Is(err, io.EOF) {
		err = errConnClosed
	}
	c.err = err
	close(c.done)
	for id, ch := range c.pending {
		ch <- response{err: err}
		delete(c.pending, id)
	}
}

func (c *conn) closeErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return errConnClosed
}

// marshalParams encodes params; nil leaves them out of the message (shutdown
// and exit have none).
func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}
