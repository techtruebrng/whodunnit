// Package git wraps the git command line for the queries whodunnit needs:
// blame for a single line, ancestry checks and pull request discovery.
package git

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Repo is a git work tree.
type Repo struct {
	Dir string

	ignoreRevsFile string
	blameCache     map[string]blameResult
	prCache        map[string]prResult
}

type blameResult struct {
	b   Blame
	err error
}

type prResult struct {
	n   int
	ok  bool
	err error
}

// Blame describes the commit that last changed a line.
type Blame struct {
	SHA        string    `json:"sha"`
	Author     string    `json:"author"`
	AuthorMail string    `json:"author_mail,omitempty"`
	Time       time.Time `json:"time"`
	Summary    string    `json:"summary"`
	OrigFile   string    `json:"orig_file,omitempty"`
	OrigLine   int       `json:"orig_line,omitempty"`
}

// ErrLineOutOfRange is returned when the requested line does not exist at
// the given revision, which usually means the trace came from another version.
var ErrLineOutOfRange = errors.New("line is past the end of the file")

// Open finds the work tree containing dir.
func Open(dir string) (*Repo, error) {
	out, err := run(dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("%s is not inside a git repository", dir)
	}
	r := &Repo{
		Dir:        strings.TrimSpace(out),
		blameCache: map[string]blameResult{},
		prCache:    map[string]prResult{},
	}
	if f := filepath.Join(r.Dir, ".git-blame-ignore-revs"); fileExists(f) {
		r.ignoreRevsFile = f
	}
	return r, nil
}

// ResolveRev returns the full commit hash for rev.
func (r *Repo) ResolveRev(rev string) (string, error) {
	out, err := r.git("rev-parse", "--verify", "--quiet", rev+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("unknown revision %q", rev)
	}
	return strings.TrimSpace(out), nil
}

// Files lists every file tracked at rev.
func (r *Repo) Files(rev string) ([]string, error) {
	out, err := r.git("ls-tree", "-r", "-z", "--name-only", rev)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, f := range strings.Split(out, "\x00") {
		if f != "" {
			files = append(files, f)
		}
	}
	return files, nil
}

// Blame returns the commit that last changed line (1-based) of file at rev.
// Whitespace-only changes are ignored, as are commits listed in
// .git-blame-ignore-revs.
func (r *Repo) Blame(rev, file string, line int) (Blame, error) {
	key := rev + "\x00" + file + "\x00" + strconv.Itoa(line)
	if c, ok := r.blameCache[key]; ok {
		return c.b, c.err
	}
	b, err := r.blame(rev, file, line)
	r.blameCache[key] = blameResult{b, err}
	return b, err
}

func (r *Repo) blame(rev, file string, line int) (Blame, error) {
	args := []string{"blame", "--porcelain", "-w", "-L", fmt.Sprintf("%d,%d", line, line)}
	tail := []string{rev, "--", file}
	out, err := "", error(nil)
	if r.ignoreRevsFile != "" {
		out, err = r.git(append(append(args, "--ignore-revs-file", r.ignoreRevsFile), tail...)...)
		// A stale or malformed ignore file shouldn't break every frame.
		if err != nil && !strings.Contains(err.Error(), "has only") {
			r.ignoreRevsFile = ""
		}
	}
	if r.ignoreRevsFile == "" {
		out, err = r.git(append(args, tail...)...)
	}
	if err != nil {
		if strings.Contains(err.Error(), "has only") {
			return Blame{}, ErrLineOutOfRange
		}
		return Blame{}, err
	}
	return parsePorcelain(out)
}

