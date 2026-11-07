package analyze

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/techtruebrng/whodunnit/internal/git"
	"github.com/techtruebrng/whodunnit/internal/testrepo"
	"github.com/techtruebrng/whodunnit/internal/trace"
)

func load(t *testing.T) []trace.Frame {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", "testdata", "shop_trace.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return trace.Parse(string(b))
}

func TestRun(t *testing.T) {
	tr, shas := testrepo.Shop(t)
	repo, err := git.Open(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(repo, load(t), Options{Since: "v1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if rep.RepoURL != "https://github.com/acme/shop" {
		t.Errorf("RepoURL = %q", rep.RepoURL)
	}

	type exp struct {
		status, path, sha string
		pr                int
		isNew             bool
	}
	want := []exp{
		{StatusBlamed, "main.py", shas["dave"], 0, true},
		{StatusBlamed, "app/billing/invoice.py", shas["carol"], 12, true},
		{StatusBlamed, "app/billing/invoice.py", shas["bob"], 7, true},
		{StatusBlamed, "app/billing/invoice.py", shas["alice"], 0, false},
		{StatusExternal, "", "", 0, false},
		{StatusOutOfRange, "app/billing/invoice.py", "", 0, false},
		{StatusNotFound, "", "", 0, false},
	}
	if len(rep.Frames) != len(want) {
		t.Fatalf("got %d frames, want %d", len(rep.Frames), len(want))
	}
	for i, w := range want {
		f := rep.Frames[i]
		sha := ""
		if f.Blame != nil {
			sha = f.Blame.SHA
		}
		isNew := f.NewSinceRef != nil && *f.NewSinceRef
		if f.Status != w.status || f.Path != w.path || sha != w.sha || f.PR != w.pr || isNew != w.isNew {
			t.Errorf("frame %d: got status=%s path=%s sha=%.8s pr=%d new=%v err=%q\n want %+v",
				i, f.Status, f.Path, sha, f.PR, isNew, f.Error, w)
		}
	}

	f := rep.Frames[2]
	if f.PRURL != "https://github.com/acme/shop/pull/7" || f.CommitURL != "https://github.com/acme/shop/commit/"+shas["bob"] {
		t.Errorf("links: %q %q", f.PRURL, f.CommitURL)
	}
	if f.Blame.Author != "Bob Ruiz" || f.Blame.Summary != "Clamp negative discounts" {
		t.Errorf("blame: %+v", f.Blame)
	}
	if f.FileCommitsSinceRef == nil || *f.FileCommitsSinceRef != 2 {
		t.Errorf("FileCommitsSinceRef = %v, want 2", f.FileCommitsSinceRef)
	}

	// Suspects: new-since-v1.0 commits first, newest first; Alice last.
	var order []string
	for _, s := range rep.Suspects {
		order = append(order, s.SHA)
	}
	wantOrder := []string{shas["dave"], shas["carol"], shas["bob"], shas["alice"]}
	if len(order) != len(wantOrder) {
		t.Fatalf("suspects = %d, want %d", len(order), len(wantOrder))
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Errorf("suspect %d = %.8s, want %.8s", i, order[i], wantOrder[i])
		}
	}
}

func TestRunAtOlderRevision(t *testing.T) {
	tr, shas := testrepo.Shop(t)
	repo, err := git.Open(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	// The trace came from v1.0, before any later change: everything is Alice's.
	rep, err := Run(repo, load(t), Options{Rev: "v1.0"})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range rep.Frames {
		if f.Blame != nil && f.Blame.SHA != shas["alice"] {
			t.Errorf("frame %d blamed %.8s at v1.0, want alice", f.Index, f.Blame.SHA)
		}
	}
}

func TestIgnoreRevsFile(t *testing.T) {
	tr, shas := testrepo.Shop(t)
	// A mechanical commit that rewrites line 2 of main.py (not just
	// whitespace, which -w already ignores)...
	tr.Write("main.py", "import app\nPORT = 8080  # noqa: E501\napp.run(PORT)\n")
	fmtSHA := tr.Commit("Bot", "bot@example.com", "2026-11-06T00:00:00Z", "Add lint pragmas")
	repo, err := git.Open(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := repo.Blame("HEAD", "main.py", 2); b.SHA != fmtSHA {
		t.Fatalf("precondition: line should blame the pragma commit, got %.8s", b.SHA)
	}
	tr.Write(".git-blame-ignore-revs", fmtSHA+"\n")
	tr.Commit("Bot", "bot@example.com", "2026-11-06T00:01:00Z", "Ignore pragma commit in blame")

	repo, err = git.Open(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(repo, trace.Parse(`File "/srv/main.py", line 2, in <module>`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	// ...is skipped, so blame lands on Dave's real change.
	if got := rep.Frames[0].Blame; got == nil || got.SHA != shas["dave"] {
		t.Fatalf("blame = %+v, want dave %.8s", got, shas["dave"])
	}
}

func TestSourceMaps(t *testing.T) {
	tr := testrepo.New(t)
	tr.Write("src/discount.ts", "export function applyDiscount(a: number, p: number) {\n  return a * (1 - p);\n}\n")
	sha := tr.Commit("Erin Park", "erin@example.com", "2026-11-01T00:00:00Z", "Add discount helper")

	// A build output directory outside the repository with a map for the bundle.
	dist := t.TempDir()
	// Generated line 1, column 0 maps to src/discount.ts line 2, column 2 (name 0).
	m := `{"version":3,"file":"main.3f2a.js","sources":["webpack:///./src/discount.ts"],"names":["applyDiscount"],"mappings":"AACEA"}`
	if err := os.WriteFile(filepath.Join(dist, "main.3f2a.js.map"), []byte(m), 0o644); err != nil {
		t.Fatal(err)
	}

	repo, err := git.Open(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	frames := trace.Parse("e@https://shop.example.com/static/main.3f2a.js:1:120")
	rep, err := Run(repo, frames, Options{SourceMaps: []string{dist}})
	if err != nil {
		t.Fatal(err)
	}
	f := rep.Frames[0]
	if f.Path != "src/discount.ts" || f.Line != 2 || f.Func != "applyDiscount" || f.Generated == nil {
		t.Fatalf("frame = %+v", f)
	}
	if f.Blame == nil || f.Blame.SHA != sha {
		t.Fatalf("blame = %+v", f.Blame)
	}
}

func TestBrokenIgnoreRevsFile(t *testing.T) {
	tr, shas := testrepo.Shop(t)
	tr.Write(".git-blame-ignore-revs", "not-a-commit\n")
	tr.Commit("Bot", "bot@example.com", "2026-11-06T00:00:00Z", "Add broken ignore file")
	repo, err := git.Open(tr.Dir)
	if err != nil {
		t.Fatal(err)
	}
	rep, err := Run(repo, trace.Parse(`File "/srv/main.py", line 2, in <module>`), Options{})
	if err != nil {
		t.Fatal(err)
	}
	if got := rep.Frames[0].Blame; got == nil || got.SHA != shas["dave"] {
		t.Fatalf("blame = %+v (err %q), want dave", got, rep.Frames[0].Error)
	}
}
