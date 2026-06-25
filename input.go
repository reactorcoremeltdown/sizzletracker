package main

import (
	"fmt"
	"path/filepath"
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
	// Help / About overlays: any key dismisses them.
	if a.ed.showHelp {
		a.ed.showHelp = false
		return true
	}
	if a.ed.showAbout {
		a.ed.showAbout = false
		return true
	}

	k := ev.Key()
	r := ev.Rune()
	mod := ev.Modifiers()

	// Dropdowns / menus: Esc closes them.
	if k == tcell.KeyEsc {
		if a.ed.chanMenuOut >= 0 {
			a.ed.chanMenuOut = -1
			return true
		}
		if a.ed.showSig || a.ed.showFile || a.ed.showStep {
			a.ed.showSig = false
			a.ed.showFile = false
			a.ed.showStep = false
			return true
		}
	}

	// Modal dialog captures all keys.
	if a.ed.focus == FocusDialog {
		a.handleDialogKey(k, r)
		return true
	}

	// Global keys first (suspended during text-field entry).
	if a.ed.focus != FocusBPM && a.ed.focus != FocusLen {
		switch k {
		case tcell.KeyF10:
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
		case tcell.KeyF4:
			a.toggleView()
			return true
		case tcell.KeyF5:
			a.toggleArm()
			return true
		case tcell.KeyCtrlT:
			a.toggleThru()
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
		case tcell.KeyCtrlS:
			a.fileOption(0) // Save (opens Save As dialog if no path yet)
			return true
		case tcell.KeyCtrlO:
			a.openDialog(DlgOpen, "Open project:", a.defaultName(""))
			return true
		case tcell.KeyCtrlE:
			a.openDialog(DlgExport, "Export MIDI to:", a.defaultMidiName())
			return true
		case tcell.KeyCtrlR:
			a.startRenameBlock(a.ed.editBlock)
			return true
		}
		if k == tcell.KeyRune && r == ' ' {
			if a.player.isPlaying() {
				a.player.stop()
			} else {
				a.player.playFrom()
			}
			return true
		}
	}

	switch a.ed.focus {
	case FocusBPM:
		a.handleBPMKey(k, r)
	case FocusLen:
		a.handleLenKey(k, r)
	default:
		switch a.ed.view {
		case ViewPatch:
			a.handlePatchKey(k, r)
		case ViewSettings:
			a.handleSettingsKey(k, r)
		default:
			if a.ed.focus == FocusArrange {
				a.handleArrangeKey(k, r, mod)
			} else {
				a.handleTrackerKey(k, r, mod)
			}
		}
	}
	return true
}

// toggleView cycles Edit -> Patchbay -> Settings -> Edit.
func (a *App) toggleView() {
	switch a.ed.view {
	case ViewEdit:
		a.ed.view = ViewPatch
		a.ed.status = "MIDI patchbay (F4 cycles views)"
		a.tryRescan(false) // refresh the device list on entry
	case ViewPatch:
		a.ed.view = ViewSettings
		a.ed.status = "Settings (F4 cycles views)"
	default:
		a.ed.view = ViewEdit
	}
	a.ed.chanMenuOut = -1
}

// handleSettingsKey scrolls the hotkey reference in the settings view.
func (a *App) handleSettingsKey(k tcell.Key, r rune) {
	switch k {
	case tcell.KeyUp:
		a.ed.settingsScroll = clampInt(a.ed.settingsScroll-1, 0, len(helpLines))
	case tcell.KeyDown:
		a.ed.settingsScroll = clampInt(a.ed.settingsScroll+1, 0, len(helpLines))
	case tcell.KeyPgUp:
		a.ed.settingsScroll = clampInt(a.ed.settingsScroll-8, 0, len(helpLines))
	case tcell.KeyPgDn:
		a.ed.settingsScroll = clampInt(a.ed.settingsScroll+8, 0, len(helpLines))
	}
}

