// Package trace extracts stack frames from stack traces written by common
// language runtimes. It is deliberately forgiving: input is usually pasted
// from logs, chat or error trackers, so lines may carry prefixes, timestamps
// or mixed traces from several languages.
package trace

import (
	"path"
	"regexp"
	"strconv"
	"strings"
)

// Lang identifies the runtime that produced a frame.
type Lang string

const (
	Python Lang = "python"
	JS     Lang = "javascript"
	Go     Lang = "go"
	Java   Lang = "java"
	Rust   Lang = "rust"
	Ruby   Lang = "ruby"
	DotNet Lang = "dotnet"
	PHP    Lang = "php"
)

// Frame is one location in a stack trace.
type Frame struct {
	Index int    `json:"index"`
	Lang  Lang   `json:"lang"`
	File  string `json:"file"`
	Line  int    `json:"line"`
	Col   int    `json:"col,omitempty"`
	Func  string `json:"func,omitempty"`
	Raw   string `json:"raw"`
}

var (
	// File "/app/billing/invoice.py", line 88, in apply_discount
	rePython = regexp.MustCompile(`File "([^"]+)", line (\d+)(?:, in (.+))?`)

	// at Object.handler (/app/src/routes.js:42:17)
	// at async Promise.all (index 0)  -- no location, ignored
	reJSParen = regexp.MustCompile(`^\s*at (.+?) \((.+):(\d+):(\d+)\)\s*$`)
	// at /app/src/routes.js:42:17   (also Rust: at ./src/main.rs:10:5)
	reJSBare = regexp.MustCompile(`^\s*at (\S.*?):(\d+):(\d+)\s*$`)
	// handler@https://example.com/static/app.js:42:17  (Firefox, Safari)
	reJSAt = regexp.MustCompile(`^\s*([^@\s]*)@(\S+?):(\d+):(\d+)\s*$`)

	// \t/home/me/src/app/server.go:123 +0x1d
	reGoFile = regexp.MustCompile(`^\s+(\S+\.go):(\d+)(?:\s+\+0x[0-9a-fA-F]+)?\s*$`)

	// at com.acme.billing.Invoice.total(Invoice.java:42)
	// at java.base/java.lang.Thread.run(Thread.java:833)
	reJava = regexp.MustCompile(`^\s*at ([\w$.<>/]+)\(([\w$.-]+\.(?:java|kt|kts|scala|groovy|clj)):(\d+)\)\s*$`)

	// thread 'main' panicked at src/main.rs:2:5:
	// thread 'main' panicked at 'boom', src/main.rs:2:5
	reRustPanic = regexp.MustCompile(`panicked at (?:'.*', )?(\S+?\.rs):(\d+):(\d+)`)
	//    3: myapp::billing::total
	reRustFunc = regexp.MustCompile(`^\s*\d+:\s+(?:0x[0-9a-f]+ - )?(\S+)\s*$`)

	// app/models/invoice.rb:42:in `total'
	// from app/models/invoice.rb:42:in 'Invoice#total'
	reRuby = regexp.MustCompile(`^\s*(?:from )?(\S+?\.(?:rb|rake|erb|haml|slim)):(\d+):in [` + "`" + `'](.*?)'`)

	// at Acme.Billing.Invoice.Total() in /src/Billing/Invoice.cs:line 42
	reDotNet = regexp.MustCompile(`^\s*at (.+?) in (.+?):line (\d+)\s*$`)

	// #0 /var/www/src/Invoice.php(42): Invoice->total()
	rePHP = regexp.MustCompile(`^\s*#\d+ (\S+?\.php)\((\d+)\): (.+?)\s*$`)
	// PHP Fatal error: ... in /var/www/src/Invoice.php:42
	rePHPError = regexp.MustCompile(` in (\S+?\.php)(?: on line |:)(\d+)\b`)
)

// Parse returns every frame it can find in text, in the order they appear.
func Parse(text string) []Frame {
	var frames []Frame
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	prev := ""
	for _, raw := range lines {
		line := strings.TrimRight(raw, " \t")
		if f, ok := parseLine(line, prev); ok {
			f.Index = len(frames)
			f.Raw = strings.TrimSpace(line)
			frames = append(frames, f)
		}
		prev = line
	}
	return frames
}

