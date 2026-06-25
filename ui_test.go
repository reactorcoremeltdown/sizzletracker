package main

import (
	"testing"

	"github.com/gdamore/tcell/v2"
)

// rowAllBg reports whether every cell in row y (0..w-1) has background bg.
func rowAllBg(cells []tcell.SimCell, w, y int, bg tcell.Color) bool {
	for x := 0; x < w; x++ {
		if _, got, _ := cells[y*w+x].Style.Decompose(); got != bg {
			return false
		}
	}
	return true
}

func TestDrawSettingsSections(t *testing.T) {
	initStyles()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	const w, h = 100, 40
	sc.SetSize(w, h)

	a := &App{screen: sc, ed: newEditor()}
	a.ed.view = ViewSettings
	a.drawSettings(1, h-2, w)
	sc.Show()
	cells, gw, _ := sc.GetContents()
	if gw != w {
		t.Fatalf("width = %d, want %d", gw, w)
	}

	_, projBg, _ := stySecProj.Decompose()
	_, midiBg, _ := stySecMidi.Decompose()
	_, keysBg, _ := stySecKeys.Decompose()
	_, panelBg, _ := styKeyPanel.Decompose()

	// Layout: SETTINGS at y=1; Project banner y=3; MIDI banner y=6;
	// Hotkeys banner y=11; panel rows from y=12 down.
	if !rowAllBg(cells, w, 3, projBg) {
		t.Errorf("row 3 is not a full-width Project banner")
	}
	if !rowAllBg(cells, w, 6, midiBg) {
		t.Errorf("row 6 is not a full-width MIDI banner")
	}
	if !rowAllBg(cells, w, 11, keysBg) {
		t.Errorf("row 11 is not a full-width Hotkeys banner")
	}
	if !rowAllBg(cells, w, 13, panelBg) {
		t.Errorf("row 13 is not part of the hotkey panel background")
	}

	// The three section colors and the panel must be visually distinct.
	colors := []tcell.Color{projBg, midiBg, keysBg, panelBg}
	for i := 0; i < len(colors); i++ {
		for j := i + 1; j < len(colors); j++ {
			if colors[i] == colors[j] {
				t.Errorf("section colors %d and %d are identical (%v)", i, j, colors[i])
			}
		}
	}
}
