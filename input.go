package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gdamore/tcell/v2"
)

// Keyboard-to-semitone maps for tracker note entry (classic tracker layout).
var lowerRow = map[rune]int{
	'z': 0, 's': 1, 'x': 2, 'd': 3, 'c': 4, 'v': 5,
	'g': 6, 'b': 7, 'h': 8, 'n': 9, 'j': 10, 'm': 11,
}
var upperRow = map[rune]int{
	'q': 12, '2': 13, 'w': 14, '3': 15, 'e': 16, 'r': 17,
	'5': 18, 't': 19, '6': 20, 'y': 21, '7': 22, 'u': 23, 'i': 24,
}

// handleKey dispatches a key event based on the current focus.
func (a *App) handleKey(ev *tcell.EventKey) bool {
	// Help overlay: any key dismisses it.
	if a.ed.showHelp {
		a.ed.showHelp = false
		return true
	}

	k := ev.Key()
	r := ev.Rune()
	mod := ev.Modifiers()

	// Global keys first (suspended during text-field entry).
	if a.ed.focus != FocusBPM && a.ed.focus != FocusLen {
		switch k {
		case tcell.KeyF10:
			return false
		case tcell.KeyCtrlC:
			return false
		case tcell.KeyF1:
			a.ed.showHelp = true
			return true
		case tcell.KeyTab:
			if a.ed.focus == FocusTracker {
				a.ed.focus = FocusArrange
			} else {
				a.ed.focus = FocusTracker
			}
			return true
		case tcell.KeyF2:
			a.ed.focus = FocusTracker
			return true
		case tcell.KeyF3:
			a.ed.focus = FocusArrange
			return true
		case tcell.KeyF5:
			a.toggleArm()
			return true
		case tcell.KeyF6:
			a.toggleLoop()
			return true
		case tcell.KeyF7:
			a.ed.follow = !a.ed.follow
			a.ed.status = fmt.Sprintf("Follow playhead: %v", a.ed.follow)
			return true
		case tcell.KeyF8:
			a.midi.allNotesOff()
			a.player.allOff()
			a.ed.status = "PANIC: all notes off"
			return true
		case tcell.KeyF9:
			a.ed.focus = FocusBPM
			a.song.mu.Lock()
			a.ed.bpmBuf = strconv.FormatFloat(a.song.BPM, 'f', -1, 64)
			a.song.mu.Unlock()
			return true
		}
		if k == tcell.KeyRune && r == ' ' {
			a.player.playFrom(a.ed.arrCursor)
			return true
		}
	}

	switch a.ed.focus {
	case FocusBPM:
		a.handleBPMKey(k, r)
	case FocusLen:
		a.handleLenKey(k, r)
	case FocusTracker:
		a.handleTrackerKey(k, r, mod)
	case FocusArrange:
		a.handleArrangeKey(k, r, mod)
	}
	return true
}

// --- text fields ---

func (a *App) handleBPMKey(k tcell.Key, r rune) {
	switch k {
	case tcell.KeyEnter:
		if v, err := strconv.ParseFloat(a.ed.bpmBuf, 64); err == nil && v >= 20 && v <= 400 {
			a.song.mu.Lock()
			a.song.BPM = v
			a.song.mu.Unlock()
			a.ed.status = fmt.Sprintf("BPM set to %.1f", v)
		} else {
			a.ed.status = "Invalid BPM (20-400)"
		}
		a.ed.focus = FocusTracker
	case tcell.KeyEsc:
		a.ed.focus = FocusTracker
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(a.ed.bpmBuf) > 0 {
			a.ed.bpmBuf = a.ed.bpmBuf[:len(a.ed.bpmBuf)-1]
		}
	case tcell.KeyRune:
		if (r >= '0' && r <= '9') || r == '.' {
			if len(a.ed.bpmBuf) < 6 {
				a.ed.bpmBuf += string(r)
			}
		}
	}
}

