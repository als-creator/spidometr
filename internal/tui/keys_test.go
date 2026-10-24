package tui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

func TestHotkeyMapsCyrillicLayout(t *testing.T) {
	cases := map[rune]rune{
		'ы': 's', 'з': 'p', 'е': 't', 'в': 'd', 'г': 'u', 'к': 'r',
		'с': 'c', 'д': 'l', 'р': 'h', 'а': 'f', 'й': 'q',
		'Ы': 's', 'З': 'p', 'Е': 't', 'В': 'd', 'Г': 'u', 'К': 'r',
		'С': 'c', 'Д': 'l', 'Р': 'h', 'А': 'f', 'Й': 'q',
		'і': 's',
	}
	for input, want := range cases {
		ev := tcell.NewEventKey(tcell.KeyRune, input, tcell.ModNone)
		if got := hotkey(ev); got != want {
			t.Errorf("hotkey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestHotkeyKeepsLatin(t *testing.T) {
	for _, r := range []rune{'s', 'p', 't', 'd', 'u', 'r', 'l', 'h', 'q'} {
		ev := tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone)
		if got := hotkey(ev); got != r {
			t.Errorf("hotkey(%q) = %q, want %q", r, got, r)
		}
	}
}

func TestHotkeyIgnoresSpecialKeys(t *testing.T) {
	ev := tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone)
	if got := hotkey(ev); got != 0 {
		t.Fatalf("hotkey(Escape) = %q, want 0", got)
	}
}
