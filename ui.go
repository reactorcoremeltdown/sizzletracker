package main

import (
	"fmt"
	"strings"

	gc "github.com/rthornton128/goncurses"
)

// Color pair identifiers.
const (
	pNormal int16 = iota + 1
	pBeat
	pBar
	pPlayhead
	pCursor
	pHeader
	pBtn
	pBtnOn
	pSel
	pDim
	pArm
	pAccent
)

func initColors() {
	gc.StartColor()
	gc.UseDefaultColors()
	gc.InitPair(pNormal, gc.C_WHITE, -1)
	gc.InitPair(pBeat, gc.C_CYAN, -1)
	gc.InitPair(pBar, gc.C_YELLOW, -1)
	gc.InitPair(pPlayhead, gc.C_BLACK, gc.C_GREEN)
	gc.InitPair(pCursor, gc.C_BLACK, gc.C_WHITE)
	gc.InitPair(pHeader, gc.C_BLACK, gc.C_CYAN)
	gc.InitPair(pBtn, gc.C_WHITE, gc.C_BLUE)
	gc.InitPair(pBtnOn, gc.C_BLACK, gc.C_GREEN)
	gc.InitPair(pSel, gc.C_BLACK, gc.C_YELLOW)
	gc.InitPair(pDim, gc.C_WHITE, -1)
	gc.InitPair(pArm, gc.C_WHITE, gc.C_RED)
	gc.InitPair(pAccent, gc.C_MAGENTA, -1)
}

// put writes s at (y,x) with a color pair and optional attributes.
func (a *App) put(y, x int, s string, pair int16, attr gc.Char) {
	if y < 0 || x < 0 {
		return
	}
	full := gc.ColorPair(pair) | attr
	a.win.AttrOn(full)
	a.win.MovePrint(y, x, s)
	a.win.AttrOff(full)
}

// Layout constants for the tracker grid.
const (
	gutterW    = 5  // "000 |"
	trackWidth = 10 // "C-4 7F 02 "
)

// Simple ASCII transport glyphs (single-cell, width == byte length).
const (
	glyphPlay  = "|>"
	glyphStop  = "[]"
	glyphRec   = "()"
	glyphLoop  = "<>" // song-loop (range)
	glyphLoop1 = "@@" // block-loop (repeat one)
	glyphPanic = "!!"
)

func (a *App) draw() {
	a.win.Erase()
	a.ed.regions = a.ed.regions[:0]

	h, w := a.win.MaxYX()
	if h < 8 || w < 40 {
		a.put(0, 0, "Terminal too small", pNormal, gc.A_BOLD)
		a.win.Refresh()
		return
	}

	topH := 1
	statusH := 1
	bodyH := h - topH - statusH
	trackerH := bodyH * 6 / 10
	if trackerH < 4 {
		trackerH = 4
	}
	arrangeY := topH + trackerH
	arrangeH := h - statusH - arrangeY

	a.drawTopBar(0, w)
	a.drawTracker(topH, trackerH, w)
	a.drawArrange(arrangeY, arrangeH, w)
	a.drawStatus(h-1, w)

	if a.ed.showHelp {
		a.drawHelp(h, w)
	}

	a.win.Refresh()
}

// helpLines is the content of the F1 help overlay. Empty strings are spacers;
// lines beginning with "# " are section headers.
var helpLines = []string{
	"# Transport (top bar)",
	"  |>  play/stop      []  stop        ()  record-arm",
	"  <>  loop song      @@  loop block  !!  panic (all notes off)",
	"",
	"# Global",
	"  Space  play/stop        Tab    switch tracker/arrange",
	"  F1     this help         F2/F3  focus tracker/arrange",
	"  F5     record-arm        F6     loop mode (song/block)",
	"  F7     follow playhead    F8     panic       F9  edit BPM",
	"  F10    quit",
	"",
	"# Tracker (upper half)",
	"  Arrows      move cursor (left/right cross columns/tracks)",
	"  Shift+L/R   previous/next track",
	"  PgUp/PgDn   jump one beat       Home/End  top/bottom",
	"  [ / ]       previous/next block to edit",
	"  z s x ...m  notes (lower octave)",
	"  q 2 w ...i  notes (upper octave)",
	"  `  (backtick)   NOTE-OFF  (note sustains until this or a new note)",
	"  . / Del     clear cell        Backspace  clear + step back",
	"  - / =       octave down/up",
	"  vel column: hex 0-9 a-f     chan column: digits 1-16",
	"  +trk / -trk header buttons add / delete the cursor track",
	"",
	"# Arrangement (lower half)",
	"  Left/Right  move slot      Shift+L/R  extend selection",
	"  Up/Down     cycle slot's block      Enter  edit that block",
	"  i/Ins insert  a append  x/Del delete  c copy  v paste",
	"  , / .  move selection      n new block  d duplicate  D remove",
	"",
	"# Live punch-in (MIDI input)",
	"  Click 'In:' to pick a controller, arm record (F5/()).",
	"  While playing, note-on/off from the controller are recorded",
	"  as notes and note-offs at the playhead on the cursor track.",
	"",
	"  Mouse: click buttons & cells; right-click cell/slot to clear;",
	"  shift-click slots to select; double-click a slot to edit it.",
	"",
	"  Press any key to close this help.",
}

