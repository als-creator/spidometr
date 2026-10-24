package theme

import "testing"

func TestAllThemesAreComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, th := range All() {
		if th.ID == "" || th.Name == "" {
			t.Fatalf("theme without id/name: %+v", th)
		}
		if seen[th.ID] {
			t.Fatalf("duplicate theme id %q", th.ID)
		}
		seen[th.ID] = true
		for name, value := range map[string]string{
			"background": th.Background,
			"foreground": th.Foreground,
			"borders":    th.Borders,
			"highlight":  th.Highlight,
			"header":     th.HeaderBg,
			"help":       th.HelpBg,
			"helpFg":     th.HelpFg,
			"modal":      th.ModalBg,
			"muted":      th.Muted,
			"download":   th.Download,
			"upload":     th.Upload,
			"red":        th.Red,
		} {
			if value == "" {
				t.Fatalf("theme %q has empty %s", th.ID, name)
			}
		}
	}
	if !seen["orange"] || !seen["classic"] || !seen["ayu"] || !seen["tokyonight"] {
		t.Fatalf("required themes are missing: %v", seen)
	}
}

func TestThemeOrder(t *testing.T) {
	themes := All()
	if len(themes) < 2 {
		t.Fatal("not enough themes")
	}
	if got := themes[len(themes)-2].ID; got != "orange" {
		t.Fatalf("penultimate theme = %q, want orange", got)
	}
	if got := themes[len(themes)-1].ID; got != "nord" {
		t.Fatalf("last theme = %q, want nord", got)
	}
}

func TestDefaultIsOriginal(t *testing.T) {
	if got := Default().ID; got != "classic" {
		t.Fatalf("Default() = %q, want classic", got)
	}
	if got := Default().Name; got != "Original" {
		t.Fatalf("Default().Name = %q, want Original", got)
	}
}

func TestGet(t *testing.T) {
	if th, ok := Get("nord"); !ok || th.ID != "nord" {
		t.Fatalf("Get(nord) = %+v, %v", th, ok)
	}
	if _, ok := Get("soma"); ok {
		t.Fatal("Get(soma) unexpectedly succeeded")
	}
	if _, ok := Get("missing"); ok {
		t.Fatal("Get(missing) unexpectedly succeeded")
	}
}