func (a *App) handleLenKey(k tcell.Key, r rune) {
	switch k {
	case tcell.KeyEnter:
		if v, err := strconv.Atoi(strings.TrimSpace(a.ed.lenBuf)); err == nil && v >= 1 {
			a.blockSetLength(v)
			a.ed.status = fmt.Sprintf("Block length set to %d", v)
		} else {
			a.ed.status = "Invalid length"
		}
		a.ed.focus = FocusTracker
	case tcell.KeyEsc:
		a.ed.focus = FocusTracker
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(a.ed.lenBuf) > 0 {
			a.ed.lenBuf = a.ed.lenBuf[:len(a.ed.lenBuf)-1]
		}
	case tcell.KeyRune:
		if r >= '0' && r <= '9' && len(a.ed.lenBuf) < 4 {
			a.ed.lenBuf += string(r)
		}
	}
}

// --- tracker ---

func (a *App) handleTrackerKey(k tcell.Key, r rune, mod tcell.ModMask) {
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	tpb := a.song.TicksPerBeat
	a.song.mu.Unlock()

	shift := mod&tcell.ModShift != 0

	switch k {
	case tcell.KeyUp:
		a.ed.curTick = wrap(a.ed.curTick-1, blk.Length)
		return
	case tcell.KeyDown:
		a.ed.curTick = wrap(a.ed.curTick+1, blk.Length)
		return
	case tcell.KeyPgUp:
		a.ed.curTick = clampInt(a.ed.curTick-tpb, 0, blk.Length-1)
		return
	case tcell.KeyPgDn:
		a.ed.curTick = clampInt(a.ed.curTick+tpb, 0, blk.Length-1)
		return
	case tcell.KeyHome:
		a.ed.curTick = 0
		return
	case tcell.KeyEnd:
		a.ed.curTick = blk.Length - 1
		return
	case tcell.KeyLeft:
		if shift {
			a.ed.curTrack = wrap(a.ed.curTrack-1, len(blk.Tracks))
		} else {
			a.ed.curCol--
			if a.ed.curCol < 0 {
				a.ed.curCol = numCols - 1
				a.ed.curTrack = wrap(a.ed.curTrack-1, len(blk.Tracks))
			}
		}
		return
	case tcell.KeyRight:
		if shift {
			a.ed.curTrack = wrap(a.ed.curTrack+1, len(blk.Tracks))
		} else {
			a.ed.curCol++
			if a.ed.curCol >= numCols {
				a.ed.curCol = 0
				a.ed.curTrack = wrap(a.ed.curTrack+1, len(blk.Tracks))
			}
		}
		return
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		a.setCell(func(st *Step) { *st = emptyStep() })
		a.ed.curTick = wrap(a.ed.curTick-1, blk.Length)
		return
	case tcell.KeyDelete:
		a.setCell(func(st *Step) { *st = emptyStep() })
		return
	}

	if k != tcell.KeyRune {
		return
	}

	switch r {
	case '[':
		a.gotoBlock(-1)
	case ']':
		a.gotoBlock(1)
	case '-', '_':
		a.ed.octave = clampInt(a.ed.octave-1, 0, 8)
	case '=', '+':
		a.ed.octave = clampInt(a.ed.octave+1, 0, 8)
	case '.':
		a.setCell(func(st *Step) { *st = emptyStep() })
	case '`':
		a.setCell(func(st *Step) { st.Note = NoteOff })
		a.advance()
	default:
		a.handleTrackerEdit(r)
	}
}

func (a *App) handleTrackerEdit(r rune) {
	switch a.ed.curCol {
	case ColNote:
		if semi, ok := lowerRow[r]; ok {
			a.enterNote((a.ed.octave+1)*12 + semi)
		} else if semi, ok := upperRow[r]; ok {
			a.enterNote((a.ed.octave+1)*12 + semi)
		}
	case ColVel:
		if d, ok := hexDigit(r); ok {
			a.setCell(func(st *Step) {
				old := st.Vel
				if old == ValEmpty {
					old = 0
				}
				v := ((old << 4) | d) & 0xff
				if v > 127 {
					v = 127
				}
				st.Vel = v
			})
		}
	case ColChan:
		if r >= '0' && r <= '9' {
			d := int(r - '0')
			a.setCell(func(st *Step) {
				disp := 0
				if st.Chan != ValEmpty {
					disp = st.Chan + 1
				}
				v := disp*10 + d
				if v > 16 || v < 1 {
					v = d
				}
				if v < 1 {
					v = 1
				}
				if v > 16 {
					v = 16
				}
				st.Chan = v - 1
			})
		}
	}
}

func (a *App) enterNote(note int) {
	if note < 0 || note > 127 {
		return
	}
	a.setCell(func(st *Step) { st.Note = note })
	a.advance()
}

