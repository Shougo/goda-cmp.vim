// Package file is a completion source for file names, looked up from the
// current directory, the directory of the buffer and their project roots. It
// is a port of ddc-source-file (https://github.com/LumaKernel/ddc-source-file,
// MIT License, Copyright (c) 2021 Luma). It has no Vim script: the patterns
// that need Vim's character classes ('isfname', 'iskeyword') are sent as one
// expression.
//
// The menu of an item says where it was found: "cwd", "buf", and "cwd^" or
// "buf^" for a project root (found by ProjMarkers), "cwd^2" for the next one
// up, and so on.
package file

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"

	"github.com/Shougo/goda-cmp.vim/cmp"
	"github.com/Shougo/goda.vim/sdk"
)

// Params are the parameters of the source, those of ddc-source-file.
type Params struct {
	// Mode is the kind of paths: "os" (default: those of this system),
	// "posix" or "win32" (on Windows only).
	Mode string `json:"mode"`
	// TakeFileNum bounds the files read in one directory (default 10000).
	TakeFileNum int `json:"takeFileNum"`
	// ProjMarkers are the names that mark a project root (default .git,
	// .vscode, .github).
	ProjMarkers []string `json:"projMarkers"`
	// CwdMaxItems and BufMaxItems bound the items from the current
	// directory and from the directory of the buffer; 0 turns them off
	// (default 1000).
	CwdMaxItems *int `json:"cwdMaxItems"`
	BufMaxItems *int `json:"bufMaxItems"`
	// ProjFromCwdMaxItems and ProjFromBufMaxItems bound the items from the
	// project roots above them, one number per root, nearest first (default
	// [1000]: the nearest root).
	ProjFromCwdMaxItems []int `json:"projFromCwdMaxItems"`
	ProjFromBufMaxItems []int `json:"projFromBufMaxItems"`
	// CwdAsRoot, BufAsRoot and ProjAsRoot: an absolute path typed is looked
	// up from these too, as if they were the root (default false, false,
	// true).
	CwdAsRoot  bool  `json:"cwdAsRoot"`
	BufAsRoot  bool  `json:"bufAsRoot"`
	ProjAsRoot *bool `json:"projAsRoot"`
	// TrailingSlash adds the separator to the word of a directory, and
	// TrailingSlashAbbr to what the popup shows (default false, true).
	TrailingSlash     bool  `json:"trailingSlash"`
	TrailingSlashAbbr *bool `json:"trailingSlashAbbr"`
	// FollowSymlinks treats a link to a directory as a directory.
	FollowSymlinks bool `json:"followSymlinks"`
	// DisableMenu leaves the menu empty.
	DisableMenu bool `json:"disableMenu"`
	// BeforeResolve and AfterResolve are put before and after the typed
	// directory when it is looked up.
	BeforeResolve string `json:"beforeResolve"`
	AfterResolve  string `json:"afterResolve"`
	// FilenameChars are the characters of the completed part, in a Vim
	// collection (default "[:keyword:]").
	FilenameChars string `json:"filenameChars"`
	// The kind shown for each kind of entry, and the menu for the current
	// directory and the buffer's.
	DisplayFile    string `json:"displayFile"`
	DisplayDir     string `json:"displayDir"`
	DisplaySym     string `json:"displaySym"`
	DisplaySymFile string `json:"displaySymFile"`
	DisplaySymDir  string `json:"displaySymDir"`
	DisplayCwd     string `json:"displayCwd"`
	DisplayBuf     string `json:"displayBuf"`
}

func fill(p Params) Params {
	def := func(s *string, v string) {
		if *s == "" {
			*s = v
		}
	}
	def(&p.Mode, "os")
	def(&p.FilenameChars, "[:keyword:]")
	def(&p.DisplayFile, "file")
	def(&p.DisplayDir, "dir")
	def(&p.DisplaySym, "sym")
	def(&p.DisplaySymFile, "sym=file")
	def(&p.DisplaySymDir, "sym=dir")
	def(&p.DisplayCwd, "cwd")
	def(&p.DisplayBuf, "buf")
	if p.TakeFileNum <= 0 {
		p.TakeFileNum = 10000
	}
	if p.ProjMarkers == nil {
		p.ProjMarkers = []string{".git", ".vscode", ".github"}
	}
	if p.CwdMaxItems == nil {
		p.CwdMaxItems = new(1000)
	}
	if p.BufMaxItems == nil {
		p.BufMaxItems = new(1000)
	}
	if p.ProjFromCwdMaxItems == nil {
		p.ProjFromCwdMaxItems = []int{1000}
	}
	if p.ProjFromBufMaxItems == nil {
		p.ProjFromBufMaxItems = []int{1000}
	}
	if p.ProjAsRoot == nil {
		p.ProjAsRoot = new(true)
	}
	if p.TrailingSlashAbbr == nil {
		p.TrailingSlashAbbr = new(true)
	}
	return p
}

