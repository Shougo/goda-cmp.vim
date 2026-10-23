package register

import (
	"context"
	"reflect"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/cmp/cmptest"
)

func host(t *testing.T, regs map[string]map[string]any) *cmptest.Host {
	return &cmptest.Host{Funcs: map[string]func(...any) (any, error){
		"map": func(a ...any) (any, error) {
			if a[1] != "getreginfo(v:val)" {
				t.Errorf("map expr %v", a[1])
			}
			out := []any{}
			for _, r := range a[0].([]any) {
				info, ok := regs[r.(string)]
				if !ok {
					info = map[string]any{}
				}
				out = append(out, info)
			}
			return out, nil
		},
	}}
}

func TestGather(t *testing.T) {
	h := host(t, map[string]map[string]any{
		"0": {"regcontents": []any{"hello world"}, "regtype": "v"},
		"1": {"regcontents": []any{"two", "lines"}, "regtype": "V"},
		"a": {"regcontents": []any{"hello world"}, "regtype": "v"},
	})
	got := func(p Params) []string {
		g, err := New(p).Gather(context.Background(), h, &cmp.Context{Input: "he"})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range g.Items {
			out = append(out, it.Menu+":"+it.Word)
		}
		return out
	}
	// Several lines are left out; the same text once.
	if w := got(Params{Registers: "01a"}); !reflect.DeepEqual(w, []string{"0:hello world"}) {
		t.Errorf("contents: %q", w)
	}
	if w := got(Params{Registers: "01", ExtractWords: true}); !reflect.DeepEqual(w, []string{"0[0]:hello", "0[1]:world", "1[0]:two", "1[1]:lines"}) {
		t.Errorf("words: %q", w)
	}
}
