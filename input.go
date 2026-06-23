package main

import (
	"fmt"
	"strconv"
	"strings"

	gc "github.com/rthornton128/goncurses"
)

// keyboard semitone maps (classic tracker layout).
var lowerRow = map[gc.Key]int{
	'z': 0, 's': 1, 'x': 2, 'd': 3, 'c': 4, 'v': 5,
	'g': 6, 'b': 7, 'h': 8, 'n': 9, 'j': 10, 'm': 11,
}
var upperRow = map[gc.Key]int{
	'q': 12, '2': 13, 'w': 14, '3': 15, 'e': 16, 'r': 17,
	'5': 18, 't': 19, '6': 20, 'y': 21, '7': 22, 'u': 23, 'i': 24,
}

// handleKey dispatches a key press based on the current focus.
func (a *App) handleKey(ch gc.Key) bool {
	// When the help overlay is up, any key dismisses it.
	if a.ed.showHelp {
		a.ed.showHelp = false
		return true
	}

	// Global keys first (work in any focus except text-field entry).
	if a.ed.focus != FocusBPM && a.ed.focus != FocusLen {
		switch ch {
		case gc.KEY_F10:
			return false // quit
		case gc.KEY_F1:
			a.ed.showHelp = true
			return true
		case ' ':
			a.player.playFrom(a.ed.arrCursor)
			return true
		case gc.KEY_TAB:
			if a.ed.focus == FocusTracker {
				a.ed.focus = FocusArrange
			} else {
				a.ed.focus = FocusTracker
			}
			return true
		case gc.KEY_F2:
			a.ed.focus = FocusTracker
			return true
		case gc.KEY_F3:
			a.ed.focus = FocusArrange
			return true
		case gc.KEY_F5:
			a.toggleArm()
			return true
		case gc.KEY_F6:
			a.toggleLoop()
			return true
		case gc.KEY_F7:
			a.ed.follow = !a.ed.follow
			a.ed.status = fmt.Sprintf("Follow playhead: %v", a.ed.follow)
			return true
		case gc.KEY_F8:
			a.midi.allNotesOff()
			a.player.allOff()
			a.ed.status = "PANIC: all notes off"
			return true
		case gc.KEY_F9:
			a.ed.focus = FocusBPM
			a.song.mu.Lock()
			a.ed.bpmBuf = strconv.FormatFloat(a.song.BPM, 'f', -1, 64)
			a.song.mu.Unlock()
			return true
		}
	}

	switch a.ed.focus {
	case FocusBPM:
		a.handleBPMKey(ch)
	case FocusLen:
		a.handleLenKey(ch)
	case FocusTracker:
		a.handleTrackerKey(ch)
	case FocusArrange:
		a.handleArrangeKey(ch)
	}
	return true
}

// handleLenKey edits the block-length text field.
func (a *App) handleLenKey(ch gc.Key) {
	switch ch {
	case gc.KEY_ENTER, gc.KEY_RETURN, 13:
		if v, err := strconv.Atoi(strings.TrimSpace(a.ed.lenBuf)); err == nil && v >= 1 {
			a.blockSetLength(v)
			a.ed.status = fmt.Sprintf("Block length set to %d", v)
		} else {
			a.ed.status = "Invalid length"
		}
		a.ed.focus = FocusTracker
	case gc.KEY_ESC:
		a.ed.focus = FocusTracker
	case gc.KEY_BACKSPACE, 127, 8:
		if len(a.ed.lenBuf) > 0 {
			a.ed.lenBuf = a.ed.lenBuf[:len(a.ed.lenBuf)-1]
		}
	default:
		if ch >= '0' && ch <= '9' && len(a.ed.lenBuf) < 4 {
			a.ed.lenBuf += string(rune(ch))
		}
	}
}

