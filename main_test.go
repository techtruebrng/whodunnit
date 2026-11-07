package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/techtruebrng/whodunnit/internal/testrepo"
)

func whodunnit(t *testing.T, stdin string, args ...string) (string, string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, strings.NewReader(stdin), &out, &errOut)
	return out.String(), errOut.String(), code
}

func traceFile(t *testing.T) string {
	p, err := filepath.Abs(filepath.Join("testdata", "shop_trace.txt"))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestCLIText(t *testing.T) {
	r, _ := testrepo.Shop(t)
	out, stderr, code := whodunnit(t, "", "-C", r.Dir, "-since", "v1.0", traceFile(t))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		"7 frames · 5 in this repo",
		"Suspect commits",
		"Carol Diaz",
		"PR #12",
		"PR #7",
		"★ new since v1.0",
		"app/billing/invoice.py:9",
		"line is past the end of the file",
		"… 1 more (--all)",
		"2 frames outside this repo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("text output missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("colors should be off when not writing to a terminal")
	}
}

func TestCLIMarkdown(t *testing.T) {
	r, shas := testrepo.Shop(t)
	out, stderr, code := whodunnit(t, "", "-C", r.Dir, "-md", traceFile(t))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	for _, want := range []string{
		"### whodunnit report",
		"[#7](https://github.com/acme/shop/pull/7)",
		"[`" + shas["bob"][:8] + "`](https://github.com/acme/shop/commit/" + shas["bob"] + ")",
		"(https://github.com/acme/shop/blob/",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("markdown missing %q\n%s", want, out)
		}
	}
}

func TestCLIJSONFromStdin(t *testing.T) {
	r, shas := testrepo.Shop(t)
	in := "Traceback (most recent call last):\n  File \"/srv/shop/app/billing/invoice.py\", line 9, in apply_discount\n"
	out, stderr, code := whodunnit(t, in, "-C", r.Dir, "-json", "-repo-url", "https://gitlab.example.com/acme/shop")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var rep struct {
		Frames []struct {
			Path  string `json:"path"`
			PR    int    `json:"pr"`
			PRURL string `json:"pr_url"`
			Blame struct {
				SHA string `json:"sha"`
			} `json:"blame"`
		} `json:"frames"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	f := rep.Frames[0]
	if f.Path != "app/billing/invoice.py" || f.PR != 7 || f.Blame.SHA != shas["bob"] ||
		f.PRURL != "https://gitlab.example.com/acme/shop/-/merge_requests/7" {
		t.Errorf("unexpected frame: %+v", f)
	}
}

func TestCLIErrors(t *testing.T) {
	r, _ := testrepo.Shop(t)
	if _, stderr, code := whodunnit(t, "just some log text\n", "-C", r.Dir); code != 1 || !strings.Contains(stderr, "no stack frames") {
		t.Errorf("no frames: code=%d stderr=%q", code, stderr)
	}
	if _, stderr, code := whodunnit(t, "", "-C", t.TempDir(), traceFile(t)); code != 1 || !strings.Contains(stderr, "not inside a git repository") {
		t.Errorf("not a repo: code=%d stderr=%q", code, stderr)
	}
	if _, stderr, code := whodunnit(t, "", "-C", r.Dir, "-rev", "nope", traceFile(t)); code != 1 || !strings.Contains(stderr, "unknown revision") {
		t.Errorf("bad rev: code=%d stderr=%q", code, stderr)
	}
	if _, _, code := whodunnit(t, "", "-format", "xml", traceFile(t)); code != 2 {
		t.Error("bad format should exit 2")
	}
	if out, _, code := whodunnit(t, "", "-version"); code != 0 || !strings.HasPrefix(out, "whodunnit ") {
		t.Errorf("version: %q %d", out, code)
	}
}
