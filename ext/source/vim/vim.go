// Package vim is a completion source for Vim script: variables, options,
// functions, mappings, features, environment variables... with the help of
// the item previewed (cmp.Previewer). It is a port of ddc-source-vim
// (https://github.com/Shougo/ddc-source-vim). It has no Vim script: the
// lines, the options and the help files it reads are asked for, and parsed
// here.
//
// It differs from the original in three fixes: a line of only spaces goes on
// from the line above (the original took "[\s\\]" for "s" or a backslash),
// the local variables are found past the functions defined inside the
// function (the original stopped at their end), and the special arguments
// of the mapping commands (<buffer>, <silent>, ...) are offered (the original
// read the table of contents of the help for them).
package vim

import (
	"context"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

type source struct{ c *cache }

// cache is what is read once: the options, the mappings and keys, and the
// filetypes.
type cache struct {
	mu        sync.Mutex
	options   []cmp.Item
	maps      []cmp.Item
	filetypes []cmp.Item
}

// New returns the source, named "vim".
func New() cmp.Source { return source{&cache{}} }

func (source) Name() string { return "vim" }

// keyword is what is completed, before the cursor: a variable, an option,
// a <Plug> mapping, a key, a function or an environment variable.
var keyword = regexp.MustCompile(`(?:` +
	`[a-z]:(?:[a-zA-Z_][a-zA-Z0-9_]*)?` +
	`|&(?:[gl]:)?[a-zA-Z0-9_:]*` +
	`|<Plug>\([^)]*\)?` +
	`|<[a-zA-Z][a-zA-Z0-9_-]*>?` +
	`|[a-zA-Z_][a-zA-Z0-9_:#]*[!(]?` +
	`|\$[a-zA-Z_][a-zA-Z0-9_]*` +
	`)$`)

var comment = regexp.MustCompile(`^\s*"`)

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	none := cmp.Gathered{Start: len(c.Input)}
	v, err := h.Eval(ctx, "[line('.'), getline(max([1, line('.') - 10]), line('.') - 1), exists('*getcompletiontype')]")
	if err != nil {
		return cmp.Gathered{}, err
	}
	l, _ := v.([]any)
	if len(l) != 3 {
		return none, nil
	}
	lnum := toInt(l[0])
	cur := c.Input
	if c.Mode != "c" {
		cur = curText(c.Input, strs(l[1]))
	}
	if comment.MatchString(cur) {
		return none, nil
	}
	loc := keyword.FindStringIndex(c.Input)
	if loc == nil {
		return none, nil
	}
	start := loc[0]
	ctype := ""
	if toInt(l[2]) == 1 {
		v, err := h.Call(ctx, "getcompletiontype", cur)
		if err != nil {
			return cmp.Gathered{}, err
		}
		ctype, _ = v.(string)
	} else {
		ctype = completionType(cur)
	}
	items, err := s.gather(ctx, h, cur, ctype, c.Input[start:], lnum)
	if err != nil {
		return cmp.Gathered{}, err
	}
	out := make([]cmp.Item, 0, len(items))
	for _, it := range items {
		// The word as the source made it, for Preview (the engine may put
		// what other sources start with in front of Word).
		it.UserData = it.Word
		if it.Word != "" {
			out = append(out, it)
		}
	}
	return cmp.Gathered{Start: start, Items: out}, nil
}

// Preview shows the help of the item, when there is any, as the original.
func (source) Preview(ctx context.Context, h sdk.Host, a cmp.DoneArgs) (cmp.Preview, error) {
	word, ok := a.Item.UserData.(string)
	if !ok {
		word = a.Item.Word
	}
	v, err := h.Call(ctx, "getcompletion", word, "help")
	if err != nil {
		return cmp.Preview{}, err
	}
	if l, _ := v.([]any); len(l) == 0 {
		return cmp.Preview{Kind: cmp.PreviewEmpty}, nil
	}
	return cmp.Preview{Kind: cmp.PreviewHelp, Tag: word}, nil
}

// The patterns of the original, in Go's syntax. Vim's "\<" and "\>" are
// "\b" here, and its "\s" is a space or a tab.
var (
	// A line that goes on from the line above.
	continuedText = regexp.MustCompile(`^[ \t]*[\\(\[,|].*(?:<(?:let|const|for|var|final|` + optional("fu", "nction") + `|def)\b|=)`)
	// Only spaces and backslashes.
	emptyText    = regexp.MustCompile(`^[ \t\\]*$`)
	nonBlank     = regexp.MustCompile(`[^ \t]`)
	lineContinue = regexp.MustCompile(`^[ \t]*\\`)
	bar          = regexp.MustCompile(`[ \t]+\|[ \t]+|<bar>`)
)

// curText is what is before the cursor, with the lines it continues (at most
// ten, prev the lines above, the nearest last), after the last "|".
func curText(input string, prev []string) string {
	cur := input
	for i := len(prev) - 1; i >= 0; i-- {
		if !emptyText.MatchString(cur) && !continuedText.MatchString(cur) {
			break
		}
		if nonBlank.MatchString(prev[i]) || lineContinue.MatchString(cur) {
			cur = prev[i] + lineContinue.ReplaceAllString(cur, "")
		} else {
			break
		}
	}
	parts := bar.Split(cur, -1)
	return parts[len(parts)-1]
}

