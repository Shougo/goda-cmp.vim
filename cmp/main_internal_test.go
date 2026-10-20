package cmp

import (
	"context"
	"strings"
	"testing"
)

// A binary built from an invalid configuration starts, reports the error
// through status and fails everything else with it.
func TestMainPluginWithBadConfig(t *testing.T) {
	b := New()
	b.PatchGlobal(Config{SourceOptions: map[string]SourceOption{"_": {Matchers: []string{"matcher_head"}}}})
	p := b.mainPlugin()
	if p.Name() != PluginName {
		t.Fatalf("name = %q", p.Name())
	}
	hs := p.Handlers()
	ctx := context.Background()
	st, err := hs["status"](ctx, nil, nil)
	if err != nil || !strings.Contains(st.(string), `unknown filter "matcher_head"`) {
		t.Errorf("status = %v, %v", st, err)
	}
	for _, m := range []string{"complete", "options", "events"} {
		if _, err := hs[m](ctx, nil, nil); err == nil || !strings.Contains(err.Error(), "unknown filter") {
			t.Errorf("%s: err = %v", m, err)
		}
	}

	ok := New().mainPlugin().Handlers()
	if st, err := ok["status"](ctx, nil, nil); err != nil || st != "" {
		t.Errorf("valid configuration: status = %v, %v", st, err)
	}
}
