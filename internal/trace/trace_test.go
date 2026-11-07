package trace

import (
	"os"
	"path/filepath"
	"testing"
)

type want struct {
	lang Lang
	file string
	line int
	fn   string
}

func TestParseFixtures(t *testing.T) {
	cases := map[string][]want{
		"python.txt": {
			{Python, "/usr/lib/python3.12/runpy.py", 198, "_run_module_as_main"},
			{Python, "/app/billing/cli.py", 14, "main"},
			{Python, "/app/billing/invoice.py", 9, "total"},
			{Python, "/app/.venv/lib/python3.12/site-packages/decimal_utils/core.py", 3, "apply_discount"},
		},
		"node.txt": {
			{JS, "/srv/app/src/billing/discount.js", 12, "applyDiscount"},
			{JS, "/srv/app/src/billing/invoice.js", 31, "Invoice.total"},
			{JS, "/srv/app/node_modules/express/lib/router/layer.js", 95, "Object.<anonymous>"},
			{JS, "node:internal/process/task_queues", 95, ""},
			{JS, "/srv/app/src/server.js", 8, ""},
		},
		"firefox.txt": {
			{JS, "https://shop.example.com/static/js/main.3f2a1c.js", 1, "applyDiscount"},
			{JS, "https://shop.example.com/static/js/main.3f2a1c.js", 1, "total"},
			{JS, "https://shop.example.com/static/js/main.3f2a1c.js", 2, ""},
		},
		"go.txt": {
			{Go, "/home/runner/work/shop/shop/internal/billing/invoice.go", 27, "github.com/acme/shop/internal/billing.(*Invoice).Total"},
			{Go, "/home/runner/work/shop/shop/cmd/shop/main.go", 11, "main.main"},
			{Go, "/usr/local/go/src/net/http/server.go", 3285, "net/http.(*Server).Serve"},
		},
		"java.txt": {
			{Java, "com/acme/billing/Discount.java", 17, "com.acme.billing.Discount.apply"},
			{Java, "com/acme/billing/Invoice.kt", 42, "com.acme.billing.Invoice$Line.total"},
			{Java, "com/acme/billing/Invoice.java", 30, "com.acme.billing.Invoice.lambda$total$0"},
			{Java, "java/util/stream/ReferencePipeline.java", 197, "java.util.stream.ReferencePipeline$3$1.accept"},
			{Java, "com/acme/App.java", 9, "com.acme.App.main"},
		},
		"rust.txt": {
			{Rust, "src/billing.rs", 21, "panic"},
			{Rust, "/rustc/90b35a6239c3d8bdabc530a6a0816f7ff89a0aaf/library/std/src/panicking.rs", 665, "rust_begin_unwind"},
			{Rust, "./src/billing.rs", 21, "shop::billing::apply_discount"},
			{Rust, "./src/main.rs", 6, "shop::main"},
		},
		"ruby.txt": {
			{Ruby, "/app/app/models/invoice.rb", 42, "total"},
			{Ruby, "/app/app/models/invoice.rb", 10, "block in subtotal"},
			{Ruby, "/usr/local/bundle/gems/activesupport-7.1.3/lib/active_support/callbacks.rb", 121, "block in run_callbacks"},
			{Ruby, "app/controllers/invoices_controller.rb", 7, "InvoicesController#show"},
		},
		"dotnet.txt": {
			{DotNet, `C:\build\src\Billing\Discount.cs`, 17, "Acme.Billing.Discount.Apply(Decimal amount)"},
			{DotNet, `C:\build\src\Billing\Invoice.cs`, 30, "Acme.Billing.Invoice.Total()"},
		},
		"php.txt": {
			{PHP, "/var/www/src/Billing/Discount.php", 17, ""},
			{PHP, "/var/www/src/Billing/Invoice.php", 30, "Discount->apply(-5)"},
			{PHP, "/var/www/public/index.php", 9, "Invoice->total()"},
		},
	}
	for name, wants := range cases {
		t.Run(name, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "traces", name))
			if err != nil {
				t.Fatal(err)
			}
			got := Parse(string(data))
			if len(got) != len(wants) {
				for _, f := range got {
					t.Logf("got %+v", f)
				}
				t.Fatalf("got %d frames, want %d", len(got), len(wants))
			}
			for i, w := range wants {
				g := got[i]
				if g.Lang != w.lang || g.File != w.file || g.Line != w.line || g.Func != w.fn || g.Index != i {
					t.Errorf("frame %d:\n got  {%s %q %d %q idx=%d}\n want {%s %q %d %q}", i, g.Lang, g.File, g.Line, g.Func, g.Index, w.lang, w.file, w.line, w.fn)
				}
			}
		})
	}
}

func TestParseTolerantOfPrefixes(t *testing.T) {
	in := `2026-11-07T10:00:00Z ERROR worker-3   File "/app/jobs/sync.py", line 77, in run
[pod/api-7d9] 	at com.acme.Api.handle(Api.java:120)`
	got := Parse(in)
	if len(got) != 1 || got[0].File != "/app/jobs/sync.py" || got[0].Line != 77 {
		t.Fatalf("python with log prefix: %+v", got)
	}
}

func TestParseIgnoresProse(t *testing.T) {
	in := "Deploy finished at 10:42:17\nSee docs at https://example.com/a:b\nnothing here"
	if got := Parse(in); len(got) != 0 {
		t.Fatalf("expected no frames, got %+v", got)
	}
}

func TestGoFunc(t *testing.T) {
	for in, want := range map[string]string{
		"main.(*Server).handle(0xc000010000, {0x1, 0x2})": "main.(*Server).handle",
		"main.main()":                          "main.main",
		"created by main.start in goroutine 7": "main.start",
		"goroutine 1 [running]:":               "",
		"panic({0x4b2c40?, 0x5d2f10?})":        "panic",
	} {
		if got := goFunc(in); got != want {
			t.Errorf("goFunc(%q) = %q, want %q", in, got, want)
		}
	}
}