var (
	mapCommand = regexp.MustCompile(`^\w*map[ \t]`)
	envVar     = regexp.MustCompile(`\$\w*$`)
)

// completionType is getcompletiontype() of an editor without it.
func completionType(cur string) string {
	switch {
	case mapCommand.MatchString(cur):
		return "mapping"
	case envVar.MatchString(cur):
		return "environment"
	}
	return ""
}

var (
	optionPrefix = regexp.MustCompile(`^&(?:[gl]:?)?`)
	setFiletype  = regexp.MustCompile(`\b` + optional("set", "local") + `[ \t]+(?:filetype|ft)=`)
	hasArg       = regexp.MustCompile(`\bhas\(\$?['"]\w*$`)
	expandArg    = regexp.MustCompile(`\bexpand\(\$?['"][<>[:alnum:]]*$`)
	expression   = regexp.MustCompile(`\$["'].*\{[^}]*$|->\w*$`)
	bareName     = regexp.MustCompile(`^[[:digit:],[:space:]$'<>]*[A-Za-z_]\w*$`)
)

func (s source) gather(ctx context.Context, h sdk.Host, cur, ctype, word string, lnum int) ([]cmp.Item, error) {
	switch {
	case ctype == "option" || optionPrefix.MatchString(word):
		if setFiletype.MatchString(cur) {
			return s.filetypes(ctx, h)
		}
		opts, err := s.options(ctx, h)
		if err != nil {
			return nil, err
		}
		prefix := optionPrefix.FindString(word)
		out := make([]cmp.Item, len(opts))
		for i, o := range opts {
			o.Word = prefix + o.Word
			out[i] = o
		}
		return out, nil
	case hasArg.MatchString(cur):
		return []cmp.Item{
			{Word: "patch", Menu: "; Included patches Ex: patch123"},
			{Word: "patch-", Menu: "; Version and patches Ex: patch-7.4.237"},
		}, nil
	case expandArg.MatchString(cur):
		return completionList([]string{
			":e", ":h", ":p", ":r", ":t", "<SID>", "<abuf>", "<afile>",
			"<amatch>", "<cWORD>", "<cexpr>", "<cfile>", "<client>", "<cword>",
			"<script>", "<sfile>", "<sflnum>", "<slnum>", "<stack>",
		}), nil
	case expression.MatchString(cur):
		v, err := h.Call(ctx, "getcompletion", word, "expression")
		if err != nil {
			return nil, err
		}
		return completionList(strs(v)), nil
	case ctype == "mapping":
		return s.maps(ctx, h)
	case ctype == "environment":
		v, err := h.Call(ctx, "environ")
		if err != nil {
			return nil, err
		}
		m, _ := v.(map[string]any)
		names := make([]string, 0, len(m))
		for k := range m {
			names = append(names, k)
		}
		sort.Strings(names)
		out := make([]cmp.Item, len(names))
		for i, n := range names {
			out[i] = cmp.Item{Word: "$" + n, Kind: "e"}
		}
		return out, nil
	case !bareName.MatchString(cur):
		return localVariables(ctx, h, lnum)
	}
	return nil, nil
}

var optionSep = regexp.MustCompile(`[ \t]{2,}|\n`)

// options are the names of the options, from ":set all".
func (s source) options(ctx context.Context, h sdk.Host) ([]cmp.Item, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.options != nil {
		return s.c.options, nil
	}
	v, err := h.Call(ctx, "execute", "set all")
	if err != nil {
		return nil, err
	}
	out, _ := v.(string)
	var names []string
	for _, f := range optionSep.Split(out, -1) {
		if f != "" {
			names = append(names, f)
		}
	}
	if len(names) > 0 {
		// "--- Options ---"
		names = names[1:]
	}
	var opts []string
	for _, n := range names {
		if !isHead(n) {
			continue
		}
		// As substitute('^no\|=\zs.*$', ''): one or the other.
		if strings.HasPrefix(n, "no") {
			n = n[2:]
		} else if i := strings.IndexByte(n, '='); i >= 0 {
			n = n[:i+1]
		}
		opts = append(opts, n)
	}
	for _, o := range opts[:len(opts):len(opts)] {
		if !strings.HasSuffix(o, "=") {
			opts = append(opts, "no"+o)
		}
	}
	items := []cmp.Item{}
	for _, o := range opts {
		if isHead(o) {
			items = append(items, cmp.Item{Word: strings.TrimSuffix(o, "="), Kind: "o"})
		}
	}
	s.c.options = items
	return items, nil
}

// isHead is "=~# '^\h'".
func isHead(s string) bool {
	if s == "" {
		return false
	}
	b := s[0]
	return b == '_' || ('a' <= b && b <= 'z') || ('A' <= b && b <= 'Z')
}

