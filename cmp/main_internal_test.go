package cmp

import (
	"context"
	"strings"
	"testing"

	"github.com/Shougo/goda.vim/sdk"
)

// A binary built from an invalid configuration starts, reports the error
// through status and fails everything else with it.
func TestMainPluginWithBadConfig(t *testing.T) {
	b := New().AddUI(stubUI{}).PatchGlobal(Config{UI: "stub"})
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

	ok := New().AddUI(stubUI{}).PatchGlobal(Config{UI: "stub"}).mainPlugin().Handlers()
	if st, err := ok["status"](ctx, nil, nil); err != nil || st != "" {
		t.Errorf("valid configuration: status = %v, %v", st, err)
	}
}

// stubUI stands in for ext/ui/native, which this package cannot import.
type stubUI struct{}

func (stubUI) Name() string                                      { return "stub" }
func (stubUI) Show(context.Context, sdk.Host, ShowRequest) error { return nil }
func (stubUI) Hide(context.Context, sdk.Host) error              { return nil }

// The UI must be chosen: there is no default, and a name never added is an
// error too.
func TestUIIsRequired(t *testing.T) {
	if err := New().Validate(); err == nil || !strings.Contains(err.Error(), "no ui is set") {
		t.Errorf("no UI: err = %v", err)
	}
	if err := New().PatchGlobal(Config{UI: "native"}).Validate(); err == nil || !strings.Contains(err.Error(), `unknown ui "native"`) {
		t.Errorf("UI not added: err = %v", err)
	}
	if err := New().AddUI(stubUI{}).AddUI(stubUI{}).PatchGlobal(Config{UI: "stub"}).Validate(); err == nil || !strings.Contains(err.Error(), "registered twice") {
		t.Errorf("UI added twice: err = %v", err)
	}
	if err := New().AddUI(stubUI{}).PatchGlobal(Config{UI: "stub"}).Validate(); err != nil {
		t.Errorf("UI added and set: %v", err)
	}
}