func (a *App) setCell(f func(*Step)) {
	a.song.mu.Lock()
	defer a.song.mu.Unlock()
	blk := a.song.Blocks[a.ed.editBlock]
	if a.ed.curTrack < len(blk.Tracks) && a.ed.curTick < blk.Length {
		f(&blk.Tracks[a.ed.curTrack].Steps[a.ed.curTick])
	}
}

func (a *App) advance() {
	a.song.mu.Lock()
	n := a.song.Blocks[a.ed.editBlock].Length
	a.song.mu.Unlock()
	a.ed.curTick = wrap(a.ed.curTick+a.ed.step, n)
}

func (a *App) resetCursorToBlock() {
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	a.ed.curTrack = clampInt(a.ed.curTrack, 0, len(blk.Tracks)-1)
	a.ed.curTick = clampInt(a.ed.curTick, 0, blk.Length-1)
	a.song.mu.Unlock()
}

// --- arrangement ---

func (a *App) handleArrangeKey(k tcell.Key, r rune, mod tcell.ModMask) {
	a.song.mu.Lock()
	n := len(a.song.Arrangement)
	nb := len(a.song.Blocks)
	a.song.mu.Unlock()

	shift := mod&tcell.ModShift != 0

	switch k {
	case tcell.KeyLeft:
		if shift {
			a.extendSel(-1, n)
		} else {
			a.ed.arrCursor = clampInt(a.ed.arrCursor-1, 0, max(0, n-1))
			a.ed.selActive = false
		}
		return
	case tcell.KeyRight:
		if shift {
			a.extendSel(1, n)
		} else {
			a.ed.arrCursor = clampInt(a.ed.arrCursor+1, 0, max(0, n-1))
			a.ed.selActive = false
		}
		return
	case tcell.KeyHome:
		a.ed.arrCursor = 0
		return
	case tcell.KeyEnd:
		a.ed.arrCursor = max(0, n-1)
		return
	case tcell.KeyUp:
		a.cycleSlot(1, nb)
		return
	case tcell.KeyDown:
		a.cycleSlot(-1, nb)
		return
	case tcell.KeyEnter:
		a.song.mu.Lock()
		if a.ed.arrCursor < len(a.song.Arrangement) {
			a.ed.editBlock = a.song.Arrangement[a.ed.arrCursor]
		}
		a.song.mu.Unlock()
		a.player.setEditBlock(a.ed.editBlock)
		a.ed.focus = FocusTracker
		a.resetCursorToBlock()
		return
	case tcell.KeyInsert:
		a.song.mu.Lock()
		a.song.arrInsert(a.ed.arrCursor, a.ed.editBlock)
		a.song.mu.Unlock()
		a.ed.status = "Inserted block into arrangement"
		return
	case tcell.KeyDelete:
		a.arrRemoveSel()
		return
	}

	if k != tcell.KeyRune {
		return
	}
	switch r {
	case 'i':
		a.song.mu.Lock()
		a.song.arrInsert(a.ed.arrCursor, a.ed.editBlock)
		a.song.mu.Unlock()
		a.ed.status = "Inserted block into arrangement"
	case 'a':
		a.song.mu.Lock()
		a.song.arrInsert(len(a.song.Arrangement), a.ed.editBlock)
		a.ed.arrCursor = len(a.song.Arrangement) - 1
		a.song.mu.Unlock()
		a.ed.status = "Appended block to arrangement"
	case 'x':
		a.arrRemoveSel()
	case 'c':
		a.copyArr()
	case 'v':
		a.pasteArr()
	case ',', '<':
		a.moveSel(-1)
	case '.', '>':
		a.moveSel(1)
	case 'n':
		a.song.mu.Lock()
		bi := a.song.addBlock(16, 4)
		a.song.mu.Unlock()
		a.ed.editBlock = bi
		a.player.setEditBlock(bi)
		a.ed.status = "Created new block"
	case 'd':
		a.song.mu.Lock()
		bi := a.song.duplicateBlock(a.ed.editBlock)
		a.song.mu.Unlock()
		a.ed.editBlock = bi
		a.player.setEditBlock(bi)
		a.ed.status = "Duplicated block"
	case 'D':
		a.song.mu.Lock()
		a.song.removeBlock(a.ed.editBlock)
		a.ed.editBlock = clampInt(a.ed.editBlock, 0, len(a.song.Blocks)-1)
		a.song.mu.Unlock()
		a.player.setEditBlock(a.ed.editBlock)
		a.ed.status = "Removed block from palette"
	}
}

