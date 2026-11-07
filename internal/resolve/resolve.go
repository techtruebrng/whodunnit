// Package resolve maps file paths from a stack trace (which were written on a
// server, in a container or on another developer's machine) onto files in the
// local repository.
package resolve

import (
	"net/url"
	"path"
	"sort"
	"strings"
)

// Result describes how a trace path matched the repository.
type Result struct {
	Path       string   // repository-relative path; empty if not found
	Candidates []string // all equally good matches when ambiguous
	External   bool     // looks like a dependency or runtime file
}

// Ambiguous reports whether more than one repository file matched equally well.
func (r Result) Ambiguous() bool { return len(r.Candidates) > 1 }

// Index is a lookup table over repository file paths.
type Index struct {
	byBase map[string][]string
}

// NewIndex builds an index from repository-relative paths (as printed by
// git ls-tree / ls-files).
func NewIndex(files []string) *Index {
	ix := &Index{byBase: make(map[string][]string, len(files))}
	for _, f := range files {
		b := path.Base(f)
		ix.byBase[b] = append(ix.byBase[b], f)
	}
	return ix
}

// externalMarkers are path fragments that indicate third-party or runtime
// code. Frames under these are never matched against the repository, even if
// the repository happens to contain a file with the same name.
var externalMarkers = []string{
	"site-packages/", "dist-packages/", "node_modules/", "/lib/python",
	"/go/pkg/mod/", "/pkg/mod/", "/usr/local/go/src/", "/rustc/", "/.cargo/registry/",
	"/.rustup/", "/gems/", "/rubygems/", "/.rbenv/", "/.rvm/", "/vendor/bundle/",
	"node:", "<", "/jdk/", "java.base/",
}

// Clean normalizes a path as it appears in a trace: strips URL schemes and
// hosts, bundler prefixes such as webpack:///, query strings, Windows
// separators and leading "./".
func Clean(p string) string {
	p = strings.TrimSpace(p)
	for _, prefix := range []string{"webpack:///", "webpack://", "webpack-internal:///", "file://"} {
		if strings.HasPrefix(p, prefix) {
			p = p[len(prefix):]
			// webpack://<project-name>/./src/x.ts
			if prefix == "webpack://" {
				if i := strings.Index(p, "/"); i >= 0 {
					p = p[i+1:]
				}
			}
		}
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		if u, err := url.Parse(p); err == nil {
			p = u.Path
		}
	}
	if i := strings.IndexAny(p, "?#"); i >= 0 {
		p = p[:i]
	}
	p = strings.ReplaceAll(p, `\`, "/")
	// Drop a Windows drive letter.
	if len(p) > 2 && p[1] == ':' && p[2] == '/' {
		p = p[2:]
	}
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return p
}

// IsExternal reports whether a cleaned trace path points at dependency or
// runtime code rather than application code.
func IsExternal(p string) bool {
	for _, m := range externalMarkers {
		if strings.Contains(p, m) {
			return true
		}
	}
	return false
}

// Resolve finds the repository file that a trace path refers to by matching
// path components from the end. A match is accepted when either path is
// fully consumed (e.g. "/app/main.py" vs "main.py", or "Invoice.java" vs
// "src/main/java/Invoice.java") or at least two trailing components agree.
// That keeps "/usr/lib/python3.12/json/decoder.py" from matching an unrelated
// "app/decoder.py".
func (ix *Index) Resolve(tracePath string) Result {
	p := Clean(tracePath)
	if p == "" || IsExternal(p) {
		return Result{External: true}
	}
	want := split(p)
	best, bestScore := []string(nil), 0
	for _, cand := range ix.byBase[want[len(want)-1]] {
		have := split(cand)
		score := suffixLen(want, have)
		ok := score == len(want) || score == len(have) || score >= 2
		if !ok {
			continue
		}
		switch {
		case score > bestScore:
			best, bestScore = []string{cand}, score
		case score == bestScore:
			best = append(best, cand)
		}
	}
	if len(best) == 0 {
		return Result{}
	}
	sort.Strings(best)
	r := Result{Path: best[0]}
	if len(best) > 1 {
		r.Candidates = best
	}
	return r
}

func split(p string) []string {
	parts := strings.Split(strings.Trim(p, "/"), "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" && s != "." {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func suffixLen(a, b []string) int {
	n := 0
	for i, j := len(a)-1, len(b)-1; i >= 0 && j >= 0 && a[i] == b[j]; i, j = i-1, j-1 {
		n++
	}
	return n
}