func parsePorcelain(out string) (Blame, error) {
	var b Blame
	sc := bufio.NewScanner(strings.NewReader(out))
	sc.Buffer(make([]byte, 1024*1024), 1024*1024)
	first := true
	for sc.Scan() {
		line := sc.Text()
		if first {
			fields := strings.Fields(line)
			if len(fields) < 3 {
				return b, fmt.Errorf("unexpected blame output: %q", line)
			}
			b.SHA = fields[0]
			b.OrigLine, _ = strconv.Atoi(fields[1])
			first = false
			continue
		}
		if strings.HasPrefix(line, "\t") {
			break // the line content ends the header block
		}
		key, val, _ := strings.Cut(line, " ")
		switch key {
		case "author":
			b.Author = val
		case "author-mail":
			b.AuthorMail = strings.Trim(val, "<>")
		case "author-time":
			sec, _ := strconv.ParseInt(val, 10, 64)
			b.Time = time.Unix(sec, 0).UTC()
		case "summary":
			b.Summary = val
		case "filename":
			b.OrigFile = val
		}
	}
	if b.SHA == "" {
		return b, errors.New("empty blame output")
	}
	return b, nil
}

// IsAncestor reports whether commit a is an ancestor of (or equal to) b.
func (r *Repo) IsAncestor(a, b string) (bool, error) {
	cmd := exec.Command("git", "merge-base", "--is-ancestor", a, b)
	cmd.Dir = r.Dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base --is-ancestor %s %s: %w", a, b, err)
}

// CommitsTouching counts commits in since..rev that changed file.
func (r *Repo) CommitsTouching(since, rev, file string) (int, error) {
	out, err := r.git("rev-list", "--count", since+".."+rev, "--", file)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(strings.TrimSpace(out))
}

var (
	reSquashPR    = regexp.MustCompile(`\(#(\d+)\)\s*$`)
	reGitHubMerge = regexp.MustCompile(`^Merge pull request #(\d+)\b`)
	reBitbucket   = regexp.MustCompile(`\(pull request #(\d+)\)`)
	reGitLabMR    = regexp.MustCompile(`(?m)^See merge request \S*!(\d+)\s*$`)
)

// PullRequest finds the pull request that brought sha into rev. It
// recognizes squash merges ("Fix totals (#123)") and merge commits from
// GitHub, GitLab and Bitbucket. It works entirely offline.
func (r *Repo) PullRequest(sha, rev string) (int, bool, error) {
	key := sha + "\x00" + rev
	if c, ok := r.prCache[key]; ok {
		return c.n, c.ok, c.err
	}
	n, ok, err := r.pullRequest(sha, rev)
	r.prCache[key] = prResult{n, ok, err}
	return n, ok, err
}

func (r *Repo) pullRequest(sha, rev string) (int, bool, error) {
	msg, err := r.git("log", "-1", "--format=%B", sha)
	if err != nil {
		return 0, false, err
	}
	if n, ok := prFromMessage(msg); ok {
		return n, true, nil
	}
	// Find the merge on rev's first-parent history that introduced sha:
	// the oldest first-parent merge that descends from sha.
	out, err := r.git("rev-list", "--ancestry-path", "--first-parent", "--merges", sha+".."+rev)
	if err != nil {
		return 0, false, err
	}
	merges := strings.Fields(out)
	if len(merges) == 0 {
		return 0, false, nil
	}
	merge := merges[len(merges)-1]
	// If sha was already on the mainline before this merge, the merge did
	// not introduce it (sha was committed directly to the branch).
	if onMain, err := r.IsAncestor(sha, merge+"^1"); err != nil || onMain {
		return 0, false, err
	}
	msg, err = r.git("log", "-1", "--format=%B", merge)
	if err != nil {
		return 0, false, err
	}
	n, ok := prFromMessage(msg)
	return n, ok, nil
}

func prFromMessage(msg string) (int, bool) {
	subject, _, _ := strings.Cut(strings.TrimSpace(msg), "\n")
	for _, re := range []*regexp.Regexp{reGitHubMerge, reSquashPR, reBitbucket} {
		if m := re.FindStringSubmatch(subject); m != nil {
			n, _ := strconv.Atoi(m[1])
			return n, true
		}
	}
	if m := reGitLabMR.FindStringSubmatch(msg); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// RemoteURL returns the fetch URL of the named remote, or "" if unset.
func (r *Repo) RemoteURL(name string) string {
	out, err := r.git("remote", "get-url", name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

func (r *Repo) git(args ...string) (string, error) { return run(r.Dir, args...) }

func run(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("git %s: %s", args[0], msg)
	}
	return stdout.String(), nil
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}
