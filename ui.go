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

// Layout constants.
const (
	gutterW    = 5 // "000 |"
	trackWidth = 10
	// Fixed height of the lower (arrangement) segment: toolbar + label +
	// several block rows. The tracker takes all remaining vertical space.
	arrangeDesiredH = 8
	minTrackerH     = 5
)

// Simple ASCII transport glyphs (single-cell, width == byte length).
const (
	glyphPlay  = "|>"
	glyphStop  = "[]"
	glyphRec   = "()"
	glyphLoop  = "<>"
	glyphLoop1 = "@@"
	glyphPanic = "!!"
)

// --- per-frame snapshot --------------------------------------------------
//
// The renderer copies everything it needs out of the shared Song under a
// single short lock, then draws from the copy. This keeps song.mu contention
// with the playback goroutine to a brief memcpy, so MIDI timing and the UI
// stay smooth regardless of how much the user is typing.

type trackView struct {
	name    string
	channel int
	steps   []Step
}

type blockView struct {
	name   string
	length int
	tracks []trackView
}

type frame struct {
	bpm float64
	sig TimeSig
	tpb int

	numBlocks  int
	blockNames []string
	blockLens  []int
	arrange    []int

	edit blockView // the block under the tracker cursor

	// transport
	playBlk, playTick, arrPos int
	playing                   bool
	loop                      LoopMode

	// timing (one pass, no repeat)
	songTicks    int
	elapsedTicks int
	spt          float64
}

func (a *App) snapshot() *frame {
	s := a.song
	s.mu.Lock()

	if a.ed.editBlock >= len(s.Blocks) {
		a.ed.editBlock = len(s.Blocks) - 1
	}
	if a.ed.editBlock < 0 {
		a.ed.editBlock = 0
	}

	fr := &frame{
		bpm:       s.BPM,
		sig:       s.Sig,
		tpb:       s.TicksPerBeat,
		numBlocks: len(s.Blocks),
		arrange:   append([]int(nil), s.Arrangement...),
		songTicks: s.totalTicks(),
		spt:       s.secondsPerTick(),
	}
	for _, b := range s.Blocks {
		fr.blockNames = append(fr.blockNames, b.Name)
		fr.blockLens = append(fr.blockLens, b.Length)
	}
	blk := s.Blocks[a.ed.editBlock]
	ev := blockView{name: blk.Name, length: blk.Length}
	for _, t := range blk.Tracks {
		ev.tracks = append(ev.tracks, trackView{
			name:    t.name(),
			channel: t.Channel,
			steps:   append([]Step(nil), t.Steps...),
		})
	}
	fr.edit = ev
	s.mu.Unlock()

	fr.playBlk, fr.playTick, fr.arrPos, fr.playing, fr.loop = a.player.state()

	// Elapsed ticks for the playback-time display.
	if fr.playing {
		if fr.loop == LoopSong {
			for i := 0; i < fr.arrPos && i < len(fr.arrange); i++ {
				bi := fr.arrange[i]
				if bi >= 0 && bi < len(fr.blockLens) {
					fr.elapsedTicks += fr.blockLens[bi]
				}
			}
			fr.elapsedTicks += fr.playTick
		} else {
			fr.elapsedTicks = fr.playTick
		}
	}
	return fr
}

// name returns a track's display name (helper so snapshot can read it).
func (t *Track) name() string { return t.Name }

// --- draw ----------------------------------------------------------------

func (a *App) draw() {
	a.win.Erase()
	a.ed.regions = a.ed.regions[:0]

	h, w := a.win.MaxYX()
	if h < 8 || w < 40 {
		a.put(0, 0, "Terminal too small", pNormal, gc.A_BOLD)
		a.win.Refresh()
		return
	}

	fr := a.snapshot()

	topH, statusH := 1, 1
	lowerH := lowerHeight(h)
	trackerY := topH
	trackerH := h - topH - statusH - lowerH
	arrangeY := topH + trackerH

	a.drawTopBar(0, w, fr)
	a.drawTracker(trackerY, trackerH, w, fr)
	a.drawArrange(arrangeY, lowerH, w, fr)
	a.drawStatus(h-1, w)

	if a.ed.showHelp {
		a.drawHelp(h, w)
	}
	a.win.Refresh()
}

// lowerHeight returns the fixed height of the arrangement segment, shrinking
// only when the terminal is too short to give the tracker its minimum.
func lowerHeight(h int) int {
	want := arrangeDesiredH
	maxLower := h - 1 - 1 - minTrackerH
	if want > maxLower {
		want = maxLower
	}
	if want < 3 {
		want = 3
	}
	return want
}

