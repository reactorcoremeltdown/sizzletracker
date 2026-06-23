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
	styRollOdd  tcell.Style // faint background for odd piano-roll rows
	styRollBar  tcell.Style // bar gridline in the piano roll
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
	styRollOdd = def.Background(tcell.NewRGBColor(38, 38, 46))
	styRollBar = def.Foreground(tcell.ColorGray)
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
	bpm   float64
	sig   TimeSig
	tpb   int // ticks per beat (3/4/5)
	tpbar int // ticks per bar (12/16/20)
	bpbar int // beats per bar (4)

	numBlocks  int
	blockNames []string
	blockBeats []int
	roll       [][]bool

	edit blockView

	playBeat          int
	playBlk, playTick int
	playing           bool
	loop              LoopMode

	songBeats    int
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
		tpb:       s.ticksPerBeat(),
		tpbar:     s.ticksPerBar(),
		bpbar:     s.Sig.beatsPerBar(),
		numBlocks: len(s.Blocks),
		songBeats: s.totalBeats(),
		songTicks: s.totalTicks(),
		spt:       s.secondsPerTick(),
	}
	for i, b := range s.Blocks {
		fr.blockNames = append(fr.blockNames, b.Name)
		fr.blockBeats = append(fr.blockBeats, s.blockBeats(i))
		fr.roll = append(fr.roll, append([]bool(nil), s.Roll[i]...))
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

	fr.playBeat, fr.playBlk, fr.playTick, fr.playing, fr.loop = a.player.state()
	if fr.playing {
		if fr.loop == LoopSong {
			fr.elapsedTicks = fr.playBeat * fr.tpb
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
	a.drawPianoRoll(arrangeY, lowerH, w, fr)
	a.drawStatus(h-1, w)

	if a.ed.showSig {
		a.drawSigDropdown(fr)
	}
	if a.ed.showFile {
		a.drawFileDropdown()
	}
	if a.ed.showHelp {
		a.drawHelp(h, w)
	}
	if a.ed.showDialog {
		a.drawDialog(h, w)
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

	a.ed.fileX = x
	x = a.button(y, x, "File", a.ed.showFile, ActFileMenu)

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

	a.ed.sigX = x
	sigSty := styBtn
	if a.ed.showSig {
		sigSty = styBtnOn
	}
	sigTxt := " Sig:" + fr.sig.String() + " v "
	x = a.putRegion(y, x, sigTxt, sigSty, ActTimeSig)

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

	tpbar := fr.tpbar
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

// --- Piano roll (lower) ------------------------------------------------
//
// Each row is a block; columns are beats along the song timeline. A marker
// means the block plays that beat. Blocks interlace vertically (alternating
// row tint) and bars interlace horizontally (a gridline every beatsPerBar).

func (a *App) drawPianoRoll(top, height, w int, fr *frame) {
	if height < 2 {
		return
	}
	a.drawRollToolbar(top, w, fr)

	const gut = 6
	gridX := gut
	visBeats := w - gridX
	if visBeats < 1 {
		visBeats = 1
	}
	laneTop := top + 2
	laneRows := height - 2
	if laneRows < 1 {
		laneRows = 1
	}

	a.ed.rollBeat = clampInt(a.ed.rollBeat, 0, rollBeats-1)

	// Vertical scroll (blocks) and horizontal scroll (beats).
	if a.ed.editBlock < a.ed.rollRowScroll {
		a.ed.rollRowScroll = a.ed.editBlock
	}
	if a.ed.editBlock >= a.ed.rollRowScroll+laneRows {
		a.ed.rollRowScroll = a.ed.editBlock - laneRows + 1
	}
	a.ed.rollRowScroll = clampInt(a.ed.rollRowScroll, 0, max(0, fr.numBlocks-laneRows))
	if a.ed.rollBeat < a.ed.rollBeatScroll {
		a.ed.rollBeatScroll = a.ed.rollBeat
	}
	if a.ed.rollBeat >= a.ed.rollBeatScroll+visBeats {
		a.ed.rollBeatScroll = a.ed.rollBeat - visBeats + 1
	}
	a.ed.rollBeatScroll = clampInt(a.ed.rollBeatScroll, 0, max(0, rollBeats-visBeats))

	a.drawRollRuler(top+1, gridX, visBeats, fr)

	bpb := fr.bpbar
	r0, b0, r1, b1 := a.ed.rollSelRect()
	focused := a.ed.focus == FocusArrange

	for r := 0; r < laneRows; r++ {
		row := a.ed.rollRowScroll + r
		if row >= fr.numBlocks {
			break
		}
		y := laneTop + r

		lblSty := styBtn
		if row == a.ed.editBlock {
			lblSty = styBtnOn
		}
		a.put(y, 0, fmt.Sprintf("%-*.*s", gut-1, gut-1, fr.blockNames[row]), lblSty)
		a.ed.addRegion(Region{x: 0, y: y, w: gut - 1, h: 1, action: ActRollLabel, data1: row})

		rowOdd := row%2 == 1
		for c := 0; c < visBeats; c++ {
			beat := a.ed.rollBeatScroll + c
			if beat >= rollBeats {
				break
			}
			x := gridX + c
			marked := beat < len(fr.roll[row]) && fr.roll[row][beat]

			ch := "."
			sty := styNormal
			if rowOdd {
				sty = styRollOdd
			}
			if beat%bpb == 0 && !marked { // bar gridline on empty cells
				ch = ":"
				sty = styRollBar
			}
			if marked {
				ch = "#"
				sty = styBtnOn
			}
			if a.ed.selActive && row >= r0 && row <= r1 && beat >= b0 && beat <= b1 {
				sty = stySel
				if !marked {
					ch = " "
				}
			}
			if fr.playing && fr.loop == LoopSong && beat == fr.playBeat {
				sty = styPlayhead
				if !marked {
					ch = "|"
				}
			}
			if focused && row == a.ed.editBlock && beat == a.ed.rollBeat {
				sty = styCursor
			}
			a.put(y, x, ch, sty)
			a.ed.addRegion(Region{x: x, y: y, w: 1, h: 1, action: ActRollCell, data1: row, data2: beat})
		}
	}
}

func (a *App) drawRollRuler(y, gridX, visBeats int, fr *frame) {
	a.put(y, 0, "bar", styDim)
	bpb := fr.bpbar
	for c := 0; c < visBeats; c++ {
		beat := a.ed.rollBeatScroll + c
		if beat%bpb == 0 {
			a.put(y, gridX+c, fmt.Sprintf("%d", beat/bpb+1), styDim)
		}
	}
}

func (a *App) drawRollToolbar(y, w int, fr *frame) {
	a.fill(y, 0, w, ' ', styHeader)
	x := 1
	a.put(y, x, "ROLL", styHeader.Bold(true))
	x += 5
	x = a.button(y, x, "Add", false, ActBlockAdd)
	x = a.button(y, x, "Remove", false, ActBlockRemove)
	x = a.button(y, x, "Cut", false, ActMarkCut)
	x = a.button(y, x, "Copy", false, ActMarkCopy)
	x = a.button(y, x, "Paste", false, ActMarkPaste)

	cur := formatClock(float64(fr.elapsedTicks) * fr.spt)
	total := formatClock(float64(fr.songTicks) * fr.spt)
	clk := fmt.Sprintf(" %s/%s  %d bars ", cur, total, (fr.songBeats+fr.bpbar-1)/fr.bpbar)
	cw := cellWidth(clk)
	if x < w-cw-1 {
		a.put(y, w-cw-1, clk, styHeader.Bold(true))
	}
}

// drawSigDropdown renders the time-signature menu under the Sig field.
func (a *App) drawSigDropdown(fr *frame) {
	x0 := a.ed.sigX
	for i, ts := range timeSigs {
		lbl := fmt.Sprintf(" %-3s ", ts.String())
		sty := styBtn
		if ts == fr.sig {
			sty = styBtnOn
		}
		y := 1 + i
		a.put(y, x0, lbl, sty)
		a.ed.addRegion(Region{x: x0, y: y, w: cellWidth(lbl), h: 1, action: ActSigOption, data1: i})
	}
}

// fileMenu is the list of File-dropdown options (indices used by ActFileOption).
var fileMenu = []string{"Save", "Save As...", "Open...", "Export MIDI..."}

func (a *App) drawFileDropdown() {
	x0 := a.ed.fileX
	wmax := 0
	for _, s := range fileMenu {
		if cellWidth(s) > wmax {
			wmax = cellWidth(s)
		}
	}
	for i, opt := range fileMenu {
		lbl := fmt.Sprintf(" %-*s ", wmax, opt)
		y := 1 + i
		a.put(y, x0, lbl, styBtn)
		a.ed.addRegion(Region{x: x0, y: y, w: cellWidth(lbl), h: 1, action: ActFileOption, data1: i})
	}
}

// drawDialog renders the modal file dialog: a prompt and a text input.
func (a *App) drawDialog(h, w int) {
	bw := 60
	if bw > w-4 {
		bw = w - 4
	}
	bh := 4
	x0 := (w - bw) / 2
	y0 := (h - bh) / 2

	for r := 0; r < bh; r++ {
		a.fill(y0+r, x0, bw, ' ', styHeader)
	}
	border := styHeader.Bold(true)
	a.put(y0, x0, "+"+strings.Repeat("-", bw-2)+"+", border)
	a.put(y0+bh-1, x0, "+"+strings.Repeat("-", bw-2)+"+", border)
	for i := 1; i < bh-1; i++ {
		a.put(y0+i, x0, "|", border)
		a.put(y0+i, x0+bw-1, "|", border)
	}
	a.put(y0, x0+2, " "+a.ed.dlgPrompt+" ", border)

	input := a.ed.dlgBuf + "_"
	a.put(y0+1, x0+2, trunc(input, bw-4), styBtnOn)
	a.put(y0+2, x0+2, trunc("Enter = confirm   Esc = cancel", bw-4), styHeader)
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
	"  File menu, BPM, Sig, Out, In are clickable fields.",
	"",
	"# Global",
	"  Space play/stop   Tab switch pane   F1 help   F2/F3 focus",
	"  F5 rec   F6 loop   F7 follow   F8 panic   F9 BPM   F10 quit",
	"",
	"# Files (File menu, or keys)",
	"  Ctrl+S save   Ctrl+O open   Ctrl+E export MIDI",
	"  Projects are plain-text .sng; type a path in the dialog, Enter.",
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
	"# Piano roll (lower half) - rows are blocks, columns are beats",
	"  Arrows move cursor   Shift+arrows or drag select a region",
	"  Enter place block (bar-length markers)   . toggle one beat",
	"  Del/Bksp erase   c copy   x cut   v paste (at cursor)",
	"  Toolbar Add/Remove add/remove a block row (a / D keys)",
	"  Click a marker to toggle it; right-click to erase.",
	"",
	"# Live punch-in (polyphonic)",
	"  Pick 'In:', arm record (F5). Controller note-on/off record at",
	"  the playhead. Each held note takes its own track; chords overflow",
	"  to free tracks and new tracks are created automatically.",
	"  CC and Program Change from the input pass through to the output.",
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

	// Reserve the bottom interior row for the close hint so it shows even if
	// the content is taller than the terminal.
	hintRow := y0 + bh - 2
	for i, line := range helpLines {
		ry := y0 + 1 + i
		if ry >= hintRow {
			break
		}
		sty := styHeader
		if strings.HasPrefix(line, "# ") {
			line = line[2:]
			sty = styHeader.Bold(true)
		}
		a.put(ry, x0+2, line, sty)
	}
	a.put(hintRow, x0+2, "Press any key to close this help.", styHeader.Bold(true))
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
