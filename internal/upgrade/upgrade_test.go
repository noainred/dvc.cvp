package upgrade

import (
	"runtime"
	"testing"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"v0.2.0", "v0.1.0", true},
		{"v0.1.1", "v0.1.0", true},
		{"v1.0.0", "v0.9.9", true},
		{"v0.1.0", "v0.1.0", false},
		{"v0.1.0", "v0.2.0", false},
		{"0.2.0", "v0.1.0", true},       // missing "v" prefix
		{"v0.2.0-rc1", "v0.1.0", true},  // pre-release suffix ignored
		{"v0.1.0", "v0.1.0-rc1", false}, // equal numeric core
	}
	for _, c := range cases {
		if got := Newer(c.a, c.b); got != c.want {
			t.Errorf("Newer(%q,%q)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func TestPickAsset(t *testing.T) {
	want := "dvc-cvp_" + runtime.GOOS + "_" + runtime.GOARCH
	rel := ghRelease{Assets: []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}{
		{Name: "dvc-cvp_other_arch", URL: "no"},
		{Name: want + ".tar.gz", URL: "yes"},
	}}
	if got := pickAsset(rel, "dvc-cvp_{os}_{arch}"); got != "yes" {
		t.Fatalf("pickAsset matched %q want yes", got)
	}

	// Single-asset fallback.
	single := ghRelease{Assets: []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	}{{Name: "anything", URL: "only"}}}
	if got := pickAsset(single, "dvc-cvp_{os}_{arch}"); got != "only" {
		t.Fatalf("pickAsset single fallback=%q want only", got)
	}

	// No match, multiple assets -> empty.
	if got := pickAsset(rel, "nomatch_{os}"); got != "" {
		t.Fatalf("pickAsset no-match=%q want empty", got)
	}
}