func (a *App) extendSel(delta, n int) {
	if !a.ed.selActive {
		a.ed.selActive = true
		a.ed.selAnchor = a.ed.arrCursor
	}
	a.ed.arrCursor = clampInt(a.ed.arrCursor+delta, 0, max(0, n-1))
}

func (a *App) cycleSlot(delta, nb int) {
	a.song.mu.Lock()
	defer a.song.mu.Unlock()
	if a.ed.arrCursor < len(a.song.Arrangement) && nb > 0 {
		a.song.Arrangement[a.ed.arrCursor] = wrap(a.song.Arrangement[a.ed.arrCursor]+delta, nb)
	}
}

func (a *App) copyArr() {
	lo, hi := a.ed.selRange()
	a.song.mu.Lock()
	defer a.song.mu.Unlock()
	if lo < 0 || lo >= len(a.song.Arrangement) {
		return
	}
	if hi >= len(a.song.Arrangement) {
		hi = len(a.song.Arrangement) - 1
	}
	a.ed.arrClip = append([]int{}, a.song.Arrangement[lo:hi+1]...)
	a.ed.status = fmt.Sprintf("Copied %d slot(s)", len(a.ed.arrClip))
}

func (a *App) pasteArr() {
	if len(a.ed.arrClip) == 0 {
		return
	}
	a.song.mu.Lock()
	at := a.ed.arrCursor + 1
	for i, bi := range a.ed.arrClip {
		a.song.arrInsert(at+i, bi)
	}
	a.ed.arrCursor = at + len(a.ed.arrClip) - 1
	a.song.mu.Unlock()
	a.ed.status = fmt.Sprintf("Pasted %d slot(s)", len(a.ed.arrClip))
}

func (a *App) moveSel(delta int) {
	lo, hi := a.ed.selRange()
	a.song.mu.Lock()
	nf, nt := a.song.arrMove(lo, hi, delta)
	a.song.mu.Unlock()
	if a.ed.selActive {
		a.ed.selAnchor = nf
		a.ed.arrCursor = nt
	} else {
		a.ed.arrCursor = nf
	}
	a.ed.status = "Moved selection"
}

// --- shared transport helpers ---

func (a *App) toggleArm() {
	a.ed.armed = !a.ed.armed
	if a.ed.armed {
		a.ed.status = "Record ARMED - MIDI input punches in at cursor/playhead"
	} else {
		a.ed.status = "Record off"
	}
}

func (a *App) toggleLoop() {
	if a.player.loopMode() == LoopSong {
		a.player.setLoopMode(LoopBlock)
		a.ed.status = "Loop mode: BLOCK (live looping the edited block)"
	} else {
		a.player.setLoopMode(LoopSong)
		a.ed.status = "Loop mode: SONG (play arrangement)"
	}
}

// --- mouse ---
//
// tcell delivers raw button-state snapshots, so we detect clicks as the
// transition from "no buttons" to "some buttons" and synthesize double-clicks
// from press timing + position.

