package vim

import (
	"context"
	"regexp"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

var (
	endFunction   = regexp.MustCompile(`\bendf` + optional("", "unction") + `\b`)
	startFunction = regexp.MustCompile(`\b(?:` + optional("fu", "nction") + `|def)!?[ \t]+`)
	declaration   = regexp.MustCompile(`\b(?:let|const|for|var|final)[ \t]+`)
)

// chunk is how many lines are asked for at once, going up to the function.
const chunk = 500

// localVariables are the variables and arguments of the function the cursor
// (at line lnum) is in, declared above it.
func localVariables(ctx context.Context, h sdk.Host, lnum int) ([]cmp.Item, error) {
	// The lines above the cursor, from first.
	var lines []string
	first := lnum
	start := 0
	for start == 0 && first > 1 {
		from := max(1, first-chunk)
		v, err := h.Call(ctx, "getline", from, first-1)
		if err != nil {
			return nil, err
		}
		lines = append(strs(v), lines...)
		first = from
		end := false
		// Only the new lines are looked at: the others had neither.
		for i := len(strs(v)) - 1; i >= 0; i-- {
			l := lines[i]
			if endFunction.MatchString(l) {
				end = true
				break
			}
			if startFunction.MatchString(l) {
				start = first + i
				break
			}
		}
		if end {
			break
		}
	}
	vars := &variables{index: map[string]int{}}
	if start == 0 {
		return vars.items, nil
	}
	at := func(n int) string { return lines[n-first] }
	vars.analyze(at(start))
	end := lnum - 1
	for n := start + 1; n <= end; n++ {
		l := at(n)
		if declaration.MatchString(l) {
			vars.analyze(l)
		} else if startFunction.MatchString(l) {
			// Skip nested definitions conservatively.
			for m := n; m <= end; m++ {
				if endFunction.MatchString(at(m)) {
					n = m
					break
				}
			}
		}
	}
	return vars.items, nil
}

// variables are the items found, in order.
type variables struct {
	items []cmp.Item
	index map[string]int
}

func (v *variables) set(it cmp.Item) {
	if i, ok := v.index[it.Word]; ok {
		v.items[i] = it
		return
	}
	v.index[it.Word] = len(v.items)
	v.items = append(v.items, it)
}

var (
	continuation   = regexp.MustCompile(`\\[ \t]*$`)
	singleVariable = regexp.MustCompile(`\b(?:let|const|for|var|final)[ \t]+([A-Za-z][[:alnum:]_:]*)(?:[^=]*=[ \t]*(.*))?`)
	multiVariables = regexp.MustCompile(`\b(?:let|const)[ \t]+\[(.*)\][ \t]*=[ \t]*`)
	argSep         = regexp.MustCompile(`[ \t]*,[ \t]*`)
	legacyFunction = regexp.MustCompile(`\b` + optional("fu", "nction") + `!?[ \t]+`)
	defFunction    = regexp.MustCompile(`\bdef!?[ \t]+`)
	argText        = regexp.MustCompile(`^[^(]*\(([^)]*)`)
	wordRe         = regexp.MustCompile(`\w+`)
	varargs        = regexp.MustCompile(`\.\.\.\)`)
)

// analyze adds what a line declares: variables, or the arguments of a
// function.
func (v *variables) analyze(line string) {
	continued := strings.HasSuffix(line, `\`)
	if continued {
		line = continuation.ReplaceAllString(line, "")
	}
	if m := singleVariable.FindStringSubmatch(line); m != nil {
		word, expr := m[1], m[2]
		kind := primaryKind(expr)
		i, ok := v.index[word]
		if !ok {
			v.set(cmp.Item{Word: word, Kind: kind})
		} else if expr != "" && v.items[i].Kind == "" {
			v.items[i].Kind = kind
		}
		return
	}
	if m := multiVariables.FindStringSubmatch(line); m != nil {
		for _, w := range split(m[1]) {
			v.set(cmp.Item{Word: w})
		}
		return
	}
	switch {
	case legacyFunction.MatchString(line):
		args := arguments(line, continued)
		for _, a := range args {
			word, kind := "a:"+wordRe.FindString(a), ""
			if a == "..." {
				word, kind = "a:000", "[]"
			}
			v.set(cmp.Item{Word: word, Kind: kind})
		}
		if varargs.MatchString(line) {
			for n := range 5 {
				kind := ""
				if n == 0 {
					kind = "0"
				}
				v.set(cmp.Item{Word: "a:" + itoa(n), Kind: kind})
			}
		}
	case defFunction.MatchString(line):
		for _, a := range arguments(line, continued) {
			v.set(cmp.Item{Word: wordRe.FindString(a)})
		}
	}
}

// arguments are the arguments of the function declared on line.
func arguments(line string, continued bool) []string {
	text := ""
	if m := argText.FindStringSubmatch(line); m != nil {
		text = m[1]
	}
	if continued {
		text += " "
	}
	return split(text)
}

// split is split(s, '\s*,\s*'): no empty parts.
func split(s string) []string {
	var out []string
	for _, p := range argSep.Split(s, -1) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var (
	floatValue  = regexp.MustCompile(`^(?:[ \t]*\+)?[ \t]*\d+\.\d+`)
	numberValue = regexp.MustCompile(`^(?:[ \t]*\+)?[ \t]*\d+`)
	stringValue = regexp.MustCompile(`^(?:[ \t]*\.)?[ \t]*["']`)
	funcValue   = regexp.MustCompile(`\bfunction\(`)
	listValue   = regexp.MustCompile(`^(?:[ \t]*\+)?[ \t]*\[`)
	dictValue   = regexp.MustCompile(`^[ \t]*\{|^\.[A-Za-z_][[:alnum:]_:]*`)
)

// primaryKind is the first kind the value of a variable looks like.
func primaryKind(expr string) string {
	for _, k := range []struct {
		re   *regexp.Regexp
		kind string
	}{
		{floatValue, "."}, {numberValue, "0"}, {stringValue, `""`},
		{funcValue, "()"}, {listValue, "[]"}, {dictValue, "{}"},
	} {
		if k.re.MatchString(expr) {
			return k.kind
		}
	}
	return ""
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
