package resolve

import (
	"reflect"
	"testing"
)

var repoFiles = []string{
	"main.py",
	"app/billing/invoice.py",
	"app/decoder.py",
	"src/billing/discount.ts",
	"src/main/java/com/acme/billing/Discount.java",
	"internal/billing/invoice.go",
	"cmd/shop/main.go",
	"svc/a/handler.go",
	"svc/b/handler.go",
	"src/Billing/Invoice.cs",
}

func TestResolve(t *testing.T) {
	ix := NewIndex(repoFiles)
	cases := []struct {
		in   string
		want Result
	}{
		{"/app/main.py", Result{Path: "main.py"}},
		{"/srv/app/app/billing/invoice.py", Result{Path: "app/billing/invoice.py"}},
		{"webpack:///./src/billing/discount.ts", Result{Path: "src/billing/discount.ts"}},
		{"webpack://shop/./src/billing/discount.ts?abc", Result{Path: "src/billing/discount.ts"}},
		{"com/acme/billing/Discount.java", Result{Path: "src/main/java/com/acme/billing/Discount.java"}},
		{"/home/runner/work/shop/shop/internal/billing/invoice.go", Result{Path: "internal/billing/invoice.go"}},
		{"github.com/acme/shop/cmd/shop/main.go", Result{Path: "cmd/shop/main.go"}},
		{`C:\build\src\Billing\Invoice.cs`, Result{Path: "src/Billing/Invoice.cs"}},
		{"file:///home/me/shop/cmd/shop/main.go", Result{Path: "cmd/shop/main.go"}},
		// Only the basename matches and neither path is consumed: reject.
		{"/usr/share/tools/json/decoder.py", Result{}},
		// Bare name with two equally good candidates.
		{"handler.go", Result{Path: "svc/a/handler.go", Candidates: []string{"svc/a/handler.go", "svc/b/handler.go"}}},
		{"/app/.venv/lib/python3.12/site-packages/app/decoder.py", Result{External: true}},
		{"/srv/app/node_modules/express/lib/router/layer.js", Result{External: true}},
		{"node:internal/process/task_queues", Result{External: true}},
		{"<frozen importlib._bootstrap>", Result{External: true}},
		{"/usr/local/go/src/net/http/server.go", Result{External: true}},
		{"/nope/missing.py", Result{}},
	}
	for _, c := range cases {
		if got := ix.Resolve(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Resolve(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestClean(t *testing.T) {
	for in, want := range map[string]string{
		"https://shop.example.com/static/js/main.js?v=3": "/static/js/main.js",
		"./src/x.ts":                     "src/x.ts",
		"webpack-internal:///./src/x.ts": "src/x.ts",
		`D:\a\b.cs`:                      "/a/b.cs",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}