func (a *App) handleBPMKey(ch gc.Key) {
	switch ch {
	case gc.KEY_ENTER, gc.KEY_RETURN, 13:
		if v, err := strconv.ParseFloat(a.ed.bpmBuf, 64); err == nil && v >= 20 && v <= 400 {
			a.song.mu.Lock()
			a.song.BPM = v
			a.song.mu.Unlock()
			a.ed.status = fmt.Sprintf("BPM set to %.1f", v)
		} else {
			a.ed.status = "Invalid BPM (20-400)"
		}
		a.ed.focus = FocusTracker
	case gc.KEY_ESC:
		a.ed.focus = FocusTracker
	case gc.KEY_BACKSPACE, 127, 8:
		if len(a.ed.bpmBuf) > 0 {
			a.ed.bpmBuf = a.ed.bpmBuf[:len(a.ed.bpmBuf)-1]
		}
	default:
		if (ch >= '0' && ch <= '9') || ch == '.' {
			if len(a.ed.bpmBuf) < 6 {
				a.ed.bpmBuf += string(rune(ch))
			}
		}
	}
}

func (a *App) handleTrackerKey(ch gc.Key) {
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	tpb := a.song.TicksPerBeat
	tpbar := a.song.ticksPerBar()
	a.song.mu.Unlock()

	switch ch {
	case gc.KEY_UP:
		a.ed.curTick = wrap(a.ed.curTick-1, blk.Length)
	case gc.KEY_DOWN:
		a.ed.curTick = wrap(a.ed.curTick+1, blk.Length)
	case gc.KEY_PAGEUP:
		a.ed.curTick = clampInt(a.ed.curTick-tpb, 0, blk.Length-1)
	case gc.KEY_PAGEDOWN:
		a.ed.curTick = clampInt(a.ed.curTick+tpb, 0, blk.Length-1)
	case gc.KEY_HOME:
		a.ed.curTick = 0
	case gc.KEY_END:
		a.ed.curTick = blk.Length - 1
	case gc.KEY_LEFT:
		a.ed.curCol--
		if a.ed.curCol < 0 {
			a.ed.curCol = numCols - 1
			a.ed.curTrack = wrap(a.ed.curTrack-1, len(blk.Tracks))
		}
	case gc.KEY_RIGHT:
		a.ed.curCol++
		if a.ed.curCol >= numCols {
			a.ed.curCol = 0
			a.ed.curTrack = wrap(a.ed.curTrack+1, len(blk.Tracks))
		}
	case gc.KEY_SLEFT:
		a.ed.curTrack = wrap(a.ed.curTrack-1, len(blk.Tracks))
	case gc.KEY_SRIGHT:
		a.ed.curTrack = wrap(a.ed.curTrack+1, len(blk.Tracks))
	case '[':
		a.ed.editBlock = wrap(a.ed.editBlock-1, len(a.song.Blocks))
		a.player.setEditBlock(a.ed.editBlock)
		a.resetCursorToBlock()
	case ']':
		a.ed.editBlock = wrap(a.ed.editBlock+1, len(a.song.Blocks))
		a.player.setEditBlock(a.ed.editBlock)
		a.resetCursorToBlock()
	case '-', '_':
		a.ed.octave = clampInt(a.ed.octave-1, 0, 8)
	case '=', '+':
		a.ed.octave = clampInt(a.ed.octave+1, 0, 8)
	case '.', gc.KEY_DC:
		a.setCell(func(st *Step) { *st = emptyStep() })
	case gc.KEY_BACKSPACE, 127, 8:
		a.setCell(func(st *Step) { *st = emptyStep() })
		a.ed.curTick = wrap(a.ed.curTick-1, blk.Length)
	case '`':
		a.setCell(func(st *Step) { st.Note = NoteOff })
		a.advance()
	default:
		a.handleTrackerEdit(ch, tpbar)
	}
}

