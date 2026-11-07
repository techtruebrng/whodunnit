# whodunnit

**`git blame` for every frame of a stack trace.**

Paste a crash, get the commit, author, age and pull request that last touched each line of *your* code in it, plus which of those changed since the last release.

```console
$ pbpaste | whodunnit --since v1.4.0
whodunnit · 4 frames · 3 in this repo · rev HEAD (c6f1e8dd) · since v1.4.0

Suspect commits
  ★ 9d22acc8  Priya Shah · 3 days ago · PR #482 · "Support multi-rule coupons"  frame #3
  ★ c6f1e8dd  Sam Lee · 18 days ago · PR #455 · "Round subtotal before discount (#455)"  frame #2
    a1af9bfe  Alice Ng · 1 year ago · "Add invoice model"  frame #1

Frames
  #1  app/cli.py:5  main
      a1af9bfe  Alice Ng  1 year ago
      Add invoice model
  #2  app/billing/invoice.py:11  total
      c6f1e8dd  Sam Lee  18 days ago  PR #455  ★ new since v1.4.0
      Round subtotal before discount (#455)
  #3  app/billing/discount.py:4  apply_discount
      9d22acc8  Priya Shah  3 days ago  PR #482  ★ new since v1.4.0
      Support multi-rule coupons
  … 1 frame outside this repo (--all to show)
```

Hosted error trackers have this as "suspect commits". whodunnit gives you the same thing locally, for any trace from anywhere (a log file, a CI run, a Slack message, a customer email), with no account, no SDK and no network access.

## Install

```sh
go install github.com/techtruebrng/whodunnit@latest
```

Or download a binary from [Releases](https://github.com/techtruebrng/whodunnit/releases). The only runtime requirement is `git`.

## Usage

Run it inside the repository that produced the trace:

```sh
whodunnit crash.txt                    # from a file
pbpaste | whodunnit                     # macOS clipboard
xclip -o | whodunnit                    # Linux clipboard
kubectl logs api-7d9 | whodunnit        # straight from logs; non-trace lines are ignored
whodunnit                               # then paste and press Ctrl-D
```

Useful flags:

| Flag | What it does |
|---|---|
| `--rev <sha\|tag>` | Blame the code as it was when the trace happened (the deployed version). Default `HEAD`. |
| `--since <ref>` | Mark (★) lines last changed after `ref`, usually the previous release, and rank those commits first. |
| `--md` / `--json` | Markdown for issues and incident docs (with links), or JSON for scripts. |
| `--sourcemaps <dir>` | Map minified JavaScript frames back to original sources using `*.map` files. Repeatable. |
| `--all` | Also list frames outside the repo and every suspect commit. |
| `-C <dir>` | Use the repository at `dir`. |
| `--repo-url <url>` | Web URL for links when the `origin` remote isn't the forge (e.g. a mirror). |

Paste an incident-ready table into an issue:

```sh
whodunnit --rev "$DEPLOYED_SHA" --since v1.4.0 --md crash.txt | gh issue comment 123 -F -
```

## Supported traces

| Language | Example frame |
|---|---|
| Python | `File "/app/billing/invoice.py", line 11, in total` |
| JavaScript / TypeScript (V8) | `at Invoice.total (/srv/app/src/invoice.js:31:12)` |
| JavaScript (Firefox, Safari) | `total@https://shop.example.com/static/main.js:1:3301` |
| Go | `\t/home/runner/work/shop/internal/billing/invoice.go:27 +0x1c` |
| Java / Kotlin / Scala | `at com.acme.billing.Invoice.total(Invoice.java:30)` |
| Rust | `at ./src/billing.rs:21:9` and `panicked at src/billing.rs:21:9` |
| Ruby | `app/models/invoice.rb:42:in 'Invoice#total'` |
| C# / .NET | `at Acme.Billing.Invoice.Total() in C:\build\src\Billing\Invoice.cs:line 30` |
| PHP | `#0 /var/www/src/Billing/Invoice.php(30): Invoice->total()` |

Traces can carry log prefixes (timestamps, pod names), and several traces can be mixed in one input.

## How it works

1. **Parse** every frame it recognizes; everything else in the input is ignored.
2. **Match** each frame's path to a file in the repo. Traces are written on servers, in containers and in CI, so absolute paths like `/srv/app/...` or `/home/runner/work/...` are matched by their trailing path components. JVM frames are mapped through their package name. Dependency and runtime frames (`site-packages`, `node_modules`, Go module cache, the JDK and so on) are skipped. When two files match equally well, it says so instead of guessing silently.
3. **Blame** the exact line at `--rev`, ignoring whitespace-only changes and anything listed in `.git-blame-ignore-revs`.
4. **Find the PR** offline from history: squash merges (`Fix totals (#123)`), GitHub and Bitbucket merge commits, and GitLab `See merge request group/app!123`. For a regular merge it finds the merge on your main line that brought the commit in.
5. **Rank suspects**: commits that are new since `--since` come first, then the most recent.

If a line number is past the end of the file, the trace almost certainly came from a different version. whodunnit tells you so; pass `--rev` with the deployed SHA.

## Limitations

- Line-level blame shows who last *touched* a line, not who introduced the bug. Treat suspects as the place to start reading, not a verdict.
- PR detection depends on merge and squash commit messages. Rebase-merged commits without a PR number in the message show no PR.
- Links are built for GitHub, GitLab and Bitbucket (including self-hosted instances whose hostname contains `gitlab` or `bitbucket`). Use `--repo-url` for anything else.

## Development

```sh
go test ./...
```

The tests build throwaway git repositories with known histories (merge commits, squash merges, tags and ignore-revs), so they need `git` on your PATH. No other dependencies.

Adding a language usually means one regex in `internal/trace/trace.go` and a fixture in `testdata/traces/`. Pull requests are welcome.

## License

MIT
