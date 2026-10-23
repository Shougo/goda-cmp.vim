package input

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func TestGather(t *testing.T) {
	var typ string
	h := &cmptest.Host{Funcs: map[string]func(...any) (any, error){
		"exists":          func(...any) (any, error) { return int64(1), nil },
		"getcmdcompltype": func(...any) (any, error) { return "file", nil },
		"getcompletion": func(a ...any) (any, error) {
			typ = a[1].(string)
			return []any{"dir/", "data.txt"}, nil
		},
	}}
	g, err := New().Gather(context.Background(), h, &cmp.Context{Mode: "c", CmdType: "@", Input: "d"})
	if err != nil {
		t.Fatal(err)
	}
	var ws, abbrs []string
	for _, it := range g.Items {
		ws, abbrs = append(ws, it.Word), append(abbrs, it.Abbr)
	}
	if typ != "file" || g.Start != 0 || !reflect.DeepEqual(ws, []string{"dir", "data.txt"}) || !reflect.DeepEqual(abbrs, []string{"dir/", ""}) {
		t.Errorf("type %q start %d words %q abbrs %q", typ, g.Start, ws, abbrs)
	}
	// Not input() nor "=": nothing.
	g, _ = New().Gather(context.Background(), h, &cmp.Context{Mode: "c", CmdType: ":", Input: "d"})
	if len(g.Items) != 0 {
		t.Errorf(": got %+v", g)
	}
}