func (a *App) drawHelp(h, w int) {
	bw := 66
	if bw > w-2 {
		bw = w - 2
	}
	bh := len(helpLines) + 2
	if bh > h-2 {
		bh = h - 2
	}
	x0 := (w - bw) / 2
	y0 := (h - bh) / 2

	// Box body + border.
	for r := 0; r < bh; r++ {
		a.put(y0+r, x0, strings.Repeat(" ", bw), pHeader, 0)
	}
	title := " sizzletracker — keys (F1) "
	a.put(y0, x0, strings.Repeat("─", bw), pHeader, gc.A_BOLD)
	a.put(y0, x0+(bw-len(title))/2, title, pHeader, gc.A_BOLD)

	for i, line := range helpLines {
		ry := y0 + 1 + i
		if ry >= y0+bh-1 {
			break
		}
		attr := gc.Char(0)
		if strings.HasPrefix(line, "# ") {
			line = line[2:]
			attr = gc.A_BOLD
		}
		// Uniform black-on-cyan panel so every row matches the border.
		a.put(ry, x0+1, fmt.Sprintf("%-*.*s", bw-2, bw-2, trunc(line, bw-2)), pHeader, attr)
	}
	a.put(y0+bh-1, x0, strings.Repeat("─", bw), pHeader, gc.A_BOLD)
}

// --- Top bar ---------------------------------------------------------------

func (a *App) button(y, x int, label string, on bool, act RegionAction) int {
	txt := " " + label + " "
	pair := pBtn
	if on {
		pair = pBtnOn
	}
	a.put(y, x, txt, pair, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(txt), h: 1, action: act})
	return x + len(txt) + 1
}

func (a *App) drawTopBar(y, w int) {
	// Background bar.
	a.put(y, 0, strings.Repeat(" ", w), pHeader, 0)

	playing := a.player.isPlaying()
	x := 1
	a.put(y, x, "SIZZLE", pHeader, gc.A_BOLD)
	x += 7

	x = a.button(y, x, glyphPlay, playing, ActPlay)
	x = a.button(y, x, glyphStop, false, ActStop)
	x = a.button(y, x, glyphRec, a.ed.armed, ActRecord)

	loopGlyph := glyphLoop
	if a.player.loopMode() == LoopBlock {
		loopGlyph = glyphLoop1
	}
	x = a.button(y, x, loopGlyph, a.player.loopMode() == LoopBlock, ActLoopMode)
	x = a.button(y, x, glyphPanic, false, ActPanic)

	// BPM field.
	a.song.mu.Lock()
	bpm := a.song.BPM
	sig := a.song.Sig
	a.song.mu.Unlock()

	bpmTxt := fmt.Sprintf("%.1f", bpm)
	if a.ed.focus == FocusBPM {
		bpmTxt = a.ed.bpmBuf + "_"
	}
	lbl := " BPM:" + bpmTxt + " "
	bpmPair := pBtn
	if a.ed.focus == FocusBPM {
		bpmPair = pBtnOn
	}
	a.put(y, x, lbl, bpmPair, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(lbl), h: 1, action: ActBPM})
	x += len(lbl) + 1

	sigTxt := " Sig:" + sig.String() + " "
	a.put(y, x, sigTxt, pBtn, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(sigTxt), h: 1, action: ActTimeSig})
	x += len(sigTxt) + 1

	out := " Out:" + trunc(a.midi.OutName(), 18) + " "
	a.put(y, x, out, pBtn, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(out), h: 1, action: ActMidiOut})
	x += len(out) + 1

	inName := a.midi.InName()
	inPair := pBtn
	if inName != "<off>" {
		inPair = pBtnOn
	}
	in := " In:" + trunc(inName, 16) + " "
	a.put(y, x, in, inPair, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(in), h: 1, action: ActMidiIn})
}

