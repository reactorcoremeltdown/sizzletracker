package main

import (
	"fmt"
	"strings"

	"github.com/gdamore/tcell/v2"
	"github.com/mattn/go-runewidth"
)

// cellWidth returns the visual width of s in terminal cells. We use this for
// layout - region hit-boxes, x advances - anywhere we cannot trust `len()`
// (which is byte length, wrong for the multibyte glyphs we render).
func cellWidth(s string) int { return runewidth.StringWidth(s) }

// --- Styles --------------------------------------------------------------
//
// In tcell, attributes (bold, dim, etc.) are part of the Style value rather
// than a separate parameter. We define the base palette here; callers can
// chain modifiers (e.g. `styNormal.Dim(true)`) at the call site as needed.

var (
	styNormal   tcell.Style
	styBeat     tcell.Style
	styBar      tcell.Style
	styPlayhead tcell.Style
	styCursor   tcell.Style
	styHeader   tcell.Style
	styBtn      tcell.Style
	styBtnOn    tcell.Style
	styBtnDel   tcell.Style
	stySel      tcell.Style
	styDim      tcell.Style
	styAccent   tcell.Style
)

func initStyles() {
	def := tcell.StyleDefault
	styNormal = def
	styBeat = def.Foreground(tcell.ColorTeal)
	styBar = def.Foreground(tcell.ColorYellow).Bold(true)
	styPlayhead = def.Background(tcell.ColorGreen).Foreground(tcell.ColorBlack).Bold(true)
	styCursor = def.Background(tcell.ColorSilver).Foreground(tcell.ColorBlack).Bold(true)
	styHeader = def.Background(tcell.ColorTeal).Foreground(tcell.ColorBlack)
	styBtn = def.Background(tcell.ColorNavy).Foreground(tcell.ColorWhite).Bold(true)
	styBtnOn = def.Background(tcell.ColorGreen).Foreground(tcell.ColorBlack).Bold(true)
	styBtnDel = def.Background(tcell.ColorMaroon).Foreground(tcell.ColorWhite).Bold(true)
	stySel = def.Background(tcell.ColorYellow).Foreground(tcell.ColorBlack)
	styDim = def.Dim(true)
	styAccent = def.Foreground(tcell.ColorFuchsia).Bold(true)
}

// put writes s at (y, x) with the given style. tcell's PutStrStyled walks
// the string by grapheme clusters and handles wide / combining characters
// correctly (Screen.Put is single-cluster - be sure to use the *Str* variant).
func (a *App) put(y, x int, s string, st tcell.Style) {
	if y < 0 || x < 0 {
		return
	}
	a.screen.PutStrStyled(x, y, s, st)
}

// fill paints n cells starting at (y, x) with a single rune in the given
// style. Used for backgrounds (top bar, status, help panel).
func (a *App) fill(y, x, n int, r rune, st tcell.Style) {
	for i := 0; i < n; i++ {
		a.screen.SetContent(x+i, y, r, nil, st)
	}
}

// Layout constants.
const (
	gutterW    = 5
	trackWidth = 10
	// Fixed height of the lower (arrangement) segment: toolbar + label +
	// several block rows. The tracker takes all remaining vertical space.
	arrangeDesiredH = 8
	minTrackerH     = 5
)

// Transport glyphs. All are single-cell in monospaced terminal fonts; tcell
// renders them correctly (uniseg-aware), and cellWidth() handles their
// multibyte length so the layout still lines up.
const (
	glyphPlay  = "▶"
	glyphStop  = "■"
	glyphRec   = "●"
	glyphLoop  = "⟲" // loop-song  (anticlockwise = "round trip the arrangement")
	glyphLoop1 = "⟳" // loop-block (clockwise gapped = "tight repeat")
	glyphPanic = "⚠"
)

// --- per-frame snapshot --------------------------------------------------
//
// The renderer copies everything it needs out of the shared Song under a
// single short lock, then draws from the copy. This keeps song.mu contention
// with the playback goroutine to a brief memcpy.

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

	edit blockView

	playBlk, playTick, arrPos int
	playing                   bool
	loop                      LoopMode

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

