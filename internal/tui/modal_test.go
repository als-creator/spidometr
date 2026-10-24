package tui

import (
	"testing"

	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func TestModalArrowsMoveSelection(t *testing.T) {
	list := tview.NewList()
	list.AddItem("one", "", 0, nil)
	list.AddItem("two", "", 0, nil)
	list.AddItem("three", "", 0, nil)

	r := &Runner{activeModal: "sources", list: list}

	if !r.handleModalKey(tcell.NewEventKey(tcell.KeyDown, 0, tcell.ModNone)) {
		t.Fatal("down was not handled")
	}
	if got := list.GetCurrentItem(); got != 1 {
		t.Fatalf("after down current = %d, want 1", got)
	}

	if !r.handleModalKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)) {
		t.Fatal("up was not handled")
	}
	if got := list.GetCurrentItem(); got != 0 {
		t.Fatalf("after up current = %d, want 0", got)
	}

	// Список зациклен: вверх с первого элемента уходит в конец.
	if !r.handleModalKey(tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone)) {
		t.Fatal("wrapping up was not handled")
	}
	if got := list.GetCurrentItem(); got != 2 {
		t.Fatalf("after wrapping up current = %d, want 2", got)
	}
}

func TestModalJKMoveSelection(t *testing.T) {
	list := tview.NewList()
	list.AddItem("one", "", 0, nil)
	list.AddItem("two", "", 0, nil)

	r := &Runner{activeModal: "sources", list: list}

	if !r.handleModalKey(tcell.NewEventKey(tcell.KeyRune, 'j', tcell.ModNone)) {
		t.Fatal("j was not handled")
	}
	if got := list.GetCurrentItem(); got != 1 {
		t.Fatalf("after j current = %d, want 1", got)
	}

	if !r.handleModalKey(tcell.NewEventKey(tcell.KeyRune, 'k', tcell.ModNone)) {
		t.Fatal("k was not handled")
	}
	if got := list.GetCurrentItem(); got != 0 {
		t.Fatalf("after k current = %d, want 0", got)
	}
}