// handleTrackerEdit deals with column-specific value entry.
func (a *App) handleTrackerEdit(ch gc.Key, tpbar int) {
	switch a.ed.curCol {
	case ColNote:
		if semi, ok := lowerRow[ch]; ok {
			a.enterNote((a.ed.octave+1)*12 + semi)
		} else if semi, ok := upperRow[ch]; ok {
			a.enterNote((a.ed.octave+1)*12 + semi)
		}
	case ColVel:
		if d, ok := hexDigit(ch); ok {
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
		if ch >= '0' && ch <= '9' {
			d := int(ch - '0')
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

// setCell mutates the step under the tracker cursor.
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

func (a *App) handleArrangeKey(ch gc.Key) {
	a.song.mu.Lock()
	n := len(a.song.Arrangement)
	nb := len(a.song.Blocks)
	a.song.mu.Unlock()

	switch ch {
	case gc.KEY_LEFT:
		a.ed.arrCursor = clampInt(a.ed.arrCursor-1, 0, max(0, n-1))
		a.ed.selActive = false
	case gc.KEY_RIGHT:
		a.ed.arrCursor = clampInt(a.ed.arrCursor+1, 0, max(0, n-1))
		a.ed.selActive = false
	case gc.KEY_SLEFT:
		a.extendSel(-1, n)
	case gc.KEY_SRIGHT:
		a.extendSel(1, n)
	case gc.KEY_HOME:
		a.ed.arrCursor = 0
	case gc.KEY_END:
		a.ed.arrCursor = max(0, n-1)
	case gc.KEY_UP: // cycle the block referenced at the cursor slot
		a.cycleSlot(1, nb)
	case gc.KEY_DOWN:
		a.cycleSlot(-1, nb)
	case gc.KEY_ENTER, gc.KEY_RETURN, 13:
		a.song.mu.Lock()
		if a.ed.arrCursor < len(a.song.Arrangement) {
			a.ed.editBlock = a.song.Arrangement[a.ed.arrCursor]
		}
		a.song.mu.Unlock()
		a.player.setEditBlock(a.ed.editBlock)
		a.ed.focus = FocusTracker
		a.resetCursorToBlock()
	case 'i', gc.KEY_IC:
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
	case 'x', gc.KEY_DC:
		lo, hi := a.ed.selRange()
		a.song.mu.Lock()
		a.song.arrDelete(lo, hi)
		a.song.mu.Unlock()
		a.ed.arrCursor = clampInt(lo, 0, max(0, len(a.song.Arrangement)-1))
		a.ed.selActive = false
		a.ed.status = "Deleted arrangement slot(s)"
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

// --- shared transport helpers (used by keys and mouse) ---

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

func (a *App) handleMouse() {
	me := gc.GetMouse()
	if me == nil {
		return
	}
	left := me.State&gc.M_B1_CLICKED != 0 || me.State&gc.M_B1_PRESSED != 0
	dbl := me.State&gc.M_B1_DBL_CLICKED != 0
	right := me.State&gc.M_B3_CLICKED != 0 || me.State&gc.M_B3_PRESSED != 0
	shift := me.State&gc.M_SHIFT != 0

	if !left && !right && !dbl {
		return
	}
	// A click anywhere dismisses the help overlay.
	if a.ed.showHelp {
		a.ed.showHelp = false
		return
	}
	reg, ok := a.ed.hitTest(me.X, me.Y)
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
			// right-click a palette block inserts it at the arrange cursor
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

// --- arrangement block operations (toolbar + keys) ---

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

// --- punch-in from MIDI input (called on the UI goroutine via channel) ---
//
// While armed and playing, note-on writes a note at the playhead on the cursor
// track and the held note is remembered; the matching note-off writes a
// NOTE-OFF event at the playhead (on the same track), so the recorded note
// sustains for exactly as long as the key was held. While armed and stopped it
// behaves as a step recorder: note-on writes a note and advances; note-off is
// ignored (there is no running clock to time it against).
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
		// Resolve the track this note was written to.
		info, known := a.ed.punch[note]
		offTrack := tr
		if known {
			offTrack = info.track
			delete(a.ed.punch, note)
		}
		if playing && offTrack >= 0 && offTrack < len(blk.Tracks) {
			offTick := tick
			// If the key was released within the same tick it started on,
			// place the note-off on the following tick so the note survives.
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

func hexDigit(ch gc.Key) (int, bool) {
	switch {
	case ch >= '0' && ch <= '9':
		return int(ch - '0'), true
	case ch >= 'a' && ch <= 'f':
		return int(ch-'a') + 10, true
	case ch >= 'A' && ch <= 'F':
		return int(ch-'A') + 10, true
	}
	return 0, false
}