// name returns a track's display name (helper so snapshot can read it under
// the song lock without exporting Track fields).
func (t *Track) name() string { return t.Name }

// --- draw ----------------------------------------------------------------

func (a *App) draw() {
	a.screen.Clear()
	a.ed.regions = a.ed.regions[:0]

	w, h := a.screen.Size()
	if h < 8 || w < 40 {
		a.put(0, 0, "Terminal too small", styNormal.Bold(true))
		a.screen.Show()
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
	a.screen.Show()
}

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
	return a.styledButton(y, x, label, styBtn, styBtnOn, on, act)
}

func (a *App) styledButton(y, x int, label string, off, onSty tcell.Style, on bool, act RegionAction) int {
	txt := " " + label + " "
	st := off
	if on {
		st = onSty
	}
	a.put(y, x, txt, st)
	w := cellWidth(txt)
	a.ed.addRegion(Region{x: x, y: y, w: w, h: 1, action: act})
	return x + w + 1
}

func (a *App) drawTopBar(y, w int, fr *frame) {
	a.fill(y, 0, w, ' ', styHeader)

	x := 1
	a.put(y, x, "SIZZLE", styHeader.Bold(true))
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
	bpmSty := styBtn
	if a.ed.focus == FocusBPM {
		bpmSty = styBtnOn
	}
	x = a.putRegion(y, x, lbl, bpmSty, ActBPM)

	sigTxt := " Sig:" + fr.sig.String() + " "
	x = a.putRegion(y, x, sigTxt, styBtn, ActTimeSig)

	out := " Out:" + trunc(a.midi.OutName(), 16) + " "
	x = a.putRegion(y, x, out, styBtn, ActMidiOut)

	inName := a.midi.InName()
	inSty := styBtn
	if inName != "<off>" {
		inSty = styBtnOn
	}
	in := " In:" + trunc(inName, 14) + " "
	a.putRegion(y, x, in, inSty, ActMidiIn)
}

