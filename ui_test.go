package main

import (
	"strings"
	"testing"

	"github.com/gdamore/tcell/v2"
)

// simScreenText reconstructs the simulation screen as newline-joined rows.
func simScreenText(sc tcell.SimulationScreen) string {
	cells, w, h := sc.GetContents()
	var sb strings.Builder
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if r := cells[y*w+x].Runes; len(r) > 0 {
				sb.WriteRune(r[0])
			} else {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

func TestTopBarHasAbout(t *testing.T) {
	initStyles()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	const w, h = 120, 10
	sc.SetSize(w, h)
	a := &App{screen: sc, ed: newEditor()}
	a.drawTopBar(0, w, &frame{bpm: 120, sig: TimeSig{4, 4}})
	sc.Show()
	if !strings.Contains(simScreenText(sc), "About") {
		t.Errorf("top bar is missing the About button")
	}
}

func TestDrawAbout(t *testing.T) {
	initStyles()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	const w, h = 80, 30
	sc.SetSize(w, h)
	a := &App{screen: sc, ed: newEditor()}
	a.drawAbout(h, w)
	sc.Show()

	screen := simScreenText(sc)
	for _, want := range []string{
		"About",
		"sizzletracker",
		"Azer Abdullaev",
		"Reactorcoremeltdown",
		"github.com/reactorcoremeltdown/sizzletracker",
		"https://rcmd.space/s",
		appVersion,
		"GPL",
		"Click a link to open it",
	} {
		if !strings.Contains(screen, want) {
			t.Errorf("About popup missing %q", want)
		}
	}

	// Both URLs are registered as clickable link regions pointing at the right
	// entries in aboutURLs.
	links := map[string]bool{}
	for _, r := range a.ed.regions {
		if r.action == ActAboutLink && r.data1 >= 0 && r.data1 < len(aboutURLs) {
			links[aboutURLs[r.data1]] = true
		}
	}
	for _, u := range []string{aboutGitHub, aboutSupport} {
		if !links[u] {
			t.Errorf("no clickable region for %q", u)
		}
	}
}

// rowAllBg reports whether every cell in row y (0..w-1) has background bg.
func rowAllBg(cells []tcell.SimCell, w, y int, bg tcell.Color) bool {
	for x := 0; x < w; x++ {
		if _, got, _ := cells[y*w+x].Style.Decompose(); got != bg {
			return false
		}
	}
	return true
}

func TestRollPlayMarker(t *testing.T) {
	initStyles()
	sc := tcell.NewSimulationScreen("UTF-8")
	if err := sc.Init(); err != nil {
		t.Fatalf("sim init: %v", err)
	}
	const w, h = 60, 20
	sc.SetSize(w, h)
	a := &App{screen: sc, ed: newEditor()}

	newFrame := func(playing bool, playBeat int) *frame {
		return &frame{
			bpbar:      4,
			numBlocks:  1,
			blockNames: []string{"A"},
			blockBeats: []int{4},
			roll:       [][]bool{make([]bool, 64)},
			playing:    playing,
			playBeat:   playBeat,
		}
	}

	const top, height = 1, 12
	gut := rollGutterWidth([]string{"A"})
	markerY := top + 1
	rune2D := func() ([]tcell.SimCell, int) {
		cells, gw, _ := sc.GetContents()
		return cells, gw
	}

	// While playing: ▼ sits on the strip above the ruler at the playhead beat.
	sc.Clear()
	a.drawPianoRoll(top, height, w, newFrame(true, 5))
	sc.Show()
	cells, gw := rune2D()
	x := gut + (5 - a.ed.rollBeatScroll)
	if r := cells[markerY*gw+x].Runes; len(r) == 0 || r[0] != '▼' {
		t.Errorf("playing: expected ▼ at (%d,%d), got %q", x, markerY, string(r))
	}

	// The bar ruler is pushed one row down (now below the marker strip).
	label := ""
	for cx := 0; cx < 3; cx++ {
		if r := cells[(top+2)*gw+cx].Runes; len(r) > 0 {
			label += string(r[0])
		}
	}
	if label != "bar" {
		t.Errorf("ruler label not on row %d, got %q", top+2, label)
	}

	// While stopped: no triangle on the strip.
	sc.Clear()
	a.drawPianoRoll(top, height, w, newFrame(false, 5))
	sc.Show()
	cells, gw = rune2D()
	for cx := 0; cx < w; cx++ {
		if r := cells[markerY*gw+cx].Runes; len(r) > 0 && r[0] == '▼' {
			t.Errorf("stopped: unexpected ▼ at (%d,%d)", cx, markerY)
		}
	}
}

func TestRenameBlockFlow(t *testing.T) {
	a := &App{song: newSong(), ed: newEditor()}

	a.startRenameBlock(0)
	if a.ed.focus != FocusDialog || a.ed.dlgAction != DlgRename {
		t.Fatalf("dialog not opened: focus=%v action=%v", a.ed.focus, a.ed.dlgAction)
	}
	a.ed.dlgBuf = "Intro Groove"
	a.executeDialog()
	if got := a.song.Blocks[0].Name; got != "Intro Groove" {
		t.Errorf("after rename, block 0 name = %q, want %q", got, "Intro Groove")
	}

	// An empty name reverts to the default letter.
	a.startRenameBlock(0)
	a.ed.dlgBuf = "   "
	a.executeDialog()
	if got := a.song.Blocks[0].Name; got != blockName(0) {
		t.Errorf("after empty rename, block 0 name = %q, want %q", got, blockName(0))
	}
}

func TestRollGutterWidth(t *testing.T) {
	for _, c := range []struct {
		names []string
		want  int
	}{
		{[]string{"A", "B"}, 6},                         // short names -> compact minimum
		{[]string{"A", "Chorus"}, 7},                    // longest 6 -> 7
		{[]string{"sixteen-char-nam"}, 17},              // 16 chars -> max gutter
		{[]string{"way too long to ever fit here"}, 17}, // clamped to max
		{nil, 6},
	} {
		if got := rollGutterWidth(c.names); got != c.want {
			t.Errorf("rollGutterWidth(%v) = %d, want %d", c.names, got, c.want)
		}
	}
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

	// Layout: SETTINGS at y=1; Project banner y=3; MIDI banner y=6 (Rec, Latch,
	// Mode + two description lines); Hotkeys banner y=13; panel rows from y=14.
	if !rowAllBg(cells, w, 3, projBg) {
		t.Errorf("row 3 is not a full-width Project banner")
	}
	if !rowAllBg(cells, w, 6, midiBg) {
		t.Errorf("row 6 is not a full-width MIDI banner")
	}
	if !rowAllBg(cells, w, 13, keysBg) {
		t.Errorf("row 13 is not a full-width Hotkeys banner")
	}
	if !rowAllBg(cells, w, 14, panelBg) {
		t.Errorf("row 14 is not part of the hotkey panel background")
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
