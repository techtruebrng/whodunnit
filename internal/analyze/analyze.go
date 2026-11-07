// Package analyze connects parsed stack frames to repository history.
package analyze

import (
	"errors"
	"io/fs"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/techtruebrng/whodunnit/internal/git"
	"github.com/techtruebrng/whodunnit/internal/resolve"
	"github.com/techtruebrng/whodunnit/internal/sourcemap"
	"github.com/techtruebrng/whodunnit/internal/trace"
)

// Options control an analysis.
type Options struct {
	Rev        string   // revision that produced the trace (default HEAD)
	Since      string   // highlight lines changed after this ref
	SourceMaps []string // directories searched for *.map files
	Remote     string   // remote used to build links (default origin)
	RepoURL    string   // explicit web URL, overrides Remote
}

// Status of a frame after analysis.
const (
	StatusBlamed     = "blamed"
	StatusExternal   = "external"
	StatusNotFound   = "not-found"
	StatusOutOfRange = "out-of-range"
	StatusError      = "error"
)

// Generated is the position in bundled code before source-map translation.
type Generated struct {
	File string `json:"file"`
	Line int    `json:"line"`
	Col  int    `json:"col,omitempty"`
}

// Frame is a stack frame plus everything learned about it.
type Frame struct {
	trace.Frame
	Generated  *Generated `json:"generated,omitempty"`
	Path       string     `json:"path,omitempty"`
	Candidates []string   `json:"candidates,omitempty"`
	Status     string     `json:"status"`
	Error      string     `json:"error,omitempty"`
	Blame      *git.Blame `json:"blame,omitempty"`
	PR         int        `json:"pr,omitempty"`
	FileURL    string     `json:"file_url,omitempty"`
	CommitURL  string     `json:"commit_url,omitempty"`
	PRURL      string     `json:"pr_url,omitempty"`
	// NewSinceRef is true when the blamed commit is not in Options.Since.
	NewSinceRef *bool `json:"new_since_ref,omitempty"`
	// FileCommitsSinceRef counts commits after Options.Since touching the file.
	FileCommitsSinceRef *int `json:"file_commits_since_ref,omitempty"`
}

// InRepo reports whether the frame was matched to a repository file.
func (f Frame) InRepo() bool { return f.Path != "" }

// Suspect is a commit that last touched one or more frames.
type Suspect struct {
	SHA         string    `json:"sha"`
	Author      string    `json:"author"`
	Time        time.Time `json:"time"`
	Summary     string    `json:"summary"`
	PR          int       `json:"pr,omitempty"`
	CommitURL   string    `json:"commit_url,omitempty"`
	PRURL       string    `json:"pr_url,omitempty"`
	Frames      []int     `json:"frames"`
	NewSinceRef bool      `json:"new_since_ref,omitempty"`
}

// Report is the result of an analysis.
type Report struct {
	Repo     string    `json:"repo"`
	Rev      string    `json:"rev"`
	RevSHA   string    `json:"rev_sha"`
	Since    string    `json:"since,omitempty"`
	RepoURL  string    `json:"repo_url,omitempty"`
	Frames   []Frame   `json:"frames"`
	Suspects []Suspect `json:"suspects"`
}

// Run analyzes frames against the repository.
func Run(repo *git.Repo, frames []trace.Frame, opt Options) (*Report, error) {
	if opt.Rev == "" {
		opt.Rev = "HEAD"
	}
	if opt.Remote == "" {
		opt.Remote = "origin"
	}
	revSHA, err := repo.ResolveRev(opt.Rev)
	if err != nil {
		return nil, err
	}
	sinceSHA := ""
	if opt.Since != "" {
		if sinceSHA, err = repo.ResolveRev(opt.Since); err != nil {
			return nil, err
		}
	}
	files, err := repo.Files(revSHA)
	if err != nil {
		return nil, err
	}
	ix := resolve.NewIndex(files)

	var forge *git.Forge
	remote := opt.RepoURL
	if remote == "" {
		remote = repo.RemoteURL(opt.Remote)
	}
	if f, ok := git.ParseForge(remote); ok {
		forge = &f
	}

	maps, err := newMapFinder(opt.SourceMaps)
	if err != nil {
		return nil, err
	}

	rep := &Report{Repo: repo.Dir, Rev: opt.Rev, RevSHA: revSHA, Since: opt.Since, Frames: []Frame{}, Suspects: []Suspect{}}
	if forge != nil {
		rep.RepoURL = forge.Base
	}

	for _, tf := range frames {
		f := Frame{Frame: tf}
		if tf.Lang == trace.JS {
			maps.apply(&f)
		}
		res := ix.Resolve(f.File)
		switch {
		case res.External:
			f.Status = StatusExternal
		case res.Path == "":
			f.Status = StatusNotFound
		default:
			f.Path, f.Candidates = res.Path, res.Candidates
			blameFrame(repo, &f, revSHA, sinceSHA, forge)
		}
		rep.Frames = append(rep.Frames, f)
	}
	rep.Suspects = suspects(rep.Frames)
	return rep, nil
}