// --- Top bar -------------------------------------------------------------

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

func (a *App) drawTopBar(y, w int, fr *frame) {
	a.put(y, 0, strings.Repeat(" ", w), pHeader, 0)

	x := 1
	a.put(y, x, "SIZZLE", pHeader, gc.A_BOLD)
	x += 7

	x = a.button(y, x, glyphPlay, fr.playing, ActPlay)
	x = a.button(y, x, glyphStop, false, ActStop)
	x = a.button(y, x, glyphRec, a.ed.armed, ActRecord)

	loopGlyph := glyphLoop
	if fr.loop == LoopBlock {
		loopGlyph = glyphLoop1
	}
	x = a.button(y, x, loopGlyph, fr.loop == LoopBlock, ActLoopMode)
	x = a.button(y, x, glyphPanic, false, ActPanic)

	bpmTxt := fmt.Sprintf("%.1f", fr.bpm)
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

	sigTxt := " Sig:" + fr.sig.String() + " "
	a.put(y, x, sigTxt, pBtn, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(sigTxt), h: 1, action: ActTimeSig})
	x += len(sigTxt) + 1

	out := " Out:" + trunc(a.midi.OutName(), 16) + " "
	a.put(y, x, out, pBtn, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(out), h: 1, action: ActMidiOut})
	x += len(out) + 1

	inName := a.midi.InName()
	inPair := pBtn
	if inName != "<off>" {
		inPair = pBtnOn
	}
	in := " In:" + trunc(inName, 14) + " "
	a.put(y, x, in, inPair, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(in), h: 1, action: ActMidiIn})
}

// --- Tracker (upper) -----------------------------------------------------