// --- Tracker (upper half) --------------------------------------------------

func (a *App) drawTracker(top, height, w int) {
	a.song.mu.Lock()
	defer a.song.mu.Unlock()

	if a.ed.editBlock >= len(a.song.Blocks) {
		a.ed.editBlock = len(a.song.Blocks) - 1
	}
	blk := a.song.Blocks[a.ed.editBlock]
	a.ed.curTrack = clampInt(a.ed.curTrack, 0, len(blk.Tracks)-1)
	a.ed.curTick = clampInt(a.ed.curTick, 0, blk.Length-1)

	tpb := a.song.TicksPerBeat
	tpbar := a.song.ticksPerBar()

	playBlk, playTick, playing := a.player.playhead()

	// Horizontal scroll so the cursor track is visible.
	visTracks := (w - gutterW) / trackWidth
	if visTracks < 1 {
		visTracks = 1
	}
	if a.ed.curTrack < a.ed.trackScroll {
		a.ed.trackScroll = a.ed.curTrack
	}
	if a.ed.curTrack >= a.ed.trackScroll+visTracks {
		a.ed.trackScroll = a.ed.curTrack - visTracks + 1
	}

	// Header row: block name + per-track names/channels.
	hdr := top
	title := fmt.Sprintf("BLK %s [%d/%d] len %d  oct%d step%d",
		blk.Name, a.ed.editBlock+1, len(a.song.Blocks), blk.Length, a.ed.octave, a.ed.step)
	a.put(hdr, 0, title, pAccent, gc.A_BOLD)

	stepsTop := top + 1
	stepsH := height - 1

	// Track column headers.
	for ti := a.ed.trackScroll; ti < len(blk.Tracks) && ti < a.ed.trackScroll+visTracks; ti++ {
		t := blk.Tracks[ti]
		cx := gutterW + (ti-a.ed.trackScroll)*trackWidth
		label := fmt.Sprintf("%-6.6s c%02d", t.Name, t.Channel+1)
		pair := pHeader
		if ti == a.ed.curTrack && a.ed.focus == FocusTracker {
			pair = pBtnOn
		}
		a.put(hdr, cx, fmt.Sprintf("%-*.*s", trackWidth, trackWidth, label), pair, 0)
	}
	// "+track" / "-track" affordances at far right of header.
	addX := gutterW + (min(len(blk.Tracks), a.ed.trackScroll+visTracks)-a.ed.trackScroll)*trackWidth
	if addX+6 < w {
		a.put(hdr, addX, " +trk ", pBtn, 0)
		a.ed.addRegion(Region{x: addX, y: hdr, w: 6, h: 1, action: ActAddTrack})
		delX := addX + 7
		if delX+6 < w && len(blk.Tracks) > 1 {
			a.put(hdr, delX, " -trk ", pArm, 0)
			a.ed.addRegion(Region{x: delX, y: hdr, w: 6, h: 1, action: ActDelTrack, data1: a.ed.curTrack})
		}
	}

	// Vertical scroll: center the active row (playhead when following, else cursor).
	center := a.ed.curTick
	if playing && a.ed.follow && playBlk == a.ed.editBlock {
		center = playTick
	}
	first := center - stepsH/2
	if first > blk.Length-stepsH {
		first = blk.Length - stepsH
	}
	if first < 0 {
		first = 0
	}

	for row := 0; row < stepsH; row++ {
		tick := first + row
		y := stepsTop + row
		if tick >= blk.Length {
			break
		}

		// Row classification for interlaced colouring.
		isBar := tick%tpbar == 0
		isBeat := tick%tpb == 0
		rowPair := pNormal
		rowAttr := gc.Char(0)
		switch {
		case isBar:
			rowPair = pBar
			rowAttr = gc.A_BOLD
		case isBeat:
			rowPair = pBeat
		default:
			if (tick/1)%2 == 0 {
				rowAttr = gc.A_DIM
			}
		}

		isPlay := playing && playBlk == a.ed.editBlock && tick == playTick

		// Gutter (row number).
		gut := fmt.Sprintf("%03d", tick)
		gp := rowPair
		if isPlay {
			gp = pPlayhead
		}
		a.put(y, 0, gut, gp, rowAttr|gc.A_BOLD)
		a.put(y, 3, " |", pDim, gc.A_DIM)

		for ti := a.ed.trackScroll; ti < len(blk.Tracks) && ti < a.ed.trackScroll+visTracks; ti++ {
			t := blk.Tracks[ti]
			st := t.Steps[tick]
			cx := gutterW + (ti-a.ed.trackScroll)*trackWidth

			note := noteName(st.Note)
			vel := velName(st.Vel)
			chn := chanName(st.Chan)

			cells := []string{note, vel, chn}
			offs := []int{0, 4, 7}
			widths := []int{3, 2, 2}

			for ci, cell := range cells {
				px := cx + offs[ci]
				cellPair := rowPair
				cellAttr := rowAttr
				if isPlay {
					cellPair = pPlayhead
					cellAttr = gc.A_BOLD
				}
				isCur := a.ed.focus == FocusTracker &&
					ti == a.ed.curTrack && tick == a.ed.curTick && ci == a.ed.curCol
				if isCur {
					cellPair = pCursor
					cellAttr = gc.A_BOLD
				}
				// Dim empty values.
				if !isCur && !isPlay && (cell == "---" || cell == "..") {
					cellAttr |= gc.A_DIM
				}
				a.put(y, px, cell, cellPair, cellAttr)
				a.ed.addRegion(Region{x: px, y: y, w: widths[ci], h: 1,
					action: ActTrackerCell, data1: ti, data2: tick, data3: ci})
			}
		}
	}
}