func (a *App) handleMouse(ev *tcell.EventMouse) {
	cur := ev.Buttons()
	x, y := ev.Position()
	mod := ev.Modifiers()
	pressed := cur & ^a.prevBtn
	a.prevBtn = cur

	if pressed == 0 {
		return // motion or release; nothing to do
	}

	// Help overlay: a click anywhere closes it.
	if a.ed.showHelp {
		a.ed.showHelp = false
		return
	}

	left := pressed&tcell.ButtonPrimary != 0
	right := pressed&tcell.ButtonSecondary != 0
	shift := mod&tcell.ModShift != 0

	// Detect double-click on the primary button.
	dbl := false
	if left {
		now := time.Now()
		if now.Sub(a.lastClickAt) < dblClickWindow &&
			a.lastClickBtn == tcell.ButtonPrimary &&
			a.lastClickX == x && a.lastClickY == y {
			dbl = true
		}
		a.lastClickAt = now
		a.lastClickX = x
		a.lastClickY = y
		a.lastClickBtn = tcell.ButtonPrimary
	}

	reg, ok := a.ed.hitTest(x, y)
	if !ok {
		return
	}

	switch reg.action {
	case ActPlay:
		a.player.playFrom(a.ed.arrCursor)
	case ActStop:
		a.player.stop()
	case ActRecord:
		a.toggleArm()
	case ActLoopMode:
		a.toggleLoop()
	case ActPanic:
		a.midi.allNotesOff()
		a.player.allOff()
		a.ed.status = "PANIC: all notes off"
	case ActBPM:
		a.ed.focus = FocusBPM
		a.song.mu.Lock()
		a.ed.bpmBuf = strconv.FormatFloat(a.song.BPM, 'f', -1, 64)
		a.song.mu.Unlock()
	case ActTimeSig:
		a.cycleTimeSig()
	case ActMidiOut:
		a.midi.cycleOut()
		a.ed.status = "MIDI out: " + a.midi.OutName()
	case ActMidiIn:
		a.midi.cycleIn()
		a.ed.status = "MIDI in: " + a.midi.InName()
	case ActAddTrack:
		a.song.mu.Lock()
		a.song.Blocks[a.ed.editBlock].addTrack()
		a.song.mu.Unlock()
		a.ed.status = "Added track"
	case ActDelTrack:
		a.deleteCurrentTrack()
	case ActBlockPrev:
		a.gotoBlock(-1)
	case ActBlockNext:
		a.gotoBlock(1)
	case ActLenHalf:
		a.lenHalve()
	case ActLenDouble:
		a.lenDouble()
	case ActLenField:
		a.startLenEdit()
	case ActArrAdd:
		a.arrAddCurrent()
	case ActArrRemove:
		a.arrRemoveSel()
	case ActArrCut:
		a.arrCut()
	case ActArrCopy:
		a.copyArr()
	case ActArrPaste:
		a.pasteArr()
	case ActTrackerCell:
		a.ed.focus = FocusTracker
		a.ed.curTrack = reg.data1
		a.ed.curTick = reg.data2
		a.ed.curCol = reg.data3
		if right {
			a.setCell(func(st *Step) { *st = emptyStep() })
		}
	case ActArrSlot:
		a.ed.focus = FocusArrange
		if shift {
			if !a.ed.selActive {
				a.ed.selActive = true
				a.ed.selAnchor = a.ed.arrCursor
			}
			a.ed.arrCursor = reg.data1
		} else {
			a.ed.arrCursor = reg.data1
			a.ed.selActive = false
		}
		if right {
			a.song.mu.Lock()
			a.song.arrDelete(reg.data1, reg.data1)
			a.song.mu.Unlock()
		}
		if dbl {
			a.song.mu.Lock()
			if reg.data1 < len(a.song.Arrangement) {
				a.ed.editBlock = a.song.Arrangement[reg.data1]
			}
			a.song.mu.Unlock()
			a.player.setEditBlock(a.ed.editBlock)
			a.ed.focus = FocusTracker
			a.resetCursorToBlock()
		}
	case ActBlockPick:
		if right {
			a.song.mu.Lock()
			a.song.arrInsert(a.ed.arrCursor, reg.data1)
			a.song.mu.Unlock()
			a.ed.status = "Inserted block into arrangement"
		} else {
			a.ed.editBlock = reg.data1
			a.player.setEditBlock(reg.data1)
			a.resetCursorToBlock()
		}
	}
}

func (a *App) cycleTimeSig() {
	options := []TimeSig{{4, 4}, {3, 4}, {6, 8}, {5, 4}, {7, 8}, {2, 4}}
	a.song.mu.Lock()
	cur := a.song.Sig
	idx := 0
	for i, o := range options {
		if o == cur {
			idx = i
			break
		}
	}
	a.song.Sig = options[(idx+1)%len(options)]
	a.song.mu.Unlock()
	a.ed.status = "Time signature: " + a.song.Sig.String()
}

func (a *App) deleteCurrentTrack() {
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	if len(blk.Tracks) > 1 {
		blk.removeTrack(a.ed.curTrack)
		a.ed.curTrack = clampInt(a.ed.curTrack, 0, len(blk.Tracks)-1)
		a.ed.status = "Deleted track"
	} else {
		a.ed.status = "Cannot delete the last track"
	}
	a.song.mu.Unlock()
}

// --- block length & navigation ---