var (
	mapTag  = regexp.MustCompile(`\*(?::map-)?(<` + keywordChars + `+>)\*`)
	keyName = regexp.MustCompile(`<` + keywordChars + `+>`)
)

// keywordChars is "\k" with the 'iskeyword' of Vim script buffers.
const keywordChars = `[0-9A-Za-z_#\x{C0}-\x{FF}]`

// maps are the special arguments of the mapping commands and the key names,
// from the help.
func (s source) maps(ctx context.Context, h sdk.Host) ([]cmp.Item, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.maps != nil {
		return s.c.maps, nil
	}
	v, err := h.Eval(ctx, `map(['doc/map.txt', 'doc/intro.txt'], `+
		`{_, f -> f->findfile(&runtimepath)->expand()->filereadable() ? f->findfile(&runtimepath)->expand()->readfile() : []})`)
	if err != nil {
		return nil, err
	}
	l, _ := v.([]any)
	if len(l) != 2 {
		return nil, nil
	}
	var words []string
	if lines := strs(l[0]); len(lines) > 0 {
		for _, line := range section(lines, `\*key-mapping\*`, `\*abbreviations\*`) {
			if m := mapTag.FindStringSubmatch(line); m != nil {
				words = append(words, m[1])
			}
		}
	}
	items := completionList(words)
	if lines := strs(l[1]); len(lines) > 0 {
		keys := []string{
			"<LeftMouse>", "<LeftDrag>", "<LeftRelease>",
			"<RightMouse>", "<RightDrag>", "<RightRelease>",
			"<MiddleMouse>", "<MiddleDrag>", "<MiddleRelease>",
			"<X1Mouse>", "<X1Drag>", "<X1Release>",
			"<X2Mouse>", "<X2Drag>", "<X2Release>",
			"<2-LeftMouse>", "<3-LeftMouse>", "<4-LeftMouse>",
			"<A-LeftMouse>", "<S-LeftMouse>", "<C-LeftMouse>",
			"<A-RightMouse>", "<S-RightMouse>", "<C-RightMouse>",
		}
		for _, line := range section(lines, `\*key-notation`, `\*vim-modes-intro\*`) {
			if k := keyName.FindString(line); k != "" {
				keys = append(keys, strings.Replace(k, "-…>", "-", 1))
			}
		}
		items = append(items, completionList(keys)...)
	}
	s.c.maps = items
	return items, nil
}

// section is lines[match(lines, start) : match(lines, end)], both included.
func section(lines []string, start, end string) []string {
	find := func(p string) int {
		re := regexp.MustCompile(p)
		for i, l := range lines {
			if re.MatchString(l) {
				return i
			}
		}
		return -1
	}
	i, j := find(start), find(end)
	// Vim's negative indexes count from the end.
	if i < 0 {
		i += len(lines)
	}
	if j < 0 {
		j += len(lines)
	}
	if i < 0 || j < i {
		return nil
	}
	return lines[i : j+1]
}

var filetypeName = regexp.MustCompile(`^[[:alnum:]-]*`)

// filetypes are the names of the syntax, indent and ftplugin files.
func (s source) filetypes(ctx context.Context, h sdk.Host) ([]cmp.Item, error) {
	s.c.mu.Lock()
	defer s.c.mu.Unlock()
	if s.c.filetypes != nil {
		return s.c.filetypes, nil
	}
	v, err := h.Eval(ctx, "globpath(&runtimepath, 'syntax/*.vim', v:true, v:true) + "+
		"globpath(&runtimepath, 'indent/*.vim', v:true, v:true) + "+
		"globpath(&runtimepath, 'ftplugin/*.vim', v:true, v:true)")
	if err != nil {
		return nil, err
	}
	var names []string
	for _, p := range strs(v) {
		// fnamemodify(':t:r')
		t := p[strings.LastIndexAny(p, `/\`)+1:]
		if i := strings.LastIndexByte(t, '.'); i > 0 {
			t = t[:i]
		}
		names = append(names, filetypeName.FindString(t))
	}
	s.c.filetypes = completionList(names)
	return s.c.filetypes, nil
}

// completionList is uniq() (of the neighbors), a trailing "/" only in Abbr.
func completionList(words []string) []cmp.Item {
	out := []cmp.Item{}
	for i, w := range words {
		if i > 0 && words[i-1] == w {
			continue
		}
		if strings.HasSuffix(w, "/") {
			out = append(out, cmp.Item{Word: w[:len(w)-1], Abbr: w})
		} else {
			out = append(out, cmp.Item{Word: w})
		}
	}
	return out
}

// optional is Vim's "word\%[rest]".
func optional(word, rest string) string {
	tail := ""
	for i := len(rest) - 1; i >= 0; i-- {
		tail = "(?:" + regexp.QuoteMeta(rest[i:i+1]) + tail + ")?"
	}
	return word + tail
}

func strs(v any) []string {
	l, _ := v.([]any)
	out := make([]string, 0, len(l))
	for _, x := range l {
		s, _ := x.(string)
		out = append(out, s)
	}
	return out
}

func toInt(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case uint64:
		return int(n)
	case int:
		return n
	case int8:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

var _ cmp.Previewer = source{}