// --- Arrangement (lower half) ----------------------------------------------

func (a *App) drawArrange(top, height, w int) {
	if height < 2 {
		return
	}
	a.song.mu.Lock()
	defer a.song.mu.Unlock()

	playBlk, _, playing := a.player.playhead()
	playArr := -1
	a.player.mu.Lock()
	if playing && a.player.loop == LoopSong {
		playArr = a.player.arrPos
	}
	a.player.mu.Unlock()

	// Header.
	focusTag := ""
	if a.ed.focus == FocusArrange {
		focusTag = "  [FOCUS]"
	}
	a.put(top, 0, "ARRANGEMENT"+focusTag, pAccent, gc.A_BOLD)

	// Palette of blocks (clickable to append/pick).
	px := 14
	for i, b := range a.song.Blocks {
		lbl := fmt.Sprintf("[%s]", b.Name)
		pair := pBtn
		if i == a.ed.editBlock {
			pair = pBtnOn
		}
		a.put(top, px, lbl, pair, gc.A_BOLD)
		a.ed.addRegion(Region{x: px, y: top, w: len(lbl), h: 1, action: ActBlockPick, data1: i})
		px += len(lbl) + 1
		if px > w-6 {
			break
		}
	}

	// Arrangement slots, wrapped across the available rows.
	a.ed.arrCursor = clampInt(a.ed.arrCursor, 0, max(0, len(a.song.Arrangement)-1))
	selLo, selHi := a.ed.selRange()

	slotW := 6 // "00:A "
	perRow := (w - 2) / slotW
	if perRow < 1 {
		perRow = 1
	}
	gridTop := top + 1
	rows := height - 1

	for i, bi := range a.song.Arrangement {
		r := i / perRow
		c := i % perRow
		if r >= rows {
			break
		}
		x := 1 + c*slotW
		y := gridTop + r

		name := "?"
		if bi >= 0 && bi < len(a.song.Blocks) {
			name = a.song.Blocks[bi].Name
		}
		txt := fmt.Sprintf("%02d:%s", i, name)
		txt = fmt.Sprintf("%-*.*s", slotW-1, slotW-1, txt)

		pair := pNormal
		attr := gc.Char(0)
		if i >= selLo && i <= selHi && a.ed.selActive {
			pair = pSel
		}
		if i == a.ed.arrCursor && a.ed.focus == FocusArrange {
			pair = pCursor
			attr = gc.A_BOLD
		}
		if i == playArr {
			pair = pPlayhead
			attr = gc.A_BOLD
		}
		a.put(y, x, txt, pair, attr)
		a.ed.addRegion(Region{x: x, y: y, w: slotW - 1, h: 1, action: ActArrSlot, data1: i})
	}

	_ = playBlk
}

func (a *App) drawStatus(y, w int) {
	a.put(y, 0, strings.Repeat(" ", w), pHeader, 0)
	a.put(y, 1, trunc(a.ed.status, w-2), pHeader, 0)
}

// --- small helpers ---------------------------------------------------------

func trunc(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