func (a *App) drawTracker(top, height, w int, fr *frame) {
	if height < 3 {
		return
	}
	be := &fr.edit
	a.ed.curTrack = clampInt(a.ed.curTrack, 0, len(be.tracks)-1)
	a.ed.curTick = clampInt(a.ed.curTick, 0, be.length-1)

	a.drawTrackerControls(top, w, fr)

	hdrY := top + 1
	stepsTop := top + 2
	stepsH := height - 2

	// Horizontal track scroll.
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

	// Track column headers.
	for ti := a.ed.trackScroll; ti < len(be.tracks) && ti < a.ed.trackScroll+visTracks; ti++ {
		t := be.tracks[ti]
		cx := gutterW + (ti-a.ed.trackScroll)*trackWidth
		label := fmt.Sprintf("%-6.6s c%02d", t.name, t.channel+1)
		pair := pHeader
		if ti == a.ed.curTrack && a.ed.focus == FocusTracker {
			pair = pBtnOn
		}
		a.put(hdrY, cx, fmt.Sprintf("%-*.*s", trackWidth, trackWidth, label), pair, 0)
	}

	// Vertical scroll offset.
	a.computeTickScroll(be.length, stepsH, fr)
	first := a.ed.tickScroll

	for row := 0; row < stepsH; row++ {
		tick := first + row
		y := stepsTop + row
		if tick >= be.length {
			break
		}

		isBar := tick%a.songTicksPerBar(fr) == 0
		isBeat := tick%fr.tpb == 0
		rowPair := pNormal
		rowAttr := gc.Char(0)
		switch {
		case isBar:
			rowPair = pBar
			rowAttr = gc.A_BOLD
		case isBeat:
			rowPair = pBeat
		default:
			if tick%2 == 0 {
				rowAttr = gc.A_DIM
			}
		}

		isPlay := fr.playing && fr.playBlk == a.ed.editBlock && tick == fr.playTick

		gut := fmt.Sprintf("%03d", tick)
		gp := rowPair
		if isPlay {
			gp = pPlayhead
		}
		a.put(y, 0, gut, gp, rowAttr|gc.A_BOLD)
		a.put(y, 3, " |", pDim, gc.A_DIM)

		for ti := a.ed.trackScroll; ti < len(be.tracks) && ti < a.ed.trackScroll+visTracks; ti++ {
			t := be.tracks[ti]
			st := t.steps[tick]
			cx := gutterW + (ti-a.ed.trackScroll)*trackWidth

			cells := []string{noteName(st.Note), velName(st.Vel), chanName(st.Chan)}
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

func (a *App) songTicksPerBar(fr *frame) int {
	n := fr.tpb * fr.sig.Num
	if n < 1 {
		return 1
	}
	return n
}

// computeTickScroll updates the vertical scroll offset so the active row stays
// visible. When the block fits it shows everything from the top; otherwise it
// follows the playhead (when playing) or edge-scrolls with the cursor.
func (a *App) computeTickScroll(length, stepsH int, fr *frame) {
	if stepsH < 1 {
		stepsH = 1
	}
	maxScroll := length - stepsH
	if maxScroll < 0 {
		maxScroll = 0
	}
	if fr.playing && a.ed.follow && fr.playBlk == a.ed.editBlock {
		a.ed.tickScroll = fr.playTick - stepsH/2
	} else {
		margin := 2
		if margin > stepsH/2 {
			margin = stepsH / 2
		}
		if a.ed.curTick < a.ed.tickScroll+margin {
			a.ed.tickScroll = a.ed.curTick - margin
		}
		if a.ed.curTick > a.ed.tickScroll+stepsH-1-margin {
			a.ed.tickScroll = a.ed.curTick - stepsH + 1 + margin
		}
	}
	a.ed.tickScroll = clampInt(a.ed.tickScroll, 0, maxScroll)
}

// drawTrackerControls renders the interactive controls row: block navigation,
// block length (- / field / +), octave/step, and add/remove track.
func (a *App) drawTrackerControls(y, w int, fr *frame) {
	x := 0
	title := fmt.Sprintf("BLK %s [%d/%d]", fr.edit.name, a.ed.editBlock+1, fr.numBlocks)
	a.put(y, x, title, pAccent, gc.A_BOLD)
	x += len(title) + 1

	x = a.button(y, x, "<", false, ActBlockPrev)
	x = a.button(y, x, ">", false, ActBlockNext)

	a.put(y, x, " len ", pDim, 0)
	x += 5
	x = a.button(y, x, "-", false, ActLenHalf)

	lenTxt := fmt.Sprintf("%4d", fr.edit.length)
	lenPair := pBtn
	if a.ed.focus == FocusLen {
		lenTxt = fmt.Sprintf("%-4.4s", a.ed.lenBuf+"_")
		lenPair = pBtnOn
	}
	field := " " + lenTxt + " "
	a.put(y, x, field, lenPair, gc.A_BOLD)
	a.ed.addRegion(Region{x: x, y: y, w: len(field), h: 1, action: ActLenField})
	x += len(field) + 1
	x = a.button(y, x, "+", false, ActLenDouble)

	info := fmt.Sprintf(" oct%d step%d", a.ed.octave, a.ed.step)
	a.put(y, x, info, pDim, 0)
	x += len(info) + 2

	// Add / remove track on the right if there is room.
	right := w - 16
	if x < right {
		rx := right
		rx = a.button(y, rx, "+trk", false, ActAddTrack)
		if len(fr.edit.tracks) > 1 {
			a.button(y, rx, "-trk", false, ActDelTrack)
		}
	}
}

// --- Arrangement (lower) -------------------------------------------------

func (a *App) drawArrange(top, height, w int, fr *frame) {
	if height < 2 {
		return
	}
	a.drawArrangeToolbar(top, w, fr)

	labelY := top + 1
	focusTag := ""
	if a.ed.focus == FocusArrange {
		focusTag = "*"
	}
	a.put(labelY, 0, "ARR"+focusTag, pAccent, gc.A_BOLD)

	// Palette of blocks.
	px := 5
	for i, name := range fr.blockNames {
		lbl := fmt.Sprintf("[%s]", name)
		pair := pBtn
		if i == a.ed.editBlock {
			pair = pBtnOn
		}
		if px+len(lbl) > w-22 {
			a.put(labelY, px, ">", pDim, 0)
			break
		}
		a.put(labelY, px, lbl, pair, gc.A_BOLD)
		a.ed.addRegion(Region{x: px, y: labelY, w: len(lbl), h: 1, action: ActBlockPick, data1: i})
		px += len(lbl) + 1
	}

	// Position indicator, right-aligned on the label row.
	pos := fmt.Sprintf("arr %d/%d  row %d", fr.arrPos+1, len(fr.arrange), fr.playTick)
	if px < w-len(pos)-1 {
		a.put(labelY, w-len(pos)-1, pos, pDim, 0)
	}

	// Slot grid.
	a.ed.arrCursor = clampInt(a.ed.arrCursor, 0, max(0, len(fr.arrange)-1))
	selLo, selHi := a.ed.selRange()
	slotW := 6
	perRow := (w - 2) / slotW
	if perRow < 1 {
		perRow = 1
	}
	gridTop := top + 2
	rows := height - 2

	playArr := -1
	if fr.playing && fr.loop == LoopSong {
		playArr = fr.arrPos
	}

	for i, bi := range fr.arrange {
		r := i / perRow
		c := i % perRow
		if r >= rows {
			break
		}
		x := 1 + c*slotW
		yy := gridTop + r
		name := "?"
		if bi >= 0 && bi < len(fr.blockNames) {
			name = fr.blockNames[bi]
		}
		txt := fmt.Sprintf("%-*.*s", slotW-1, slotW-1, fmt.Sprintf("%02d:%s", i, name))

		pair := pNormal
		attr := gc.Char(0)
		if a.ed.selActive && i >= selLo && i <= selHi {
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
		a.put(yy, x, txt, pair, attr)
		a.ed.addRegion(Region{x: x, y: yy, w: slotW - 1, h: 1, action: ActArrSlot, data1: i})
	}
}

// drawArrangeToolbar renders the block-ops toolbar plus the song-time display.
func (a *App) drawArrangeToolbar(y, w int, fr *frame) {
	a.put(y, 0, strings.Repeat(" ", w), pHeader, 0)
	x := 1
	x = a.button(y, x, "Add", false, ActArrAdd)
	x = a.button(y, x, "Remove", false, ActArrRemove)
	x = a.button(y, x, "Cut", false, ActArrCut)
	x = a.button(y, x, "Copy", false, ActArrCopy)
	x = a.button(y, x, "Paste", false, ActArrPaste)

	cur := formatClock(float64(fr.elapsedTicks) * fr.spt)
	total := formatClock(float64(fr.songTicks) * fr.spt)
	clk := fmt.Sprintf(" time %s / %s ", cur, total)
	if x < w-len(clk)-1 {
		a.put(y, w-len(clk)-1, clk, pHeader, gc.A_BOLD)
	}
}

func (a *App) drawStatus(y, w int) {
	a.put(y, 0, strings.Repeat(" ", w), pHeader, 0)
	a.put(y, 1, trunc(a.ed.status, w-2), pHeader, 0)
}

// --- Help overlay --------------------------------------------------------

var helpLines = []string{
	"# Transport (top bar)",
	"  |> play/stop   [] stop   () rec   <>/@@ loop   !! panic",
	"  BPM / Sig / Out / In are clickable fields.",
	"",
	"# Global",
	"  Space play/stop   Tab switch pane   F1 help   F2/F3 focus",
	"  F5 rec   F6 loop   F7 follow   F8 panic   F9 BPM   F10 quit",
	"",
	"# Tracker (upper half)",
	"  Arrows move (L/R cross columns/tracks)",
	"  Shift+L/R change track   PgUp/PgDn beat   Home/End ends",
	"  [ / ] or < / > switch block",
	"  len: - halves, + doubles, click number to type a length",
	"  z..m / q..i notes   ` note-off   . or Del clear   Bksp back",
	"  - / = octave    +trk / -trk add/delete track",
	"  Tall blocks scroll to keep the cursor/playhead in view.",
	"",
	"# Arrangement (lower half) - fixed-height segment",
	"  Toolbar: Add Remove Cut Copy Paste (mouse-clickable too)",
	"  Left/Right move   Shift+L/R select   Up/Down cycle block",
	"  Enter edit   i insert  a append  x delete  c copy  v paste",
	"  , / . move selection   n new  d dup  D remove block",
	"  Song time / position shown in this pane.",
	"",
	"# Live punch-in",
	"  Pick 'In:', arm record (F5). While playing, controller",
	"  note-on/off get recorded at the playhead on the cursor track.",
	"",
	"  Press any key to close this help.",
}

func (a *App) drawHelp(h, w int) {
	bw := 70
	if bw > w-2 {
		bw = w - 2
	}
	bh := len(helpLines) + 2
	if bh > h-2 {
		bh = h - 2
	}
	x0 := (w - bw) / 2
	y0 := (h - bh) / 2

	for r := 0; r < bh; r++ {
		a.put(y0+r, x0, strings.Repeat(" ", bw), pHeader, 0)
	}
	title := " sizzletracker - keys (F1) "
	a.put(y0, x0, strings.Repeat("-", bw), pHeader, gc.A_BOLD)
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
		a.put(ry, x0+1, fmt.Sprintf("%-*.*s", bw-2, bw-2, trunc(line, bw-2)), pHeader, attr)
	}
	a.put(y0+bh-1, x0, strings.Repeat("-", bw), pHeader, gc.A_BOLD)
}

// --- helpers -------------------------------------------------------------

func formatClock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	total := int(sec + 0.5)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

func trunc(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
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