func (a *App) blockSetLength(n int) {
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	blk.setLength(n)
	a.ed.curTick = clampInt(a.ed.curTick, 0, blk.Length-1)
	a.song.mu.Unlock()
}

func (a *App) blockLen() int {
	a.song.mu.Lock()
	defer a.song.mu.Unlock()
	return a.song.Blocks[a.ed.editBlock].Length
}

func (a *App) lenHalve() {
	a.blockSetLength(max(1, a.blockLen()/2))
	a.ed.status = fmt.Sprintf("Block length %d", a.blockLen())
}

func (a *App) lenDouble() {
	a.blockSetLength(a.blockLen() * 2)
	a.ed.status = fmt.Sprintf("Block length %d", a.blockLen())
}

func (a *App) startLenEdit() {
	a.ed.focus = FocusLen
	a.ed.lenBuf = strconv.Itoa(a.blockLen())
}

func (a *App) gotoBlock(delta int) {
	a.song.mu.Lock()
	n := len(a.song.Blocks)
	a.song.mu.Unlock()
	a.ed.editBlock = wrap(a.ed.editBlock+delta, n)
	a.player.setEditBlock(a.ed.editBlock)
	a.resetCursorToBlock()
}

// --- arrangement toolbar operations ---

func (a *App) arrAddCurrent() {
	a.song.mu.Lock()
	at := a.ed.arrCursor + 1
	if len(a.song.Arrangement) == 0 {
		at = 0
	}
	a.song.arrInsert(at, a.ed.editBlock)
	a.ed.arrCursor = clampInt(at, 0, len(a.song.Arrangement)-1)
	a.song.mu.Unlock()
	a.ed.status = "Added block to arrangement"
}

func (a *App) arrRemoveSel() {
	lo, hi := a.ed.selRange()
	a.song.mu.Lock()
	a.song.arrDelete(lo, hi)
	n := len(a.song.Arrangement)
	a.song.mu.Unlock()
	a.ed.arrCursor = clampInt(lo, 0, max(0, n-1))
	a.ed.selActive = false
	a.ed.status = "Removed arrangement slot(s)"
}

func (a *App) arrCut() {
	a.copyArr()
	a.arrRemoveSel()
	a.ed.status = "Cut arrangement slot(s)"
}

// --- live punch-in ---
//
// While armed and playing, note-on writes a note at the playhead on the
// cursor track and the matching note-off writes a NOTE-OFF event on the same
// track. While armed and stopped it behaves as a step recorder.

func (a *App) applyPunch(on bool, note, vel int) {
	if !a.ed.armed {
		return
	}
	playing := a.player.isPlaying()

	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	tr := clampInt(a.ed.curTrack, 0, len(blk.Tracks)-1)
	tick := a.ed.curTick
	if playing {
		pb, pt, _ := a.player.playhead()
		if pb == a.ed.editBlock {
			tick = pt
		}
	}

	if on {
		if tick >= 0 && tick < blk.Length {
			st := &blk.Tracks[tr].Steps[tick]
			st.Note = note
			st.Vel = vel
		}
		a.ed.punch[note] = punchInfo{track: tr, tick: tick}
	} else {
		info, known := a.ed.punch[note]
		offTrack := tr
		if known {
			offTrack = info.track
			delete(a.ed.punch, note)
		}
		if playing && offTrack >= 0 && offTrack < len(blk.Tracks) {
			offTick := tick
			if known && offTick == info.tick {
				offTick = info.tick + 1
			}
			if offTick >= 0 && offTick < blk.Length {
				blk.Tracks[offTrack].Steps[offTick].Note = NoteOff
			}
		}
	}
	a.song.mu.Unlock()

	if on && !playing {
		a.advance()
	}
	if on {
		a.ed.status = fmt.Sprintf("Punched %s vel %d", noteName(note), vel)
	}
}

// --- tiny helpers ---

func wrap(v, n int) int {
	if n <= 0 {
		return 0
	}
	v %= n
	if v < 0 {
		v += n
	}
	return v
}

func hexDigit(r rune) (int, bool) {
	switch {
	case r >= '0' && r <= '9':
		return int(r - '0'), true
	case r >= 'a' && r <= 'f':
		return int(r-'a') + 10, true
	case r >= 'A' && r <= 'F':
		return int(r-'A') + 10, true
	}
	return 0, false
}