func blameFrame(repo *git.Repo, f *Frame, rev, since string, forge *git.Forge) {
	if f.Line <= 0 {
		f.Status, f.Error = StatusError, "frame has no line number"
		return
	}
	b, err := repo.Blame(rev, f.Path, f.Line)
	if errors.Is(err, git.ErrLineOutOfRange) {
		f.Status, f.Error = StatusOutOfRange, "line is past the end of the file at this revision; the trace may come from another version (try --rev)"
		return
	}
	if err != nil {
		f.Status, f.Error = StatusError, err.Error()
		return
	}
	f.Status, f.Blame = StatusBlamed, &b
	if forge != nil {
		f.FileURL = forge.FileURL(rev, f.Path, f.Line)
		f.CommitURL = forge.CommitURL(b.SHA)
	}
	if n, ok, err := repo.PullRequest(b.SHA, rev); err == nil && ok {
		f.PR = n
		if forge != nil {
			f.PRURL = forge.PullURL(n)
		}
	}
	if since != "" {
		old, err := repo.IsAncestor(b.SHA, since)
		if err == nil {
			isNew := !old
			f.NewSinceRef = &isNew
		}
		if n, err := repo.CommitsTouching(since, rev, f.Path); err == nil {
			f.FileCommitsSinceRef = &n
		}
	}
}

// suspects groups blamed frames by commit. Commits that are new since the
// --since ref come first, then the most recent.
func suspects(frames []Frame) []Suspect {
	bySHA := map[string]*Suspect{}
	var order []string
	for _, f := range frames {
		if f.Blame == nil {
			continue
		}
		s, ok := bySHA[f.Blame.SHA]
		if !ok {
			s = &Suspect{
				SHA: f.Blame.SHA, Author: f.Blame.Author, Time: f.Blame.Time,
				Summary: f.Blame.Summary, PR: f.PR, CommitURL: f.CommitURL, PRURL: f.PRURL,
				Frames: []int{},
			}
			bySHA[f.Blame.SHA] = s
			order = append(order, f.Blame.SHA)
		}
		s.Frames = append(s.Frames, f.Index)
		if f.NewSinceRef != nil && *f.NewSinceRef {
			s.NewSinceRef = true
		}
	}
	out := make([]Suspect, 0, len(order))
	for _, sha := range order {
		out = append(out, *bySHA[sha])
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].NewSinceRef != out[j].NewSinceRef {
			return out[i].NewSinceRef
		}
		return out[i].Time.After(out[j].Time)
	})
	return out
}

// mapFinder locates and caches source maps for bundled JavaScript files.
type mapFinder struct {
	byName map[string][]string // "app.3f2a.js" -> paths of app.3f2a.js.map
	cache  map[string]*sourcemap.Map
}

func newMapFinder(dirs []string) (*mapFinder, error) {
	mf := &mapFinder{byName: map[string][]string{}, cache: map[string]*sourcemap.Map{}}
	for _, dir := range dirs {
		err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() && d.Name() == "node_modules" {
				return filepath.SkipDir
			}
			if !d.IsDir() && strings.HasSuffix(d.Name(), ".map") {
				name := strings.TrimSuffix(d.Name(), ".map")
				mf.byName[name] = append(mf.byName[name], p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return mf, nil
}

// apply rewrites a bundled frame to its original source location.
func (mf *mapFinder) apply(f *Frame) {
	if len(mf.byName) == 0 {
		return
	}
	name := path.Base(resolve.Clean(f.File))
	candidates := mf.byName[name]
	if len(candidates) == 0 {
		return
	}
	// Prefer the map whose directory best matches the frame's path.
	best := candidates[0]
	if len(candidates) > 1 {
		ix := resolve.NewIndex(stripMapExt(candidates))
		if r := ix.Resolve(f.File); r.Path != "" {
			best = r.Path + ".map"
		}
	}
	m, ok := mf.cache[best]
	if !ok {
		var err error
		if m, err = sourcemap.ParseFile(best); err != nil {
			m = nil
		}
		mf.cache[best] = m
	}
	if m == nil {
		return
	}
	pos, ok := m.Lookup(f.Line, f.Col)
	if !ok {
		return
	}
	f.Generated = &Generated{File: f.File, Line: f.Line, Col: f.Col}
	f.File, f.Line, f.Col = pos.Source, pos.Line, pos.Col
	if pos.Name != "" && (f.Func == "" || len(f.Func) <= 2) {
		f.Func = pos.Name
	}
}

func stripMapExt(paths []string) []string {
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = filepath.ToSlash(strings.TrimSuffix(p, ".map"))
	}
	return out
}
