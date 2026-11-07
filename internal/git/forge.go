package git

import (
	"fmt"
	"net/url"
	"strings"
)

// Forge builds web links for a hosted repository.
type Forge struct {
	Kind string // "github", "gitlab" or "bitbucket"
	Base string // e.g. https://github.com/acme/app
}

// ParseForge turns a remote URL (https, ssh or scp-style) or a plain web URL
// into a Forge. It returns false when the URL cannot be interpreted.
func ParseForge(remote string) (Forge, bool) {
	remote = strings.TrimSpace(remote)
	if remote == "" {
		return Forge{}, false
	}
	var host, p string
	switch {
	case strings.Contains(remote, "://"):
		u, err := url.Parse(remote)
		if err != nil || u.Host == "" {
			return Forge{}, false
		}
		host, p = u.Hostname(), u.Path
	case strings.Contains(remote, ":"):
		// scp-style: git@github.com:acme/app.git
		at := strings.LastIndex(remote, "@")
		hp := remote[at+1:]
		h, rest, _ := strings.Cut(hp, ":")
		host, p = h, rest
	default:
		return Forge{}, false
	}
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	if host == "" || p == "" {
		return Forge{}, false
	}
	// Self-hosted GitLab and Bitbucket usually carry the product name in the
	// host. Anything else gets GitHub-style links, which also fit GitHub
	// Enterprise; --repo-url overrides this guess.
	kind := "github"
	switch {
	case strings.Contains(host, "gitlab"):
		kind = "gitlab"
	case strings.Contains(host, "bitbucket"):
		kind = "bitbucket"
	}
	host = strings.TrimPrefix(host, "ssh.")
	return Forge{Kind: kind, Base: "https://" + host + "/" + p}, true
}

// CommitURL links to a commit.
func (f Forge) CommitURL(sha string) string {
	switch f.Kind {
	case "gitlab":
		return f.Base + "/-/commit/" + sha
	case "bitbucket":
		return f.Base + "/commits/" + sha
	}
	return f.Base + "/commit/" + sha
}

// PullURL links to a pull (merge) request.
func (f Forge) PullURL(n int) string {
	switch f.Kind {
	case "gitlab":
		return fmt.Sprintf("%s/-/merge_requests/%d", f.Base, n)
	case "bitbucket":
		return fmt.Sprintf("%s/pull-requests/%d", f.Base, n)
	}
	return fmt.Sprintf("%s/pull/%d", f.Base, n)
}

// FileURL links to a line of a file at a revision.
func (f Forge) FileURL(rev, file string, line int) string {
	switch f.Kind {
	case "gitlab":
		return fmt.Sprintf("%s/-/blob/%s/%s#L%d", f.Base, rev, file, line)
	case "bitbucket":
		return fmt.Sprintf("%s/src/%s/%s#lines-%d", f.Base, rev, file, line)
	}
	return fmt.Sprintf("%s/blob/%s/%s#L%d", f.Base, rev, file, line)
}