// putRegion draws s and registers a hit-region of the correct cell width.
// Returns the next x position (x + cellWidth(s) + 1 spacer).
func (a *App) putRegion(y, x int, s string, st tcell.Style, act RegionAction) int {
	a.put(y, x, s, st)
	w := cellWidth(s)
	a.ed.addRegion(Region{x: x, y: y, w: w, h: 1, action: act})
	return x + w + 1
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

	for ti := a.ed.trackScroll; ti < len(be.tracks) && ti < a.ed.trackScroll+visTracks; ti++ {
		t := be.tracks[ti]
		cx := gutterW + (ti-a.ed.trackScroll)*trackWidth
		label := fmt.Sprintf("%-6.6s c%02d", t.name, t.channel+1)
		sty := styHeader
		if ti == a.ed.curTrack && a.ed.focus == FocusTracker {
			sty = styBtnOn
		}
		a.put(hdrY, cx, fmt.Sprintf("%-*.*s", trackWidth, trackWidth, label), sty)
	}

	a.computeTickScroll(be.length, stepsH, fr)
	first := a.ed.tickScroll

	tpbar := a.songTicksPerBar(fr)
	for row := 0; row < stepsH; row++ {
		tick := first + row
		y := stepsTop + row
		if tick >= be.length {
			break
		}

		isBar := tick%tpbar == 0
		isBeat := tick%fr.tpb == 0
		rowSty := styNormal
		switch {
		case isBar:
			rowSty = styBar
		case isBeat:
			rowSty = styBeat
		default:
			if tick%2 == 0 {
				rowSty = styDim
			}
		}

		isPlay := fr.playing && fr.playBlk == a.ed.editBlock && tick == fr.playTick

		gut := fmt.Sprintf("%03d", tick)
		gSty := rowSty.Bold(true)
		if isPlay {
			gSty = styPlayhead
		}
		a.put(y, 0, gut, gSty)
		a.put(y, 3, " |", styDim)

		for ti := a.ed.trackScroll; ti < len(be.tracks) && ti < a.ed.trackScroll+visTracks; ti++ {
			t := be.tracks[ti]
			st := t.steps[tick]
			cx := gutterW + (ti-a.ed.trackScroll)*trackWidth

			cells := []string{noteName(st.Note), velName(st.Vel), chanName(st.Chan)}
			offs := []int{0, 4, 7}
			widths := []int{3, 2, 2}

			for ci, cell := range cells {
				px := cx + offs[ci]
				cellSty := rowSty
				if isPlay {
					cellSty = styPlayhead
				}
				isCur := a.ed.focus == FocusTracker &&
					ti == a.ed.curTrack && tick == a.ed.curTick && ci == a.ed.curCol
				if isCur {
					cellSty = styCursor
				}
				if !isCur && !isPlay && (cell == "---" || cell == "..") {
					cellSty = cellSty.Dim(true)
				}
				a.put(y, px, cell, cellSty)
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

func (a *App) drawTrackerControls(y, w int, fr *frame) {
	x := 0
	title := fmt.Sprintf("BLK %s [%d/%d]", fr.edit.name, a.ed.editBlock+1, fr.numBlocks)
	a.put(y, x, title, styAccent)
	x += cellWidth(title) + 1

	x = a.button(y, x, "<", false, ActBlockPrev)
	x = a.button(y, x, ">", false, ActBlockNext)

	a.put(y, x, " len ", styDim)
	x += 5
	x = a.button(y, x, "-", false, ActLenHalf)

	lenTxt := fmt.Sprintf("%4d", fr.edit.length)
	lenSty := styBtn
	if a.ed.focus == FocusLen {
		lenTxt = fmt.Sprintf("%-4.4s", a.ed.lenBuf+"_")
		lenSty = styBtnOn
	}
	field := " " + lenTxt + " "
	x = a.putRegion(y, x, field, lenSty, ActLenField)
	x = a.button(y, x, "+", false, ActLenDouble)

	info := fmt.Sprintf(" oct%d step%d", a.ed.octave, a.ed.step)
	a.put(y, x, info, styDim)
	x += cellWidth(info) + 2

	right := w - 16
	if x < right {
		rx := right
		rx = a.button(y, rx, "+trk", false, ActAddTrack)
		if len(fr.edit.tracks) > 1 {
			a.styledButton(y, rx, "-trk", styBtnDel, styBtnDel, false, ActDelTrack)
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
	a.put(labelY, 0, "ARR"+focusTag, styAccent)

	px := 5
	for i, name := range fr.blockNames {
		lbl := fmt.Sprintf("[%s]", name)
		sty := styBtn
		if i == a.ed.editBlock {
			sty = styBtnOn
		}
		lw := cellWidth(lbl)
		if px+lw > w-22 {
			a.put(labelY, px, ">", styDim)
			break
		}
		a.put(labelY, px, lbl, sty)
		a.ed.addRegion(Region{x: px, y: labelY, w: lw, h: 1, action: ActBlockPick, data1: i})
		px += lw + 1
	}

	pos := fmt.Sprintf("arr %d/%d  row %d", fr.arrPos+1, len(fr.arrange), fr.playTick)
	pw := cellWidth(pos)
	if px < w-pw-1 {
		a.put(labelY, w-pw-1, pos, styDim)
	}

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

		sty := styNormal
		if a.ed.selActive && i >= selLo && i <= selHi {
			sty = stySel
		}
		if i == a.ed.arrCursor && a.ed.focus == FocusArrange {
			sty = styCursor
		}
		if i == playArr {
			sty = styPlayhead
		}
		a.put(yy, x, txt, sty)
		a.ed.addRegion(Region{x: x, y: yy, w: slotW - 1, h: 1, action: ActArrSlot, data1: i})
	}
}

func (a *App) drawArrangeToolbar(y, w int, fr *frame) {
	a.fill(y, 0, w, ' ', styHeader)
	x := 1
	x = a.button(y, x, "Add", false, ActArrAdd)
	x = a.button(y, x, "Remove", false, ActArrRemove)
	x = a.button(y, x, "Cut", false, ActArrCut)
	x = a.button(y, x, "Copy", false, ActArrCopy)
	x = a.button(y, x, "Paste", false, ActArrPaste)

	cur := formatClock(float64(fr.elapsedTicks) * fr.spt)
	total := formatClock(float64(fr.songTicks) * fr.spt)
	clk := fmt.Sprintf(" time %s / %s ", cur, total)
	cw := cellWidth(clk)
	if x < w-cw-1 {
		a.put(y, w-cw-1, clk, styHeader.Bold(true))
	}
}

func (a *App) drawStatus(y, w int) {
	a.fill(y, 0, w, ' ', styHeader)
	a.put(y, 1, trunc(a.ed.status, w-2), styHeader)
}

// --- Help overlay --------------------------------------------------------
//
// The overlay is intentionally ASCII-only so it renders identically in every
// terminal and font. tcell already prevents mojibake; this additionally avoids
// ambiguous-width glyphs (bullets, middots, box-drawing) that some terminals
// draw as double-width, which would misalign the panel.

var helpLines = []string{
	"# Transport (top bar)",
	"  ▶ play/stop   ■ stop   ● rec   ⟲/⟳ loop   ⚠ panic",
	"  BPM   Sig   Out   In are clickable fields.",
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
	"  - / = octave   +trk / -trk add/delete track",
	"  Tall blocks scroll to keep the cursor / playhead in view.",
	"",
	"# Arrangement (lower half) - fixed-height segment",
	"  Toolbar: Add   Remove   Cut   Copy   Paste",
	"  Left/Right move   Shift+L/R select   Up/Down cycle block",
	"  Enter edit   i insert   a append   x delete   c copy   v paste",
	"  , / . move selection   n new   d duplicate   D remove block",
	"  Song time / position shown in this pane.",
	"",
	"# Live punch-in",
	"  Pick 'In:', arm record (F5). While playing, controller",
	"  note-on/off get recorded at the playhead on the cursor track.",
	"",
	"  Press any key to close this help.",
}

func (a *App) drawHelp(h, w int) {
	bw := 72
	if bw > w-2 {
		bw = w - 2
	}
	bh := len(helpLines) + 2
	if bh > h-2 {
		bh = h - 2
	}
	x0 := (w - bw) / 2
	y0 := (h - bh) / 2

	// Panel background.
	for r := 0; r < bh; r++ {
		a.fill(y0+r, x0, bw, ' ', styHeader)
	}
	// ASCII frame - renders identically on every terminal and font.
	border := styHeader.Bold(true)
	hbar := "+" + strings.Repeat("-", bw-2) + "+"
	a.put(y0, x0, hbar, border)
	a.put(y0+bh-1, x0, hbar, border)
	for i := 1; i < bh-1; i++ {
		a.put(y0+i, x0, "|", border)
		a.put(y0+i, x0+bw-1, "|", border)
	}

	title := " sizzletracker - keys (F1) "
	a.put(y0, x0+(bw-cellWidth(title))/2, title, border)

	for i, line := range helpLines {
		ry := y0 + 1 + i
		if ry >= y0+bh-1 {
			break
		}
		sty := styHeader
		if strings.HasPrefix(line, "# ") {
			line = line[2:]
			sty = styHeader.Bold(true)
		}
		a.put(ry, x0+2, line, sty)
	}
}

// --- helpers -------------------------------------------------------------

func formatClock(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	total := int(sec + 0.5)
	return fmt.Sprintf("%d:%02d", total/60, total%60)
}

// trunc clips s to at most n DISPLAY cells using grapheme-cluster boundaries,
// so a multibyte rune is never sliced mid-sequence (MIDI port names from the
// system can contain non-ASCII). The ellipsis is one cell.
func trunc(s string, n int) string {
	if n < 0 {
		n = 0
	}
	if cellWidth(s) <= n {
		return s
	}
	if n <= 1 {
		return runewidth.Truncate(s, n, "")
	}
	return runewidth.Truncate(s, n, "..")
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