type source struct{ p Params }

// New returns the source, named "file".
func New(p Params) cmp.Source { return source{fill(p)} }

func (source) Name() string { return "file" }

// WithParams sets the parameters from Vim script (sourceParams).
func (s source) WithParams(p cmp.Params) (any, error) {
	np, err := cmp.PatchParams(s.p, p)
	if err != nil {
		return nil, err
	}
	return New(np), nil
}

// paths are the path functions of a mode.
type paths struct {
	sep   string
	posix bool
}

func (m paths) join(elem ...string) string {
	if m.posix {
		return path.Join(elem...)
	}
	return filepath.Join(elem...)
}

func (m paths) dir(p string) string {
	if m.posix {
		return path.Dir(p)
	}
	return filepath.Dir(p)
}

func (m paths) isAbs(p string) bool {
	if m.posix {
		return path.IsAbs(p)
	}
	return filepath.IsAbs(p)
}

func (s source) paths() paths {
	switch s.p.Mode {
	case "posix":
		return paths{"/", true}
	case "win32":
		return paths{`\`, runtime.GOOS != "windows"}
	}
	if runtime.GOOS == "windows" {
		return paths{`\`, false}
	}
	return paths{"/", true}
}

type point struct {
	dir    string
	max    int
	asRoot bool
	menu   string
}

var envPrefix = regexp.MustCompile(`(?i)^(?:\$(?:env:)?(\w+)|%(\w+)%)[/\\]`)

func (s source) Gather(ctx context.Context, h sdk.Host, c *cmp.Context) (cmp.Gathered, error) {
	p := s.p
	pm := s.paths()
	v, err := h.Eval(ctx, infoExpr(c.Input, p.FilenameChars, pm.sep == "/"))
	if err != nil {
		return cmp.Gathered{}, err
	}
	l, _ := v.([]any)
	if len(l) != 5 {
		return cmp.Gathered{Start: len(c.Input)}, nil
	}
	start := toInt(l[0])
	if start < 0 || start > len(c.Input) {
		start = len(c.Input)
	}
	full, _ := l[1].(string)
	basePrefix, _ := l[2].(string)
	bufPath, _ := l[3].(string)
	cwd, _ := l[4].(string)

	expanded := s.expand(ctx, h, full, pm)
	inputDir := expanded
	if !strings.HasSuffix(expanded, pm.sep) {
		inputDir = pm.dir(expanded)
	}
	isAbs := pm.isAbs(expanded)

	var points []point
	if isAbs {
		m := max(*p.CwdMaxItems, *p.BufMaxItems)
		for _, n := range append(append([]int{}, p.ProjFromCwdMaxItems...), p.ProjFromBufMaxItems...) {
			m = max(m, n)
		}
		points = append(points, point{dir: "", max: m, asRoot: true})
	}
	points = append(points, point{dir: cwd, max: *p.CwdMaxItems, menu: p.DisplayCwd, asRoot: p.CwdAsRoot})
	points = append(points, s.projPoints(cwd, p.ProjFromCwdMaxItems, p.DisplayCwd, pm)...)
	if pm.isAbs(bufPath) {
		bufDir := pm.dir(bufPath)
		points = append(points, point{dir: bufDir, max: *p.BufMaxItems, menu: p.DisplayBuf, asRoot: p.BufAsRoot})
		points = append(points, s.projPoints(bufDir, p.ProjFromBufMaxItems, p.DisplayBuf, pm)...)
	}

	var items []cmp.Item
	for _, pt := range points {
		if pt.max < 1 || (isAbs && !pt.asRoot) {
			continue
		}
		dir := pm.join(pt.dir, p.BeforeResolve, inputDir, p.AfterResolve)
		if isAbs && pt.dir == "" && pm.sep == "/" && !strings.HasPrefix(dir, "/") {
			dir = "/" + dir
		}
		items = append(items, s.list(dir, basePrefix, pt, isAbs, pm)...)
		if ctx.Err() != nil {
			return cmp.Gathered{}, ctx.Err()
		}
	}
	return cmp.Gathered{Start: start, Items: items}, nil
}

// list reads the entries of dir that start with prefix.
func (s source) list(dir, prefix string, pt point, isAbs bool, pm paths) []cmp.Item {
	p := s.p
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var items []cmp.Item
	count := 0
	for _, e := range ents {
		if count >= p.TakeFileNum || len(items) >= pt.max {
			break
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) {
			continue
		}
		isSym := e.Type()&os.ModeSymlink != 0
		isDir := e.IsDir()
		if isSym && p.FollowSymlinks {
			if st, err := os.Stat(filepath.Join(dir, name)); err == nil && st.IsDir() {
				isDir = true
			}
		}
		part := name[len(prefix):]
		it := cmp.Item{Word: part, Abbr: part}
		if isDir && p.TrailingSlash {
			it.Word += pm.sep
		}
		if isDir && *p.TrailingSlashAbbr {
			it.Abbr += pm.sep
		}
		if !p.DisableMenu {
			if pt.menu != "" && isAbs {
				it.Menu = pm.sep
			}
			it.Menu += pt.menu
		}
		switch {
		case isSym && p.FollowSymlinks && isDir:
			it.Kind = p.DisplaySymDir
		case isSym && p.FollowSymlinks:
			it.Kind = p.DisplaySymFile
		case isSym:
			it.Kind = p.DisplaySym
		case isDir:
			it.Kind = p.DisplayDir
		default:
			it.Kind = p.DisplayFile
		}
		items = append(items, it)
		count++
	}
	return items
}

// projPoints are the project roots above dir, nearest first, one per number
// of maxItems.
func (s source) projPoints(dir string, maxItems []int, label string, pm paths) []point {
	roots := findMarkers(len(maxItems), dir, s.p.ProjMarkers, pm)
	out := make([]point, 0, len(roots))
	for i, r := range roots {
		menu := label + "^"
		if i > 0 {
			menu += itoa(i + 1)
		}
		out = append(out, point{dir: r, max: maxItems[i], menu: menu, asRoot: *s.p.ProjAsRoot})
	}
	return out
}

// findMarkers walks up from dir and returns at most n directories that
// contain one of markers.
func findMarkers(n int, dir string, markers []string, pm paths) []string {
	var out []string
	cur := dir
	for len(out) < n && cur != "" {
		for _, m := range markers {
			if _, err := os.Lstat(pm.join(cur, m)); err == nil {
				out = append(out, cur)
				break
			}
		}
		parent := pm.dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return out
}

// expand expands "~", $HOME, %USERPROFILE%, $VAR, $env:VAR and %VAR% at the
// head of a path, with Vim's environment.
func (s source) expand(ctx context.Context, h sdk.Host, full string, pm paths) string {
	home, _ := os.UserHomeDir()
	if v, err := h.Call(ctx, "getenv", "HOME"); err == nil {
		if hs, ok := v.(string); ok && hs != "" {
			home = hs
		}
	}
	last := ""
	if strings.HasSuffix(full, pm.sep) {
		last = pm.sep
	}
	for _, pre := range []string{"~" + pm.sep, "$HOME" + pm.sep} {
		if home != "" && strings.HasPrefix(full, pre) {
			return strings.TrimSuffix(pm.join(home, full[len(pre):]), pm.sep) + last
		}
	}
	if pre := "%USERPROFILE%" + pm.sep; home != "" && strings.HasPrefix(strings.ToUpper(full), pre) {
		return strings.TrimSuffix(pm.join(home, full[len(pre):]), pm.sep) + last
	}
	if m := envPrefix.FindStringSubmatch(full); m != nil {
		name := m[1] + m[2]
		if v, err := h.Call(ctx, "getenv", name); err == nil {
			if env, ok := v.(string); ok && env != "" {
				return strings.TrimSuffix(pm.join(env, full[len(m[0]):]), pm.sep) + last
			}
		}
	}
	return full
}

// infoExpr is the Vim expression for what is before the cursor, in the terms
// of 'isfname' and chars (a collection of 'iskeyword' and others), as the
// Vim script of ddc-source-file finds it: [the start of the completed part,
// the path typed, the part of its file name before chars, the path of the
// buffer, the current directory].
func infoExpr(input, chars string, posix bool) string {
	// The separator, escaped as an atom and in a collection.
	sep, inColl := `\\`, `\\`
	if posix {
		sep, inColl = `\/`, "/"
	}
	// The path: file name characters and separators, maybe after "$" or
	// "$env:".
	fullPat := `\c\%(^\$\%(env:\)\?\)\?[[:fname:]` + inColl + `]*$`
	// What is before chars in the last part of the path.
	basePat := `\%(.*` + sep + `\|^\)\(.*[^` + chars + inColl + `]\)[` + chars + `]*$`
	in := vimString(input)
	full := "matchstr(" + in + ", " + vimString(fullPat) + ")"
	return "[match(" + in + ", " + vimString("["+chars+"]*$") + "), " + full + ", " +
		"get(matchlist(" + full + ", " + vimString(basePat) + "), 1, ''), expand('%:p'), getcwd()]"
}

func vimString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

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
	return -1
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return itoa(n/10) + string(rune('0'+n%10))
}
