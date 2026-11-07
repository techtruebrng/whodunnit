package git

import "testing"

func TestParseForge(t *testing.T) {
	cases := map[string]Forge{
		"git@github.com:acme/shop.git":                        {"github", "https://github.com/acme/shop"},
		"https://github.com/acme/shop":                        {"github", "https://github.com/acme/shop"},
		"https://x-token:abc@github.com/acme/shop.git":        {"github", "https://github.com/acme/shop"},
		"ssh://git@gitlab.example.com:2222/group/sub/app.git": {"gitlab", "https://gitlab.example.com/group/sub/app"},
		"git@bitbucket.org:team/app.git":                      {"bitbucket", "https://bitbucket.org/team/app"},
	}
	for in, want := range cases {
		got, ok := ParseForge(in)
		if !ok || got != want {
			t.Errorf("ParseForge(%q) = %+v, %v; want %+v", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "/local/path/repo", "nonsense"} {
		if _, ok := ParseForge(bad); ok {
			t.Errorf("ParseForge(%q) should fail", bad)
		}
	}
}

func TestForgeLinks(t *testing.T) {
	gl := Forge{"gitlab", "https://gitlab.com/g/app"}
	if got := gl.PullURL(5); got != "https://gitlab.com/g/app/-/merge_requests/5" {
		t.Error(got)
	}
	gh := Forge{"github", "https://github.com/a/b"}
	if got := gh.FileURL("abc", "x/y.go", 3); got != "https://github.com/a/b/blob/abc/x/y.go#L3" {
		t.Error(got)
	}
}

func TestPRFromMessage(t *testing.T) {
	cases := map[string]int{
		"Merge pull request #42 from a/b\n\nbody":                    42,
		"Fix rounding (#913)":                                        913,
		"Merged in feature/x (pull request #8)\n\nApproved-by: z":    8,
		"Merge branch 'x' into 'main'\n\nSee merge request g/app!77": 77,
		"Refs #12 in body only\n\nCloses #12":                        0,
	}
	for msg, want := range cases {
		n, ok := prFromMessage(msg)
		if (want == 0 && ok) || n != want {
			t.Errorf("prFromMessage(%q) = %d, %v; want %d", msg, n, ok, want)
		}
	}
}
