package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadSave(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	if s, err := Load(); err != nil || s != (Settings{}) {
		t.Fatalf("Load empty = %+v, %v", s, err)
	}

	want := Settings{Theme: "nord", Layout: 2}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("Load = %+v, want %+v", got, want)
	}
	path := filepath.Join(dir, "spidometr", "config.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 600", info.Mode().Perm())
	}
}