func parseLine(line, prev string) (Frame, bool) {
	if m := rePython.FindStringSubmatch(line); m != nil {
		return Frame{Lang: Python, File: m[1], Line: atoi(m[2]), Func: m[3]}, true
	}
	if m := reJava.FindStringSubmatch(line); m != nil {
		return Frame{Lang: Java, File: javaPath(m[1], m[2]), Line: atoi(m[3]), Func: stripJavaModule(m[1])}, true
	}
	if m := reDotNet.FindStringSubmatch(line); m != nil {
		return Frame{Lang: DotNet, File: m[2], Line: atoi(m[3]), Func: m[1]}, true
	}
	if m := reJSParen.FindStringSubmatch(line); m != nil {
		return Frame{Lang: langByExt(m[2], JS), File: m[2], Line: atoi(m[3]), Col: atoi(m[4]), Func: m[1]}, true
	}
	if m := reJSBare.FindStringSubmatch(line); m != nil {
		lang := langByExt(m[1], JS)
		f := Frame{Lang: lang, File: m[1], Line: atoi(m[2]), Col: atoi(m[3])}
		if lang == Rust {
			if fm := reRustFunc.FindStringSubmatch(prev); fm != nil {
				f.Func = fm[1]
			}
		}
		return f, true
	}
	if m := reGoFile.FindStringSubmatch(line); m != nil {
		f := Frame{Lang: Go, File: m[1], Line: atoi(m[2])}
		f.Func = goFunc(prev)
		return f, true
	}
	if m := reRustPanic.FindStringSubmatch(line); m != nil {
		return Frame{Lang: Rust, File: m[1], Line: atoi(m[2]), Col: atoi(m[3]), Func: "panic"}, true
	}
	if m := reRuby.FindStringSubmatch(line); m != nil {
		return Frame{Lang: Ruby, File: m[1], Line: atoi(m[2]), Func: m[3]}, true
	}
	if m := rePHP.FindStringSubmatch(line); m != nil {
		return Frame{Lang: PHP, File: m[1], Line: atoi(m[2]), Func: m[3]}, true
	}
	if m := rePHPError.FindStringSubmatch(line); m != nil {
		return Frame{Lang: PHP, File: m[1], Line: atoi(m[2])}, true
	}
	if m := reJSAt.FindStringSubmatch(line); m != nil {
		return Frame{Lang: langByExt(m[2], JS), File: m[2], Line: atoi(m[3]), Col: atoi(m[4]), Func: m[1]}, true
	}
	return Frame{}, false
}

// goFunc extracts the function from the line above a Go frame location, e.g.
// "main.(*Server).handle(0xc000010000, {0x1, 0x2})" -> "main.(*Server).handle"
// or "created by main.main in goroutine 1" -> "main.main".
func goFunc(line string) string {
	line = strings.TrimSpace(line)
	if rest, ok := strings.CutPrefix(line, "created by "); ok {
		name, _, _ := strings.Cut(rest, " ")
		return name
	}
	if !strings.HasSuffix(line, ")") {
		return ""
	}
	depth := 0
	for i := len(line) - 1; i >= 0; i-- {
		switch line[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				return line[:i]
			}
		}
	}
	return ""
}

func langByExt(file string, fallback Lang) Lang {
	switch strings.ToLower(path.Ext(strings.SplitN(file, "?", 2)[0])) {
	case ".rs":
		return Rust
	case ".go":
		return Go
	case ".py":
		return Python
	case ".rb":
		return Ruby
	}
	return fallback
}

func stripJavaModule(fn string) string {
	if i := strings.LastIndex(fn, "/"); i >= 0 {
		return fn[i+1:]
	}
	return fn
}

// javaPath turns "com.acme.billing.Invoice$Line.total" + "Invoice.java" into
// "com/acme/billing/Invoice.java". JVM traces only carry the bare file name,
// so the package is recovered from the qualified method name.
func javaPath(qualified, file string) string {
	parts := strings.Split(stripJavaModule(qualified), ".")
	if len(parts) < 3 {
		return file
	}
	pkg := parts[:len(parts)-2] // drop class and method
	// Lambdas and nested classes can add segments; stop at the first one
	// that looks like a class name.
	for i, p := range pkg {
		if p != "" && p[0] >= 'A' && p[0] <= 'Z' {
			pkg = pkg[:i]
			break
		}
	}
	if len(pkg) == 0 {
		return file
	}
	return strings.Join(pkg, "/") + "/" + file
}

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
