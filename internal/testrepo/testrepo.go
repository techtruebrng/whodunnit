// Package testrepo builds small git repositories with a known history for
// tests.
package testrepo

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Repo is a scratch repository.
type Repo struct {
	t   testing.TB
	Dir string
}

// New creates an empty repository with deterministic git configuration.
func New(t testing.TB) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	r := &Repo{t: t, Dir: t.TempDir()}
	r.Git("init", "-q", "-b", "main")
	r.Git("config", "commit.gpgsign", "false")
	return r
}

// Git runs a git command and returns its trimmed output.
func (r *Repo) Git(args ...string) string {
	r.t.Helper()
	return r.GitEnv(nil, args...)
}

// GitEnv runs git with extra environment variables.
func (r *Repo) GitEnv(env []string, args ...string) string {
	r.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), env...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Write creates or replaces a file.
func (r *Repo) Write(name, content string) {
	r.t.Helper()
	p := filepath.Join(r.Dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		r.t.Fatal(err)
	}
}

// Commit stages everything and commits as author at date (RFC 3339).
func (r *Repo) Commit(author, email, date, msg string) string {
	r.t.Helper()
	r.Git("add", "-A")
	r.GitEnv(identity(author, email, date), "commit", "-q", "-m", msg)
	return r.Git("rev-parse", "HEAD")
}

// Merge creates a --no-ff merge commit of branch into the current branch.
func (r *Repo) Merge(branch, author, email, date, msg string) string {
	r.t.Helper()
	r.GitEnv(identity(author, email, date), "merge", "-q", "--no-ff", "-m", msg, branch)
	return r.Git("rev-parse", "HEAD")
}

func identity(name, email, date string) []string {
	return []string{
		"GIT_AUTHOR_NAME=" + name, "GIT_AUTHOR_EMAIL=" + email, "GIT_AUTHOR_DATE=" + date,
		"GIT_COMMITTER_NAME=" + name, "GIT_COMMITTER_EMAIL=" + email, "GIT_COMMITTER_DATE=" + date,
	}
}

// Shop builds the history used across tests:
//
//	v1.0  Alice  "Add invoice model"           (all lines)
//	      Bob    "Clamp negative discounts"    on branch, merged as PR #7
//	      Carol  "Round totals (#12)"          squash-merged PR on main
//	      Dave   "Bump default port"           direct commit, no PR
//
// It returns the commit hashes keyed by author.
func Shop(t testing.TB) (*Repo, map[string]string) {
	r := New(t)
	shas := map[string]string{}
	r.Write("main.py", "import app\nPORT = 8000\napp.run(PORT)\n")
	r.Write("app/billing/invoice.py", lines(
		"from decimal import Decimal",
		"",
		"def total(items):",
		"    subtotal = sum(i.price for i in items)",
		"    return apply_discount(subtotal)",
		"",
		"",
		"def apply_discount(amount, pct=0):",
		"    return amount * (1 - pct)",
		"",
	))
	shas["alice"] = r.Commit("Alice Ng", "alice@example.com", "2024-01-15T10:00:00Z", "Add invoice model")
	r.Git("tag", "v1.0")

	r.Git("checkout", "-q", "-b", "bob/discount")
	r.Write("app/billing/invoice.py", strings.Replace(read(t, r, "app/billing/invoice.py"),
		"    return amount * (1 - pct)", "    return amount * (1 - max(pct, 0))", 1))
	shas["bob"] = r.Commit("Bob Ruiz", "bob@example.com", "2026-10-30T09:00:00Z", "Clamp negative discounts")
	r.Git("checkout", "-q", "main")
	shas["merge"] = r.Merge("bob/discount", "Bob Ruiz", "bob@example.com", "2026-10-31T09:00:00Z", "Merge pull request #7 from bob/discount\n\nClamp negative discounts")

	r.Write("app/billing/invoice.py", strings.Replace(read(t, r, "app/billing/invoice.py"),
		"    subtotal = sum(i.price for i in items)", "    subtotal = round(sum(i.price for i in items), 2)", 1))
	shas["carol"] = r.Commit("Carol Diaz", "carol@example.com", "2026-11-03T12:00:00Z", "Round totals (#12)")

	r.Write("main.py", "import app\nPORT = 8080\napp.run(PORT)\n")
	shas["dave"] = r.Commit("Dave Kim", "dave@example.com", "2026-11-05T08:00:00Z", "Bump default port")

	r.Git("remote", "add", "origin", "git@github.com:acme/shop.git")
	return r, shas
}

func lines(ls ...string) string { return strings.Join(ls, "\n") + "\n" }

func read(t testing.TB, r *Repo, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.Dir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
