package mocword

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
)

func TestExtractWords(t *testing.T) {
	for _, tc := range []struct {
		in       string
		sentence string
		offset   int
	}{
		{"input", "input", 0},
		{"UPPER_CASE_INPUT", "UPPER CASE INPUT", 11},
		{"camelCaseInput", "camel Case Input", 9},
		{"_snake_case_input", "snake case input", 12},
		{"_unfinished_input_", "unfinished input ", 18},
		{"unfinishedI", "unfinished I", 10},
		{"_i", "i", 1},
	} {
		s, o := extractWords(tc.in)
		if s != tc.sentence || o != tc.offset {
			t.Errorf("extractWords(%q) = %q, %d; want %q, %d", tc.in, s, o, tc.sentence, tc.offset)
		}
	}
}

// A fake mocword: answers each query with two words.
func TestGather(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("uses a shell script")
	}
	cmd := filepath.Join(t.TempDir(), "mocword")
	script := "#!/bin/sh\nwhile read -r q; do echo \"is was\"; done\n"
	if err := os.WriteFile(cmd, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	src := New(Params{CommandPath: cmd}).(source)
	ctx := context.Background()
	if err := src.Init(ctx, nil); err != nil {
		t.Fatal(err)
	}
	defer src.Close(ctx)
	g, err := src.Gather(ctx, nil, &cmp.Context{Input: "this camelCaseI"})
	if err != nil {
		t.Fatal(err)
	}
	var ws []string
	for _, it := range g.Items {
		ws = append(ws, it.Word)
	}
	// "camelCaseI": the query is "camel Case I"; "camelCase" stays in front.
	if g.Start != 5 || !reflect.DeepEqual(ws, []string{"camelCaseis", "camelCasewas"}) {
		t.Errorf("start %d words %q", g.Start, ws)
	}
}