// handlePatchKey drives the patchbay matrix cursor and connections.
func (a *App) handlePatchKey(k tcell.Key, r rune) {
	ni := a.midi.numInputs()
	no := a.midi.numOutputs()
	switch k {
	case tcell.KeyLeft:
		a.ed.patchIn = clampInt(a.ed.patchIn-1, 0, max(0, ni-1))
		return
	case tcell.KeyRight:
		a.ed.patchIn = clampInt(a.ed.patchIn+1, 0, max(0, ni-1))
		return
	case tcell.KeyUp:
		a.ed.patchOut = clampInt(a.ed.patchOut-1, 0, max(0, no-1))
		return
	case tcell.KeyDown:
		a.ed.patchOut = clampInt(a.ed.patchOut+1, 0, max(0, no-1))
		return
	case tcell.KeyEnter:
		a.midi.toggleRoute(a.ed.patchIn, a.ed.patchOut)
		return
	}
	if k == tcell.KeyRune {
		switch r {
		case '*', 'x', '.':
			a.midi.toggleRoute(a.ed.patchIn, a.ed.patchOut)
		case 'c':
			if a.ed.chanMenuOut == a.ed.patchOut {
				a.ed.chanMenuOut = -1
			} else {
				a.ed.chanMenuOut = a.ed.patchOut
			}
		case 'r':
			a.ed.status = "Rescanning MIDI devices..."
			a.tryRescan(true)
		}
	}
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

// --- file dialog ---

func (a *App) handleDialogKey(k tcell.Key, r rune) {
	switch k {
	case tcell.KeyEnter:
		a.executeDialog()
	case tcell.KeyEsc:
		a.ed.showDialog = false
		a.ed.focus = FocusTracker
		a.ed.status = "Cancelled"
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if len(a.ed.dlgBuf) > 0 {
			a.ed.dlgBuf = a.ed.dlgBuf[:len(a.ed.dlgBuf)-1]
		}
	case tcell.KeyRune:
		// Accept ordinary printable characters. Block names are bounded to
		// maxBlockNameLen; paths get a generous cap.
		limit := 200
		if a.ed.dlgAction == DlgRename {
			limit = maxBlockNameLen
		}
		if r >= 0x20 && r < 0x7f && len(a.ed.dlgBuf) < limit {
			a.ed.dlgBuf += string(r)
		}
	}
}

// startRenameBlock opens the rename dialog for block i, prefilled with its
// current name.
func (a *App) startRenameBlock(i int) {
	a.song.mu.Lock()
	if i < 0 || i >= len(a.song.Blocks) {
		a.song.mu.Unlock()
		return
	}
	name := a.song.Blocks[i].Name
	a.song.mu.Unlock()
	a.ed.renameTarget = i
	a.openDialog(DlgRename, "Rename block (max 16):", name)
}

func (a *App) openDialog(action DialogAction, prompt, initial string) {
	a.ed.showFile = false
	a.ed.dlgAction = action
	a.ed.dlgPrompt = prompt
	a.ed.dlgBuf = initial
	a.ed.showDialog = true
	a.ed.focus = FocusDialog
}

// fileOption handles a click on a File-menu entry.
func (a *App) fileOption(i int) {
	switch i {
	case 0: // Save
		if a.ed.projPath != "" {
			a.doSave(a.ed.projPath)
			a.ed.showFile = false
		} else {
			a.openDialog(DlgSave, "Save project as:", a.defaultProjectPath())
		}
	case 1: // Save As...
		a.openDialog(DlgSave, "Save project as:", a.defaultName(a.defaultProjectPath()))
	case 2: // Open...
		a.openDialog(DlgOpen, "Open project:", a.defaultName(""))
	case 3: // Export MIDI...
		a.openDialog(DlgExport, "Export MIDI to:", a.defaultMidiName())
	case 4: // Exit
		a.ed.showFile = false
		a.quit = true
	}
}

func (a *App) defaultName(fallback string) string {
	if a.ed.projPath != "" {
		return a.ed.projPath
	}
	return fallback
}

func (a *App) defaultMidiName() string {
	if a.ed.projPath != "" {
		return strings.TrimSuffix(a.ed.projPath, ".sng") + ".mid"
	}
	if a.ed.saveDir != "" {
		return filepath.Join(a.ed.saveDir, "song.mid")
	}
	return "song.mid"
}

func (a *App) executeDialog() {
	path := strings.TrimSpace(a.ed.dlgBuf)
	a.ed.showDialog = false
	a.ed.focus = FocusTracker
	if a.ed.dlgAction == DlgSaveDir { // empty is allowed (= current dir)
		a.ed.saveDir = path
		if path == "" {
			a.ed.status = "Default save folder cleared"
		} else {
			a.ed.status = "Default save folder: " + path
		}
		return
	}
	if a.ed.dlgAction == DlgRename {
		name := sanitizeBlockName(a.ed.dlgBuf)
		a.song.mu.Lock()
		i := a.ed.renameTarget
		if i >= 0 && i < len(a.song.Blocks) {
			if name == "" {
				name = blockName(i) // empty reverts to the default letter
			}
			a.song.Blocks[i].Name = name
		}
		a.song.mu.Unlock()
		a.ed.status = "Renamed block to " + name
		return
	}
	if path == "" {
		a.ed.status = "Cancelled (empty path)"
		return
	}
	switch a.ed.dlgAction {
	case DlgSave:
		a.doSave(path)
	case DlgOpen:
		a.doOpen(path)
	case DlgExport:
		a.doExport(path)
	}
}

func (a *App) doSave(path string) {
	a.song.mu.Lock()
	data := encodeProject(a.song)
	a.song.mu.Unlock()
	if err := writeFile(path, []byte(data)); err != nil {
		a.ed.status = "Save failed: " + err.Error()
		return
	}
	a.ed.projPath = path
	a.ed.status = "Saved " + path
}

func (a *App) doExport(path string) {
	a.song.mu.Lock()
	data := encodeMIDI(a.song)
	a.song.mu.Unlock()
	if err := writeFile(path, data); err != nil {
		a.ed.status = "Export failed: " + err.Error()
		return
	}
	a.ed.status = "Exported MIDI to " + path
}

func (a *App) doOpen(path string) {
	o, err := loadProject(path)
	if err != nil {
		a.ed.status = "Open failed: " + err.Error()
		return
	}
	a.player.stop()
	a.song.mu.Lock()
	a.song.replaceWith(o)
	a.song.mu.Unlock()
	a.ed.editBlock = 0
	a.ed.curTrack = 0
	a.ed.curTick = 0
	a.ed.curCol = 0
	a.ed.rollBeat = 0
	a.ed.selActive = false
	a.player.setEditBlock(0)
	a.ed.projPath = path
	a.ed.status = "Opened " + path
}

// --- tracker ---

func (a *App) handleTrackerKey(k tcell.Key, r rune, mod tcell.ModMask) {
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	tpb := a.song.ticksPerBeat()
	a.song.mu.Unlock()

	shift := mod&tcell.ModShift != 0

	switch k {
	case tcell.KeyUp:
		if shift {
			a.trkExtend(0, -1)
		} else {
			a.ed.tSelActive = false
			a.ed.curTick = wrap(a.ed.curTick-1, blk.Length)
		}
		return
	case tcell.KeyDown:
		if shift {
			a.trkExtend(0, 1)
		} else {
			a.ed.tSelActive = false
			a.ed.curTick = wrap(a.ed.curTick+1, blk.Length)
		}
		return
	case tcell.KeyPgUp:
		a.ed.tSelActive = false
		a.ed.curTick = clampInt(a.ed.curTick-tpb, 0, blk.Length-1)
		return
	case tcell.KeyPgDn:
		a.ed.tSelActive = false
		a.ed.curTick = clampInt(a.ed.curTick+tpb, 0, blk.Length-1)
		return
	case tcell.KeyHome:
		a.ed.tSelActive = false
		a.ed.curTick = 0
		return
	case tcell.KeyEnd:
		a.ed.tSelActive = false
		a.ed.curTick = blk.Length - 1
		return
	case tcell.KeyLeft:
		if shift {
			a.trkExtend(-1, 0)
		} else {
			a.ed.tSelActive = false
			a.ed.curCol--
			if a.ed.curCol < 0 {
				a.ed.curCol = numCols - 1
				a.ed.curTrack = wrap(a.ed.curTrack-1, len(blk.Tracks))
			}
		}
		return
	case tcell.KeyRight:
		if shift {
			a.trkExtend(1, 0)
		} else {
			a.ed.tSelActive = false
			a.ed.curCol++
			if a.ed.curCol >= numCols {
				a.ed.curCol = 0
				a.ed.curTrack = wrap(a.ed.curTrack+1, len(blk.Tracks))
			}
		}
		return
	case tcell.KeyEsc:
		a.ed.tSelActive = false
		return
	case tcell.KeyCtrlC:
		a.trkCopy()
		return
	case tcell.KeyCtrlX:
		a.trkCut()
		return
	case tcell.KeyCtrlV:
		a.trkPaste()
		return
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if a.ed.tSelActive {
			a.trkClearSel() // clear a multi-cell selection (mouse drag / shift+arrows)
		} else {
			a.setCell(func(st *Step) { *st = emptyStep() })
			a.ed.curTick = wrap(a.ed.curTick-1, blk.Length)
		}
		return
	case tcell.KeyDelete:
		if a.ed.tSelActive {
			a.trkClearSel()
		} else {
			a.setCell(func(st *Step) { *st = emptyStep() })
		}
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
	// Keyboard-entered notes default to channel 1 (index 0), matching the
	// punch-in default.
	a.setCell(func(st *Step) {
		st.Note = note
		st.Chan = 0
	})
	a.advance()
}

func (a *App) setCell(f func(*Step)) {
	a.ed.tSelActive = false // a single-cell edit clears any selection
	a.song.mu.Lock()
	defer a.song.mu.Unlock()
	blk := a.song.Blocks[a.ed.editBlock]
	if a.ed.curTrack < len(blk.Tracks) && a.ed.curTick < blk.Length {
		f(&blk.Tracks[a.ed.curTrack].Steps[a.ed.curTick])
	}
}

// --- tracker selection (tracks × ticks) ---

// trkExtend moves the cursor by (dTrack, dTick), extending the selection
// (anchoring it on the first shift-move).
func (a *App) trkExtend(dt, dk int) {
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	nt, nk := len(blk.Tracks), blk.Length
	a.song.mu.Unlock()
	if !a.ed.tSelActive {
		a.ed.tSelActive = true
		a.ed.tSelTrack = a.ed.curTrack
		a.ed.tSelTick = a.ed.curTick
	}
	a.ed.curTrack = clampInt(a.ed.curTrack+dt, 0, max(0, nt-1))
	a.ed.curTick = clampInt(a.ed.curTick+dk, 0, max(0, nk-1))
}

func (a *App) trkCopy() {
	t0, k0, t1, k1 := a.ed.trkSelRect()
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	t0, t1 = clampInt(t0, 0, len(blk.Tracks)-1), clampInt(t1, 0, len(blk.Tracks)-1)
	k0, k1 = clampInt(k0, 0, blk.Length-1), clampInt(k1, 0, blk.Length-1)
	clip := make([][]Step, t1-t0+1)
	for t := t0; t <= t1; t++ {
		col := make([]Step, k1-k0+1)
		for k := k0; k <= k1; k++ {
			col[k-k0] = blk.Tracks[t].Steps[k]
		}
		clip[t-t0] = col
	}
	a.song.mu.Unlock()
	a.ed.trkClip = clip
	a.ed.status = fmt.Sprintf("Copied %d track(s) x %d row(s)", t1-t0+1, k1-k0+1)
}

func (a *App) trkClearSel() {
	t0, k0, t1, k1 := a.ed.trkSelRect()
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	for t := t0; t <= t1 && t < len(blk.Tracks); t++ {
		for k := k0; k <= k1 && k < blk.Length; k++ {
			if t >= 0 && k >= 0 {
				blk.Tracks[t].Steps[k] = emptyStep()
			}
		}
	}
	a.song.mu.Unlock()
	a.ed.tSelActive = false
	a.ed.status = "Cleared selection"
}

func (a *App) trkCut() {
	a.trkCopy()
	a.trkClearSel()
	a.ed.status = "Cut selection"
}

// trkPaste writes the clipboard with its top-left corner at the cursor,
// restoring note / velocity / channel into their respective columns.
func (a *App) trkPaste() {
	if len(a.ed.trkClip) == 0 {
		return
	}
	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	for dt, col := range a.ed.trkClip {
		tt := a.ed.curTrack + dt
		if tt < 0 || tt >= len(blk.Tracks) {
			continue
		}
		for dk, st := range col {
			kk := a.ed.curTick + dk
			if kk < 0 || kk >= blk.Length {
				continue
			}
			blk.Tracks[tt].Steps[kk] = st
		}
	}
	a.song.mu.Unlock()
	a.ed.tSelActive = false
	a.ed.status = "Pasted at cursor"
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

// --- piano roll ---

func (a *App) handleArrangeKey(k tcell.Key, r rune, mod tcell.ModMask) {
	shift := mod&tcell.ModShift != 0
	switch k {
	case tcell.KeyLeft:
		a.rollMove(0, -1, shift)
		return
	case tcell.KeyRight:
		a.rollMove(0, 1, shift)
		return
	case tcell.KeyUp:
		a.rollMove(-1, 0, shift)
		return
	case tcell.KeyDown:
		a.rollMove(1, 0, shift)
		return
	case tcell.KeyHome:
		a.ed.selActive = false
		a.ed.rollBeat = 0
		return
	case tcell.KeyEnter:
		a.rollPaintCursor()
		return
	case tcell.KeyDelete, tcell.KeyBackspace, tcell.KeyBackspace2:
		a.rollEraseSel()
		return
	}

	if k != tcell.KeyRune {
		return
	}
	switch r {
	case '.':
		a.rollToggle()
	case 'p':
		a.rollPaintCursor()
	case 'l':
		a.markLoopFromSelection()
	case 'c':
		a.markCopy()
	case 'x':
		a.markCut()
	case 'v':
		a.markPaste()
	case 'a':
		a.blockAddBelow()
	case 'd':
		a.blockDuplicate()
	case 'D':
		a.blockRemoveCurrent()
	}
}

// rollToggleBar toggles every beat of the bar containing the given beat (right
// click on the piano roll). If the whole bar is set it clears it, else fills it.
func (a *App) rollToggleBar(row, beat int) {
	a.song.mu.Lock()
	bpb := a.song.Sig.beatsPerBar()
	start := (beat / bpb) * bpb
	allSet := true
	for k := 0; k < bpb; k++ {
		if !a.song.rollGet(row, start+k) {
			allSet = false
			break
		}
	}
	for k := 0; k < bpb; k++ {
		a.song.rollSet(row, start+k, !allSet)
	}
	a.song.mu.Unlock()
}

// rollMove moves the roll cursor; with shift it extends the selection.
func (a *App) rollMove(dr, db int, shift bool) {
	a.song.mu.Lock()
	nb := len(a.song.Blocks)
	a.song.mu.Unlock()
	if shift {
		if !a.ed.selActive {
			a.ed.selActive = true
			a.ed.selRow = a.ed.editBlock
			a.ed.selBeat = a.ed.rollBeat
		}
	} else {
		a.ed.selActive = false
	}
	if dr != 0 {
		a.ed.editBlock = clampInt(a.ed.editBlock+dr, 0, max(0, nb-1))
		a.player.setEditBlock(a.ed.editBlock)
		a.resetCursorToBlock()
	}
	if db != 0 {
		a.ed.rollBeat = clampInt(a.ed.rollBeat+db, 0, maxRollBeats-1)
	}
}

func (a *App) rollToggle() {
	if a.ed.selActive {
		a.rollToggleSel()
		return
	}
	a.song.mu.Lock()
	v := a.song.rollGet(a.ed.editBlock, a.ed.rollBeat)
	a.song.rollSet(a.ed.editBlock, a.ed.rollBeat, !v)
	a.song.mu.Unlock()
}

// rollToggleSel toggles every beat in the current selection rectangle: if all
// of them are already marked it clears them, otherwise it marks them all. The
// selection is kept active so it can be toggled again.
func (a *App) rollToggleSel() {
	r0, b0, r1, b1 := a.ed.rollSelRect()
	a.song.mu.Lock()
	allSet := true
	for r := r0; r <= r1 && allSet; r++ {
		for b := b0; b <= b1; b++ {
			if !a.song.rollGet(r, b) {
				allSet = false
				break
			}
		}
	}
	for r := r0; r <= r1; r++ {
		for b := b0; b <= b1; b++ {
			a.song.rollSet(r, b, !allSet)
		}
	}
	a.song.mu.Unlock()
	n := (r1 - r0 + 1) * (b1 - b0 + 1)
	if allSet {
		a.ed.status = fmt.Sprintf("Cleared %d beat(s)", n)
	} else {
		a.ed.status = fmt.Sprintf("Marked %d beat(s)", n)
	}
}

func (a *App) rollPaintCursor() {
	a.song.mu.Lock()
	a.song.rollPaint(a.ed.editBlock, a.ed.rollBeat)
	a.song.mu.Unlock()
	a.ed.status = "Placed block (bar-length markers) on the roll"
}

func (a *App) rollEraseSel() {
	r0, b0, r1, b1 := a.ed.rollSelRect()
	a.song.mu.Lock()
	for r := r0; r <= r1; r++ {
		for b := b0; b <= b1; b++ {
			a.song.rollSet(r, b, false)
		}
	}
	a.song.mu.Unlock()
	a.ed.selActive = false
	a.ed.status = "Erased markers"
}

func (a *App) markCopy() {
	r0, b0, r1, b1 := a.ed.rollSelRect()
	clip := make([][]bool, 0, r1-r0+1)
	a.song.mu.Lock()
	for r := r0; r <= r1; r++ {
		row := make([]bool, b1-b0+1)
		for b := b0; b <= b1; b++ {
			row[b-b0] = a.song.rollGet(r, b)
		}
		clip = append(clip, row)
	}
	a.song.mu.Unlock()
	a.ed.markClip = clip
	a.ed.status = fmt.Sprintf("Copied %dx%d markers", len(clip), b1-b0+1)
}

func (a *App) markCut() {
	a.markCopy()
	a.rollEraseSel()
	a.ed.status = "Cut markers"
}

// markPaste writes the clipboard with its top-left corner at the cursor.
func (a *App) markPaste() {
	if len(a.ed.markClip) == 0 {
		return
	}
	a.song.mu.Lock()
	for dr, row := range a.ed.markClip {
		for db, v := range row {
			a.song.rollSet(a.ed.editBlock+dr, a.ed.rollBeat+db, v)
		}
	}
	a.song.mu.Unlock()
	a.ed.status = "Pasted markers at cursor"
}

// --- block list operations (piano-roll rows) ---

func (a *App) blockAddBelow() {
	a.song.mu.Lock()
	at := a.song.addBlockAt(a.ed.editBlock)
	a.song.mu.Unlock()
	a.ed.editBlock = at
	a.player.setEditBlock(at)
	a.resetCursorToBlock()
	a.ed.status = "Added block below"
}

func (a *App) blockDuplicate() {
	a.song.mu.Lock()
	at := a.song.duplicateBlockAt(a.ed.editBlock)
	a.song.mu.Unlock()
	a.ed.editBlock = at
	a.player.setEditBlock(at)
	a.resetCursorToBlock()
	a.ed.status = "Duplicated block"
}

func (a *App) blockRemoveCurrent() {
	a.song.mu.Lock()
	a.song.removeBlockAt(a.ed.editBlock)
	a.ed.editBlock = clampInt(a.ed.editBlock, 0, len(a.song.Blocks)-1)
	a.song.mu.Unlock()
	a.player.setEditBlock(a.ed.editBlock)
	a.resetCursorToBlock()
	a.ed.status = "Removed block"
}

// setLowerFromY resizes the lower (piano-roll) pane so its separator sits at
// screen row y. layout() clamps the result so both panes stay visible.
func (a *App) setLowerFromY(y int) {
	_, h := a.screen.Size()
	a.ed.lowerH = h - 2 - y // status row is h-1; lower pane is y+1 .. h-2
}

// --- time-signature dropdown ---

func (a *App) selectSig(i int) {
	if i < 0 || i >= len(timeSigs) {
		return
	}
	a.song.mu.Lock()
	a.song.setSig(timeSigs[i])
	a.song.mu.Unlock()
	a.ed.showSig = false
	a.resetCursorToBlock()
	a.ed.status = "Time signature: " + timeSigs[i].String()
}

// --- shared transport helpers ---

func (a *App) toggleArm() {
	a.ed.armed = !a.ed.armed
	a.ed.status = "MIDI latch: " + a.ed.latchMode()
}

// toggleThru flips note thru (forwarding incoming notes to the patched outputs).
func (a *App) toggleThru() {
	a.ed.thru = !a.ed.thru
	a.midi.setNoteThru(a.ed.thru)
	a.ed.status = "MIDI latch: " + a.ed.latchMode()
}

// defaultProjectPath is the suggested path for a new project (save dialog),
// rooted in the configured save folder.
func (a *App) defaultProjectPath() string {
	if a.ed.saveDir != "" {
		return filepath.Join(a.ed.saveDir, "song.sng")
	}
	return "song.sng"
}

func (a *App) toggleLoop() {
	if a.player.loopMode() == LoopSong {
		a.player.setLoopMode(LoopRegion)
		a.ed.status = "Loop: REGION (loops the marked bars)"
	} else {
		a.player.setLoopMode(LoopSong)
		a.ed.status = "Loop: SONG (play once to the end)"
	}
}

// markLoopFromSelection sets the loop region to the bars covered by the current
// piano-roll selection (extended to whole bars) and switches to loop mode.
func (a *App) markLoopFromSelection() {
	_, b0, _, b1 := a.ed.rollSelRect()
	a.song.mu.Lock()
	bpb := a.song.Sig.beatsPerBar()
	a.song.LoopBar0 = b0 / bpb
	a.song.LoopBar1 = b1 / bpb
	bar0, bar1 := a.song.LoopBar0, a.song.LoopBar1
	a.song.mu.Unlock()
	a.ed.selActive = false
	a.player.setLoopMode(LoopRegion)
	a.ed.status = fmt.Sprintf("Loop region: bars %d-%d", bar0+1, bar1+1)
}

// --- mouse ---
//
// tcell delivers raw button-state snapshots, so we detect clicks as the
// transition from "no buttons" to "some buttons" and synthesize double-clicks
// from press timing + position.

func (a *App) handleMouse(ev *tcell.EventMouse) {
	cur := ev.Buttons()
	prev := a.prevBtn
	a.prevBtn = cur
	x, y := ev.Position()
	shift := ev.Modifiers()&tcell.ModShift != 0

	pPress := cur&tcell.ButtonPrimary != 0 && prev&tcell.ButtonPrimary == 0
	pHeld := cur&tcell.ButtonPrimary != 0 && prev&tcell.ButtonPrimary != 0
	pRelease := cur&tcell.ButtonPrimary == 0 && prev&tcell.ButtonPrimary != 0
	sPress := cur&tcell.ButtonSecondary != 0 && prev&tcell.ButtonSecondary == 0

	// A drag begun on the piano roll extends the selection while held. A plain
	// click only moves the cursor / starts a selection (markers are toggled by
	// double-click or right-click, see below).
	if a.rollDrag {
		if pHeld {
			if reg, ok := a.ed.hitTest(x, y); ok && reg.action == ActRollCell {
				if reg.data1 != a.dragRow || reg.data2 != a.dragBeat {
					a.dragMoved = true
				}
				if a.dragMoved {
					a.ed.selActive = true
					a.ed.selRow = a.dragRow
					a.ed.selBeat = a.dragBeat
					a.ed.editBlock = reg.data1
					a.player.setEditBlock(reg.data1)
					a.resetCursorToBlock()
					a.ed.rollBeat = reg.data2
				}
			}
			return
		}
		if pRelease {
			a.rollDrag = false
			return
		}
		return
	}

	// Dragging the tracker / piano-roll separator resizes the lower pane.
	if a.sepDrag {
		if pHeld {
			a.setLowerFromY(y)
			return
		}
		if pRelease {
			a.sepDrag = false
			return
		}
		return
	}

	// Dragging over tracker cells selects a rectangle of steps.
	if a.trkDrag {
		if pHeld {
			if reg, ok := a.ed.hitTest(x, y); ok && reg.action == ActTrackerCell {
				if reg.data1 != a.trkDragT || reg.data2 != a.trkDragK {
					a.trkDragMoved = true
				}
				if a.trkDragMoved {
					a.ed.tSelActive = true
					a.ed.tSelTrack = a.trkDragT
					a.ed.tSelTick = a.trkDragK
					a.ed.curTrack = reg.data1
					a.ed.curTick = reg.data2
				}
			}
			return
		}
		if pRelease {
			a.trkDrag = false
			return
		}
		return
	}

	if !pPress && !sPress {
		return // motion or release with nothing pending
	}
	if a.ed.showHelp {
		a.ed.showHelp = false
		return
	}
	if a.ed.showAbout {
		a.ed.showAbout = false
		return
	}
	if a.ed.showDialog {
		return // modal: use Enter / Esc
	}

	reg, ok := a.ed.hitTest(x, y)
	if !ok {
		a.ed.showSig = false
		a.ed.showStep = false
		a.ed.showFile = false
		a.ed.chanMenuOut = -1
		return
	}
	right := sPress

	// Channel dropdown is semi-modal: a click off its controls just closes it.
	if a.ed.chanMenuOut >= 0 {
		switch reg.action {
		case ActChanCell, ActChanAll, ActChanNone, ActChanMenu, ActClockToggle:
			// handled below; keep the dropdown open
		default:
			a.ed.chanMenuOut = -1
			return
		}
	}

	switch reg.action {
	case ActPlay:
		if a.player.isPlaying() {
			a.player.stop()
		} else {
			a.player.playFrom()
		}
	case ActStop:
		a.player.stop()
	case ActRecord:
		a.toggleArm()
	case ActThru:
		a.toggleThru()
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
		a.ed.showSig = !a.ed.showSig
		a.ed.showFile = false
		a.ed.showStep = false
	case ActSigOption:
		a.selectSig(reg.data1)
	case ActStepMenu:
		a.ed.showStep = !a.ed.showStep
		a.ed.showSig = false
		a.ed.showFile = false
	case ActStepOption:
		if reg.data1 >= 0 && reg.data1 < len(stepOptions) {
			a.ed.step = stepOptions[reg.data1]
			a.ed.status = fmt.Sprintf("Punch-in line skip: %d", a.ed.step)
		}
		a.ed.showStep = false
	case ActFileMenu:
		a.ed.showFile = !a.ed.showFile
		a.ed.showSig = false
		a.ed.showStep = false
	case ActFileOption:
		a.fileOption(reg.data1)
	case ActTabEdit:
		a.ed.view = ViewEdit
		a.ed.chanMenuOut = -1
	case ActTabPatch:
		a.ed.view = ViewPatch
		a.ed.chanMenuOut = -1
		a.tryRescan(false) // refresh the device list on entry
	case ActRescan:
		a.ed.status = "Rescanning MIDI devices..."
		a.tryRescan(true)
	case ActTabSettings:
		a.ed.view = ViewSettings
		a.ed.chanMenuOut = -1
	case ActAbout:
		a.ed.showAbout = true
	case ActSettingsDir:
		a.openDialog(DlgSaveDir, "Default save folder:", a.ed.saveDir)
	case ActPatchCell:
		a.ed.patchIn = reg.data1
		a.ed.patchOut = reg.data2
		a.midi.toggleRoute(reg.data1, reg.data2)
	case ActChanMenu:
		if a.ed.chanMenuOut == reg.data1 {
			a.ed.chanMenuOut = -1
		} else {
			a.ed.chanMenuOut = reg.data1
		}
	case ActChanCell:
		a.midi.toggleFilter(reg.data1, reg.data2) // keeps the dropdown open
	case ActChanAll:
		a.midi.setFilterAll(reg.data1, true)
	case ActChanNone:
		a.midi.setFilterAll(reg.data1, false)
	case ActClockToggle:
		a.midi.toggleClock(reg.data1) // keeps the dropdown open
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
	case ActBlockAdd:
		a.blockAddBelow()
	case ActBlockRemove:
		a.blockRemoveCurrent()
	case ActMarkCut:
		a.markCut()
	case ActMarkCopy:
		a.markCopy()
	case ActMarkPaste:
		a.markPaste()
	case ActTrackerCell:
		a.ed.focus = FocusTracker
		a.ed.curTrack = reg.data1
		a.ed.curTick = reg.data2
		a.ed.curCol = reg.data3
		if right {
			a.setCell(func(st *Step) { *st = emptyStep() })
		} else {
			// Begin a drag-select; a plain click (no drag) just moves the cursor.
			a.ed.tSelActive = false
			a.trkDrag = true
			a.trkDragT = reg.data1
			a.trkDragK = reg.data2
			a.trkDragMoved = false
		}
	case ActSeparator:
		a.sepDrag = true
		a.setLowerFromY(y)
	case ActBlockTitle:
		now := time.Now()
		dbl := now.Sub(a.lastClickAt) < dblClickWindow && a.lastClickX == x && a.lastClickY == y
		a.lastClickAt, a.lastClickX, a.lastClickY = now, x, y
		a.ed.focus = FocusTracker
		if dbl {
			a.startRenameBlock(a.ed.editBlock)
		}
	case ActRollLabel:
		now := time.Now()
		dbl := now.Sub(a.lastClickAt) < dblClickWindow && a.lastClickX == x && a.lastClickY == y
		a.lastClickAt, a.lastClickX, a.lastClickY = now, x, y
		a.ed.focus = FocusArrange
		a.ed.selActive = false
		a.ed.editBlock = reg.data1
		a.player.setEditBlock(reg.data1)
		a.resetCursorToBlock()
		if dbl {
			a.startRenameBlock(reg.data1)
		}
	case ActRollCell:
		a.ed.focus = FocusArrange
		a.ed.editBlock = reg.data1
		a.player.setEditBlock(reg.data1)
		a.resetCursorToBlock()
		a.ed.rollBeat = reg.data2
		switch {
		case right:
			// Right-click toggles the whole bar.
			a.rollToggleBar(reg.data1, reg.data2)
			a.ed.selActive = false
		case shift:
			if !a.ed.selActive {
				a.ed.selActive = true
				a.ed.selRow = reg.data1
				a.ed.selBeat = reg.data2
			}
		default:
			// Double-click toggles a single beat; a single click starts a
			// cursor move / drag-select.
			now := time.Now()
			dbl := now.Sub(a.lastClickAt) < dblClickWindow && a.lastClickX == x && a.lastClickY == y
			a.lastClickAt = now
			a.lastClickX = x
			a.lastClickY = y
			if dbl {
				a.rollToggle()
				a.ed.selActive = false
			} else {
				a.ed.selActive = false
				a.rollDrag = true
				a.dragRow = reg.data1
				a.dragBeat = reg.data2
				a.dragMoved = false
			}
		}
	}

	// A click that is not on a menu (or its toggle) dismisses the dropdowns.
	if reg.action != ActSigOption && reg.action != ActTimeSig {
		a.ed.showSig = false
	}
	if reg.action != ActStepOption && reg.action != ActStepMenu {
		a.ed.showStep = false
	}
	if reg.action != ActFileOption && reg.action != ActFileMenu {
		a.ed.showFile = false
	}
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

// --- live punch-in (polyphonic) ---
//
// Each held incoming note occupies its own track until its note-off. A new
// note is placed on the first free track (scanning from the cursor, wrapping);
// if every track is already holding a note a new track is created. While
// playing, note-on/off are written at the playhead; while stopped it is a
// chord step-recorder: notes of a chord stack at the cursor tick and the
// cursor only advances once the whole chord has been released.

// freePunchTrack returns a track to record a new incoming note on: the first
// track not currently holding a punched note, scanning from the cursor and
// wrapping. If all tracks are busy it appends a new one. Caller holds song.mu.
func (a *App) freePunchTrack(blk *Block) int {
	occupied := make(map[int]bool, len(a.ed.punch))
	for _, info := range a.ed.punch {
		occupied[info.track] = true
	}
	n := len(blk.Tracks)
	start := clampInt(a.ed.curTrack, 0, n-1)
	for off := 0; off < n; off++ {
		idx := (start + off) % n
		if !occupied[idx] {
			return idx
		}
	}
	blk.addTrack()
	return len(blk.Tracks) - 1
}

func (a *App) applyPunch(on bool, note, vel, ch int) {
	// Rec (armed) + Latch (thru) decide what happens to incoming MIDI:
	//   Rec on,  Latch on  -> record at playhead (follow) + play through
	//   Rec on,  Latch off -> record at playhead (follow), no play through
	//   Rec off, Latch on  -> play through only, do NOT record
	//   Rec off, Latch off -> punch-in: record at the cursor, no follow/play
	// Forwarding ("play") is done by the MIDI engine (thru); here we only
	// decide whether to write the note into the tracker. The single case that
	// records nothing is play-only (Rec off, Latch on).
	if !a.ed.armed && a.ed.thru {
		return
	}
	// Record the incoming channel; default to channel 1 (index 0) when the
	// channel is unknown / out of range.
	if ch < 0 || ch > 15 {
		ch = 0
	}
	// Follow the playhead only while armed and playing (live recording).
	// Otherwise incoming notes behave like keyboard entry: written at the edit
	// cursor (a chord step-recorder), with the view staying on the cursor.
	atPlayhead := a.ed.armed && a.player.isPlaying()

	a.song.mu.Lock()
	blk := a.song.Blocks[a.ed.editBlock]
	tick := a.ed.curTick
	if atPlayhead {
		pb, pt, _ := a.player.playhead()
		if pb == a.ed.editBlock {
			tick = pt
		}
	}

	if on {
		tr := a.freePunchTrack(blk)
		if tick >= 0 && tick < blk.Length {
			st := &blk.Tracks[tr].Steps[tick]
			st.Note = note
			st.Vel = vel
			st.Chan = ch
		}
		a.ed.punch[note] = punchInfo{track: tr, tick: tick}
		voices := len(a.ed.punch)
		a.song.mu.Unlock()
		a.ed.status = fmt.Sprintf("Punched %s vel %d -> T%d (%d voice)", noteName(note), vel, tr+1, voices)
		return
	}

	// note-off
	info, known := a.ed.punch[note]
	if known {
		delete(a.ed.punch, note)
	}
	if atPlayhead && known && info.track >= 0 && info.track < len(blk.Tracks) {
		offTick := tick
		if offTick == info.tick {
			offTick = info.tick + 1 // released within the same tick; keep the note
		}
		if offTick >= 0 && offTick < blk.Length {
			blk.Tracks[info.track].Steps[offTick].Note = NoteOff
		}
	}
	chordDone := len(a.ed.punch) == 0
	a.song.mu.Unlock()

	// Cursor step-recorder (not following the playhead): advance only once the
	// whole chord has been released.
	if !atPlayhead && known && chordDone {
		a.advance()
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
