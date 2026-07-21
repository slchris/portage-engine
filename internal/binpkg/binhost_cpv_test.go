package binpkg

import "testing"

func TestCpvFromPathGpkgStripsBuildID(t *testing.T) {
	cases := []struct {
		rel  string
		gpkg bool
		want string
	}{
		{"app-misc/screenfetch/screenfetch-3.9.9-1.gpkg.tar", true, "app-misc/screenfetch-3.9.9"},
		{"dev-libs/oniguruma/oniguruma-6.9.10-1.gpkg.tar", true, "dev-libs/oniguruma-6.9.10"},
		{"app-editors/vim/vim-9.0.100-r2-3.gpkg.tar", true, "app-editors/vim-9.0.100-r2"},
		{"app-misc/foo/foo-1-1.gpkg.tar", true, "app-misc/foo-1"},
		{"app-misc/jq-1.7.tbz2", false, "app-misc/jq-1.7"}, // legacy: no build id stripping
	}
	for _, c := range cases {
		if got := cpvFromPath(c.rel, c.gpkg); got != c.want {
			t.Errorf("cpvFromPath(%q,%v)=%q want %q", c.rel, c.gpkg, got, c.want)
		}
	}
}

func TestGpkgBuildID(t *testing.T) {
	if got := gpkgBuildID("app-misc/screenfetch/screenfetch-3.9.9-1.gpkg.tar"); got != "1" {
		t.Errorf("build id = %q want 1", got)
	}
	if got := gpkgBuildID("a/b/b-1.2.3-r4-12.gpkg.tar"); got != "12" {
		t.Errorf("build id = %q want 12", got)
	}
}
