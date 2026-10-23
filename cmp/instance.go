package cmp

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sync"
)

// Configurable is implemented by extensions whose parameters can be set from
// Vim script, as ddc.vim's sourceParams, filterParams and uiParams: WithParams
// returns the extension with params laid over the ones it was built with. The
// result must be of the same kind (a Source for a source, ...). PatchParams
// does the work for a Params struct with JSON tags:
//
//	func (s source) WithParams(p cmp.Params) (any, error) {
//		np, err := cmp.PatchParams(s.p, p)
//		if err != nil {
//			return nil, err
//		}
//		return New(np), nil
//	}
//
// The engine keeps one value per set of parameters, so WithParams is called
// once for each. The value answers the requests of the source (Gather,
// Resolve, OnCompleteDone, Preview), and is initialized and closed on its own
// (Initializer, Closer); the buffer events (EventHandler) reach only the
// value the configuration added.
type Configurable interface {
	WithParams(p Params) (any, error)
}

// PatchParams returns base with the fields named in p (by their JSON names)
// set from p. A name base does not have, or a value of the wrong type, is an
// error.
func PatchParams[T any](base T, p Params) (T, error) {
	out := base
	if len(p) == 0 {
		return out, nil
	}
	b, err := json.Marshal(p)
	if err != nil {
		return base, err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&out); err != nil && err != io.EOF {
		return base, fmt.Errorf("params: %w", err)
	}
	return out, nil
}

// instances keeps the values made by WithParams.
type instances struct {
	mu   sync.Mutex
	vals map[string]any
	// order of creation, for Close
	order []string
}

// lookup finds the extension named name: an added one, or an alias of one
// (aliases maps the alias to the name it stands for). With params, it is the
// value WithParams made for them. key identifies the value: the added name
// without params.
func lookup[T any](in *instances, kind string, reg map[string]T, aliases map[string]string, name string, params Params) (v T, key string, err error) {
	base, ok := reg[name]
	baseName := name
	if !ok {
		if b, ok2 := aliases[name]; ok2 {
			base, ok = reg[b]
			baseName = b
		}
	}
	if !ok {
		return v, "", fmt.Errorf("unknown %s %q", kind, name)
	}
	if len(params) == 0 {
		return base, baseName, nil
	}
	pj, err := json.Marshal(params) // map keys are sorted: a stable key
	if err != nil {
		return v, "", err
	}
	key = kind + "\x00" + baseName + "\x00" + string(pj)
	in.mu.Lock()
	defer in.mu.Unlock()
	if got, ok := in.vals[key]; ok {
		return got.(T), key, nil
	}
	c, ok := any(base).(Configurable)
	if !ok {
		return v, "", fmt.Errorf("%s %q takes no params (it does not implement cmp.Configurable)", kind, name)
	}
	made, err := c.WithParams(params)
	if err != nil {
		return v, "", fmt.Errorf("%s %q: %w", kind, name, err)
	}
	t, ok := made.(T)
	if !ok {
		return v, "", fmt.Errorf("%s %q: WithParams returned %T", kind, name, made)
	}
	if in.vals == nil {
		in.vals = map[string]any{}
	}
	in.vals[key] = t
	in.order = append(in.order, key)
	return t, key, nil
}

// all returns the values made so far, in the order they were made.
func (in *instances) all() []any {
	in.mu.Lock()
	defer in.mu.Unlock()
	out := make([]any, 0, len(in.order))
	for _, k := range in.order {
		out = append(out, in.vals[k])
	}
	return out
}
