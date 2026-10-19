package lsp

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/Shougo/goda.vim/sdk"
)

// applyEdits applies text edits (additionalTextEdits) to a buffer with Vim's
// buffer functions, last edit first so that earlier positions stay valid,
// and keeps the cursor on the text it was on.
//
// The edits refer to the text the server had when it answered; the user has
// since accepted the item, which changed the cursor line only. LSP forbids
// additional edits that touch the completed text, so this is safe.
func applyEdits(ctx context.Context, h sdk.Host, bufnr int, enc string, edits []textEdit) error {
	edits = append([]textEdit(nil), edits...)
	sort.SliceStable(edits, func(i, j int) bool {
		a, b := edits[i].Range.Start, edits[j].Range.Start
		return a.Line > b.Line || a.Line == b.Line && a.Character > b.Character
	})

	v, err := h.Eval(ctx, "[bufnr('%'), line('.'), col('.')]")
	if err != nil {
		return err
	}
	cur, _ := v.([]any)
	if len(cur) != 3 {
		return fmt.Errorf("lsp: unexpected cursor answer %v", v)
	}
	current := toInt(cur[0]) == bufnr
	curLine, curCol := toInt(cur[1])-1, toInt(cur[2])-1 // 0-based line, byte
	newLine, newCol := curLine, curCol

	for _, e := range edits {
		sl, el := e.Range.Start.Line, e.Range.End.Line
		if el < sl {
			continue
		}
		raw, err := h.Call(ctx, "getbufline", bufnr, sl+1, el+1)
		if err != nil {
			return err
		}
		got, _ := raw.([]any)
		old := make([]string, el-sl+1)
		for i := range old {
			if i < len(got) {
				old[i], _ = got[i].(string)
			}
		}
		first, last := old[0], old[len(old)-1]
		sb := byteOffset(first, e.Range.Start.Character, enc)
		eb := byteOffset(last, e.Range.End.Character, enc)
		suffix := last[eb:]
		repl := strings.Split(strings.ReplaceAll(first[:sb]+e.NewText+suffix, "\r\n", "\n"), "\n")

		// Lines past the end of the buffer (an insertion at the very end)
		// do not exist yet: only the lines that do are replaced. The line
		// after the last newline of a file is not a buffer line either.
		have := min(len(got), len(old))
		if have < len(old) && len(repl) > 1 && repl[len(repl)-1] == "" {
			repl = repl[:len(repl)-1]
		}
		common := min(have, len(repl))
		if common > 0 {
			if _, err := h.Call(ctx, "setbufline", bufnr, sl+1, repl[:common]); err != nil {
				return err
			}
		}
		if len(repl) > common {
			if _, err := h.Call(ctx, "appendbufline", bufnr, sl+common, repl[common:]); err != nil {
				return err
			}
		} else if have > common {
			if _, err := h.Call(ctx, "deletebufline", bufnr, sl+common+1, sl+have); err != nil {
				return err
			}
		}

		// Follow the cursor when the edit ends before it.
		switch {
		case el < newLine:
			newLine += len(repl) - (el - sl + 1)
		case el == newLine && eb <= newCol:
			newCol = len(repl[len(repl)-1]) - len(suffix) + (newCol - eb)
			newLine = sl + len(repl) - 1
		}
	}
	if current && (newLine != curLine || newCol != curCol) {
		if _, err := h.Call(ctx, "cursor", newLine+1, newCol+1); err != nil {
			return err
		}
	}
	return nil
}

// deleteAfterCursor removes tail from right after the cursor, if the text
// there still is tail and the buffer is the current one (the cursor belongs
// to it).
func deleteAfterCursor(ctx context.Context, h sdk.Host, bufnr int, tail string) error {
	v, err := h.Eval(ctx, "[bufnr('%'), line('.'), col('.'), getline('.')]")
	if err != nil {
		return err
	}
	cur, _ := v.([]any)
	if len(cur) != 4 || toInt(cur[0]) != bufnr {
		return nil
	}
	lnum, col := toInt(cur[1]), toInt(cur[2])-1
	line, _ := cur[3].(string)
	if col < 0 || col > len(line) || !strings.HasPrefix(line[col:], tail) {
		return nil
	}
	_, err = h.Call(ctx, "setbufline", bufnr, lnum, line[:col]+line[col+len(tail):])
	return err
}

func toInt(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	case bool:
		if n {
			return 1
		}
	}
	return 0
}
