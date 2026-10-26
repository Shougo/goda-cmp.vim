package convertertruncateabbr_test

import (
	"context"
	"testing"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda-cmp.vim/ext/filter/convertertruncateabbr"
)

func TestTruncateAbbr(t *testing.T) {
	f := convertertruncateabbr.New(convertertruncateabbr.Params{MaxWidth: 5})
	in := []cmp.Item{{Word: "日本語日本語日本語"}, {Word: "ab"}}
	out, _ := f.Filter(context.Background(), cmp.FilterInput{Items: in})
	if out[0].Abbr != "日本語日…" || out[1].Abbr != "" {
		t.Errorf("abbrs = %q, %q", out[0].Abbr, out[1].Abbr)
	}
	if in[0].Abbr != "" {
		t.Error("the input slice was modified")
	}
}
