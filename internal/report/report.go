// Package report renders an analysis as terminal text, Markdown or JSON.
package report

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/techtruebrng/whodunnit/internal/analyze"
)

// Options control rendering.
type Options struct {
	Color       bool
	All         bool      // show frames outside the repo and every suspect
	Now         time.Time // reference time for "3 days ago"
	MaxSuspects int       // suspects shown unless All (default 3)
}

// JSON writes the report as indented JSON.
func JSON(w io.Writer, r *analyze.Report) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(r)
}

type palette struct{ bold, dim, yellow, red, cyan, reset string }

func colors(on bool) palette {
	if !on {
		return palette{}
	}
	return palette{"\x1b[1m", "\x1b[2m", "\x1b[33m", "\x1b[31m", "\x1b[36m", "\x1b[0m"}
}

// Text writes a human-readable report for a terminal.
func Text(w io.Writer, r *analyze.Report, o Options) error {
	o = defaults(o)
	c := colors(o.Color)
	var b strings.Builder

	inRepo := 0
	for _, f := range r.Frames {
		if f.InRepo() {
			inRepo++
		}
	}
	fmt.Fprintf(&b, "%swhodunnit%s · %d frames · %d in this repo · rev %s (%s)",
		c.bold, c.reset, len(r.Frames), inRepo, r.Rev, short(r.RevSHA))
	if r.Since != "" {
		fmt.Fprintf(&b, " · since %s", r.Since)
	}
	b.WriteString("\n")

	if len(r.Suspects) > 0 {
		fmt.Fprintf(&b, "\n%sSuspect commits%s\n", c.bold, c.reset)
		for i, s := range r.Suspects {
			if !o.All && i >= o.MaxSuspects {
				fmt.Fprintf(&b, "  %s… %d more (--all)%s\n", c.dim, len(r.Suspects)-i, c.reset)
				break
			}
			mark := "  "
			if s.NewSinceRef {
				mark = c.red + "★ " + c.reset
			}
			fmt.Fprintf(&b, "  %s%s%s%s  %s · %s", mark, c.yellow, short(s.SHA), c.reset, s.Author, ago(s.Time, o.Now))
			if s.PR > 0 {
				fmt.Fprintf(&b, " · PR #%d", s.PR)
			}
			fmt.Fprintf(&b, " · %q  %s%s%s\n", truncate(s.Summary, 60), c.dim, frameList(s.Frames), c.reset)
		}
	}

	fmt.Fprintf(&b, "\n%sFrames%s\n", c.bold, c.reset)
	hidden := 0
	for _, f := range r.Frames {
		if !f.InRepo() && !o.All {
			hidden++
			continue
		}
		loc := location(f)
		fn := ""
		if f.Func != "" {
			fn = "  " + c.cyan + f.Func + c.reset
		}
		if !f.InRepo() {
			why := "outside repo"
			if f.Status == analyze.StatusNotFound {
				why = "no matching file"
			}
			fmt.Fprintf(&b, "  %s#%-2d %s%s  (%s)%s\n", c.dim, f.Index, loc, stripColor(fn), why, c.reset)
			continue
		}
		fmt.Fprintf(&b, "  #%-2d %s%s%s%s\n", f.Index, c.bold, loc, c.reset, fn)
		if f.Generated != nil {
			fmt.Fprintf(&b, "      %svia source map from %s:%d:%d%s\n", c.dim, f.Generated.File, f.Generated.Line, f.Generated.Col, c.reset)
		}
		if f.Blame == nil {
			fmt.Fprintf(&b, "      %s%s%s\n", c.red, f.Error, c.reset)
			continue
		}
		bl := f.Blame
		fmt.Fprintf(&b, "      %s%s%s  %s  %s", c.yellow, short(bl.SHA), c.reset, bl.Author, ago(bl.Time, o.Now))
		if f.PR > 0 {
			fmt.Fprintf(&b, "  PR #%d", f.PR)
		}
		if f.NewSinceRef != nil && *f.NewSinceRef {
			fmt.Fprintf(&b, "  %s★ new since %s%s", c.red, r.Since, c.reset)
		} else if f.FileCommitsSinceRef != nil && *f.FileCommitsSinceRef > 0 {
			fmt.Fprintf(&b, "  %sfile changed %d× since %s%s", c.dim, *f.FileCommitsSinceRef, r.Since, c.reset)
		}
		fmt.Fprintf(&b, "\n      %s%s%s\n", c.dim, truncate(bl.Summary, 72), c.reset)
		if len(f.Candidates) > 1 {
			fmt.Fprintf(&b, "      %sambiguous: also matches %s%s\n", c.dim, strings.Join(f.Candidates[1:], ", "), c.reset)
		}
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "  %s… %d frame%s outside this repo (--all to show)%s\n", c.dim, hidden, plural(hidden), c.reset)
	}
	if inRepo == 0 {
		fmt.Fprintf(&b, "\nNo frames matched files in %s. Run whodunnit inside the repository that produced the trace, or pass -C <dir>.\n", r.Repo)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// Markdown writes a report suitable for issues, incident docs and chat.
func Markdown(w io.Writer, r *analyze.Report, o Options) error {
	o = defaults(o)
	var b strings.Builder
	fmt.Fprintf(&b, "### whodunnit report\n\nRevision `%s` (`%s`)", r.Rev, short(r.RevSHA))
	if r.Since != "" {
		fmt.Fprintf(&b, ", compared with `%s`", r.Since)
	}
	b.WriteString(".\n\n")

	if len(r.Suspects) > 0 {
		b.WriteString("**Suspect commits**\n\n| | Commit | Author | When | PR | Summary | Frames |\n|---|---|---|---|---|---|---|\n")
		for i, s := range r.Suspects {
			if !o.All && i >= o.MaxSuspects {
				break
			}
			mark := ""
			if s.NewSinceRef {
				mark = "★"
			}
			fmt.Fprintf(&b, "| %s | %s | %s | %s | %s | %s | %s |\n",
				mark, link(code(short(s.SHA)), s.CommitURL), cell(s.Author), ago(s.Time, o.Now),
				prCell(s.PR, s.PRURL), cell(truncate(s.Summary, 80)), frameList(s.Frames))
		}
		b.WriteString("\n")
	}

	b.WriteString("**Frames**\n\n| # | Location | Function | Last changed | Author | When | PR |\n|---|---|---|---|---|---|---|\n")
	hidden := 0
	for _, f := range r.Frames {
		if !f.InRepo() && !o.All {
			hidden++
			continue
		}
		loc := link(code(location(f)), f.FileURL)
		fn := ""
		if f.Func != "" {
			fn = code(f.Func)
		}
		if f.Blame == nil {
			note := f.Error
			if !f.InRepo() {
				note = "outside repo"
			}
			fmt.Fprintf(&b, "| %d | %s | %s | _%s_ | | | |\n", f.Index, loc, fn, cell(note))
			continue
		}
		when := ago(f.Blame.Time, o.Now)
		if f.NewSinceRef != nil && *f.NewSinceRef {
			when += " ★"
		}
		fmt.Fprintf(&b, "| %d | %s | %s | %s | %s | %s | %s |\n", f.Index, loc, fn,
			link(code(short(f.Blame.SHA)), f.CommitURL)+" "+cell(truncate(f.Blame.Summary, 50)),
			cell(f.Blame.Author), when, prCell(f.PR, f.PRURL))
	}
	if hidden > 0 {
		fmt.Fprintf(&b, "\n_%d frame%s outside this repository not shown._\n", hidden, plural(hidden))
	}
	if r.Since != "" {
		fmt.Fprintf(&b, "\n★ = line last changed after `%s`.\n", r.Since)
	}
	_, err := io.WriteString(w, b.String())
	return err
}

func defaults(o Options) Options {
	if o.Now.IsZero() {
		o.Now = time.Now()
	}
	if o.MaxSuspects == 0 {
		o.MaxSuspects = 3
	}
	return o
}

func location(f analyze.Frame) string {
	p := f.Path
	if p == "" {
		p = f.File
	}
	return fmt.Sprintf("%s:%d", p, f.Line)
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}

func frameList(idx []int) string {
	parts := make([]string, len(idx))
	for i, n := range idx {
		parts[i] = fmt.Sprintf("#%d", n)
	}
	label := "frame "
	if len(idx) > 1 {
		label = "frames "
	}
	return label + strings.Join(parts, ", ")
}

// ago formats t relative to now ("3 days ago").
func ago(t, now time.Time) string {
	d := now.Sub(t)
	if d < 0 {
		return t.Format("2006-01-02")
	}
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return unit(int(d/time.Minute), "minute")
	case d < 24*time.Hour:
		return unit(int(d/time.Hour), "hour")
	case d < 30*24*time.Hour:
		return unit(int(d/(24*time.Hour)), "day")
	case d < 365*24*time.Hour:
		return unit(int(d/(30*24*time.Hour)), "month")
	}
	return unit(int(d/(365*24*time.Hour)), "year")
}

func unit(n int, name string) string {
	if n == 1 {
		return "1 " + name + " ago"
	}
	return fmt.Sprintf("%d %ss ago", n, name)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func stripColor(s string) string {
	for _, code := range []string{"\x1b[1m", "\x1b[2m", "\x1b[33m", "\x1b[31m", "\x1b[36m", "\x1b[0m"} {
		s = strings.ReplaceAll(s, code, "")
	}
	return s
}

func code(s string) string { return "`" + strings.ReplaceAll(s, "`", "'") + "`" }

func cell(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

func link(text, url string) string {
	if url == "" {
		return text
	}
	return "[" + text + "](" + url + ")"
}

func prCell(n int, url string) string {
	if n == 0 {
		return ""
	}
	return link(fmt.Sprintf("#%d", n), url)
}
