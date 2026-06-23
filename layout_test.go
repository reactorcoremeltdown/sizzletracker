package main

import "testing"

func TestLayoutResizeClamp(t *testing.T) {
	app := &App{ed: newEditor()}
	const h = 30
	avail := h - 3 // top + separator + status

	// Default: lower pane uses arrangeDesiredH; rows account for everything.
	th, sepY, ay, lh := app.layout(h)
	if lh != arrangeDesiredH {
		t.Errorf("default lower = %d, want %d", lh, arrangeDesiredH)
	}
	if th+lh != avail {
		t.Errorf("tracker+lower = %d, want %d", th+lh, avail)
	}
	if sepY != 1+th || ay != sepY+1 {
		t.Errorf("sepY=%d arrangeY=%d (tracker=%d)", sepY, ay, th)
	}

	// Dragging huge: tracker keeps its minimum.
	app.ed.lowerH = 1000
	th, _, _, lh = app.layout(h)
	if th < minTrackerH {
		t.Errorf("tracker %d below min %d", th, minTrackerH)
	}
	if th+lh != avail {
		t.Errorf("rows dont sum after big drag: %d != %d", th+lh, avail)
	}

	// Dragging tiny: lower pane keeps its minimum.
	app.ed.lowerH = 1
	_, _, _, lh = app.layout(h)
	if lh < minLowerH {
		t.Errorf("lower %d below min %d", lh, minLowerH)
	}
}
