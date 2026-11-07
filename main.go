// Command whodunnit reads a stack trace and shows, for every frame in your
// code, the commit, author and pull request that last changed that line.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/techtruebrng/whodunnit/internal/analyze"
	"github.com/techtruebrng/whodunnit/internal/git"
	"github.com/techtruebrng/whodunnit/internal/report"
	"github.com/techtruebrng/whodunnit/internal/trace"
)

var version = "dev"

const usage = `whodunnit - git blame for every frame of a stack trace

Usage:
  whodunnit [flags] [trace-file]
  some-command 2>&1 | whodunnit [flags]

Reads a stack trace from trace-file or stdin (Python, JavaScript/TypeScript,
Go, Java/Kotlin, Rust, Ruby, C#, PHP) and, for each frame that points into
the current git repository, prints the commit, author, age and pull request
that last changed that line. Works offline; pull requests are found from
merge and squash commit messages.

Flags:
`

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("whodunnit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var (
		dir      = fs.String("C", ".", "run as if started in `dir` (the repository that produced the trace)")
		rev      = fs.String("rev", "HEAD", "`revision` that was running when the trace was captured (deployed SHA or tag)")
		since    = fs.String("since", "", "mark lines changed after `ref`, e.g. the previous release tag")
		format   = fs.String("format", "text", "output `format`: text, markdown or json")
		color    = fs.String("color", "auto", "colorize text output: auto, always or never")
		all      = fs.Bool("all", false, "show frames outside the repository and every suspect commit")
		remote   = fs.String("remote", "origin", "git `remote` used to build commit and PR links")
		repoURL  = fs.String("repo-url", "", "web `url` of the repository, overriding the remote (e.g. https://github.com/acme/app)")
		showVer  = fs.Bool("version", false, "print version and exit")
		srcmaps  multiFlag
		mdAlias  = fs.Bool("md", false, "shorthand for -format markdown")
		jsonFlag = fs.Bool("json", false, "shorthand for -format json")
	)
	fs.Var(&srcmaps, "sourcemaps", "`dir` containing *.map files for bundled JavaScript (repeatable)")
	fs.Usage = func() {
		fmt.Fprint(stderr, usage)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVer {
		fmt.Fprintln(stdout, "whodunnit", version)
		return 0
	}
	if *mdAlias {
		*format = "markdown"
	}
	if *jsonFlag {
		*format = "json"
	}
	switch *format {
	case "text", "markdown", "md", "json":
	default:
		fmt.Fprintf(stderr, "whodunnit: unknown format %q (want text, markdown or json)\n", *format)
		return 2
	}
	if fs.NArg() > 1 {
		fmt.Fprintln(stderr, "whodunnit: expected at most one trace file")
		return 2
	}

	input, err := readInput(fs.Arg(0), stdin, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "whodunnit:", err)
		return 1
	}
	frames := trace.Parse(input)
	if len(frames) == 0 {
		fmt.Fprintln(stderr, "whodunnit: no stack frames found in the input")
		return 1
	}

	repo, err := git.Open(*dir)
	if err != nil {
		fmt.Fprintln(stderr, "whodunnit:", err)
		return 1
	}
	rep, err := analyze.Run(repo, frames, analyze.Options{
		Rev: *rev, Since: *since, SourceMaps: srcmaps, Remote: *remote, RepoURL: *repoURL,
	})
	if err != nil {
		fmt.Fprintln(stderr, "whodunnit:", err)
		return 1
	}

	opts := report.Options{All: *all, Now: time.Now(), Color: useColor(*color, stdout)}
	switch *format {
	case "text":
		err = report.Text(stdout, rep, opts)
	case "markdown", "md":
		err = report.Markdown(stdout, rep, opts)
	case "json":
		err = report.JSON(stdout, rep)
	}
	if err != nil {
		fmt.Fprintln(stderr, "whodunnit:", err)
		return 1
	}
	return 0
}

func readInput(name string, stdin io.Reader, stderr io.Writer) (string, error) {
	if name != "" && name != "-" {
		b, err := os.ReadFile(name)
		return string(b), err
	}
	if isTerminal(stdin) {
		fmt.Fprintln(stderr, "Paste a stack trace, then press Ctrl-D (Ctrl-Z then Enter on Windows):")
	}
	b, err := io.ReadAll(stdin)
	return string(b), err
}

func useColor(mode string, w io.Writer) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	}
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	return isTerminal(w)
}

func isTerminal(v any) bool {
	f, ok := v.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}
