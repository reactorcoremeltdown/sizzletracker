package main

// View selects the main screen: the editor (tracker + piano roll) or the
// MIDI patchbay.
type View int

const (
	ViewEdit View = iota
	ViewPatch
	ViewSettings
)

// Focus identifies which pane currently receives keyboard input.
type Focus int

const (
	FocusTracker Focus = iota // upper half: editing the steps of a block
	FocusArrange              // lower half: the piano roll
	FocusBPM                  // editing the BPM text field in the top bar
	FocusLen                  // editing the block-length text field
	FocusDialog               // a modal text-input dialog (open/save/export)
)

// Dialog actions for the modal file dialog.
type DialogAction int

const (
	DlgSave DialogAction = iota
	DlgOpen
	DlgExport
	DlgSaveDir // edit the default save folder
	DlgRename  // rename a block (renameTarget holds the block index)
)

// Column indices within a track.
const (
	ColNote = iota
	ColVel
	ColChan
	numCols
)

// RegionAction enumerates what a clickable screen region does.
type RegionAction int

const (
	ActNone RegionAction = iota
	ActPlay
	ActStop
	ActRecord
	ActLoopMode
	ActPanic
	ActBPM
	ActTimeSig     // open the time-signature dropdown
	ActSigOption   // data1=index into timeSigs
	ActStepMenu    // open the line-skip dropdown
	ActStepOption  // data1=index into stepOptions
	ActFileMenu    // open the File dropdown
	ActFileOption  // data1=index into fileMenu
	ActTabEdit     // switch to the editor view
	ActTabPatch    // switch to the patchbay view
	ActTabSettings // switch to the settings view
	ActAbout       // open the About popup
	ActThru        // toggle MIDI note thru (forward input notes to outputs)
	ActSettingsDir // edit the default save folder
	// Patchbay.
	ActPatchCell   // data1=input, data2=output (toggle a connection)
	ActChanMenu    // data1=output (open the channel-filter dropdown)
	ActChanCell    // data1=output, data2=channel (toggle one channel)
	ActChanAll     // data1=output (all channels on)
	ActChanNone    // data1=output (all channels off)
	ActClockToggle // data1=output (toggle MIDI clock to this output)
	ActRescan      // rescan connected MIDI devices
	ActTrackerCell // data1=track, data2=tick, data3=column
	ActAddTrack
	ActDelTrack   // data1=track index to delete
	ActBlockTitle // tracker block name (click=focus, double-click=rename)
	ActBlockPrev  // edit previous block
	ActBlockNext  // edit next block
	ActLenHalf    // halve block length
	ActLenDouble  // double block length
	ActLenField   // edit block length text field
	// Piano-roll toolbar + grid.
	ActBlockAdd    // add a block below the selected one
	ActBlockRemove // remove the selected block
	ActMarkCut     // cut the marker selection
	ActMarkCopy    // copy the marker selection
	ActMarkPaste   // paste markers at the cursor
	ActRollCell    // data1=block row, data2=beat
	ActRollLabel   // data1=block row (select for editing)
	ActSeparator   // draggable divider between tracker and piano roll
)

// Region is a hit-testable rectangle produced during drawing.
type Region struct {
	x, y, w, h          int
	action              RegionAction
	data1, data2, data3 int
}

func (r Region) hit(x, y int) bool {
	return x >= r.x && x < r.x+r.w && y >= r.y && y < r.y+r.h
}

// Editor holds all UI/interaction state (never touched by the player goroutine).
type Editor struct {
	focus Focus
	view  View

	// Patchbay cursor + scroll, and which output's channel dropdown is open
	// (-1 = none).
	patchIn     int
	patchOut    int
	patchInScr  int
	patchOutScr int
	chanMenuOut int

	// Tracker cursor (which block we edit, and the cell within it). editBlock
	// doubles as the piano-roll row cursor.
	editBlock int
	curTrack  int
	curTick   int
	curCol    int
	octave    int // base octave for keyboard note entry
	step      int // cursor advance amount after entering a note
	follow    bool

	// Piano-roll cursor + rectangular selection. The cursor corner is
	// (editBlock, rollBeat); the anchor is (selRow, selBeat) when selActive.
	rollBeat  int
	selActive bool
	selRow    int
	selBeat   int

	// Tracker rectangular selection (tracks × ticks). Cursor corner is
	// (curTrack, curTick); anchor is (tSelTrack, tSelTick) when tSelActive.
	tSelActive bool
	tSelTrack  int
	tSelTick   int
	trkClip    [][]Step // copied steps: trkClip[trackOffset][tickOffset]

	// Marker clipboard (rectangle of beat-markers).
	markClip [][]bool

	// MIDI input latch: armed (Rec) records incoming notes into the tracker;
	// thru (Latch) plays them through to the patched outputs. Together they form
	// the mode (Both / Record / Playback / Punch-in); see latchMode.
	armed bool
	thru  bool
	punch map[int]punchInfo

	// Settings.
	saveDir        string // default folder for new projects
	settingsScroll int    // scroll offset of the hotkey reference

	// Modal overlays.
	showHelp  bool
	showAbout bool
	showSig   bool
	sigX      int // x of the Sig field (so the dropdown can align under it)
	showStep  bool
	stepX     int // x of the Step field
	showFile  bool
	fileX     int // x of the File menu (for dropdown alignment)

	// Modal file dialog.
	showDialog   bool
	dlgAction    DialogAction
	dlgPrompt    string
	dlgBuf       string
	renameTarget int // block index targeted by a DlgRename dialog

	// Current project file path (for plain "Save").
	projPath string

	// Text-field editing buffers.
	bpmBuf string
	lenBuf string

	// User-chosen height of the lower (piano roll) pane; 0 = default.
	lowerH int

	// Scroll bookkeeping.
	trackScroll    int // first visible track (tracker, horizontal)
	tickScroll     int // first visible tick (tracker, vertical)
	rollBeatScroll int // first visible beat (roll, horizontal)
	rollRowScroll  int // first visible block (roll, vertical)

	// Collected each frame for mouse hit-testing.
	regions []Region

	// Transient status line message.
	status string
}

// punchInfo records where a held MIDI input note was written.
type punchInfo struct {
	track int
	tick  int
}

func newEditor() *Editor {
	return &Editor{
		focus:       FocusTracker,
		editBlock:   0,
		octave:      4,
		step:        1,
		follow:      true,
		thru:        true, // monitor the controller by default (Playback latch)
		punch:       make(map[int]punchInfo),
		chanMenuOut: -1,
		status:      "Ready. Press F1 for help.",
	}
}

// latchMode names the current MIDI input behaviour from (armed=Rec, thru=Latch):
//
//	Rec on,  Latch on  -> "Both"     (record at playhead + play through)
//	Rec on,  Latch off -> "Record"   (record at playhead, no play)
//	Rec off, Latch on  -> "Playback" (play through only, no record)
//	Rec off, Latch off -> "Punch-in" (record at the cursor, no play/follow)
func (e *Editor) latchMode() string {
	switch {
	case e.armed && e.thru:
		return "Both"
	case e.armed:
		return "Record"
	case e.thru:
		return "Playback"
	default:
		return "Punch-in"
	}
}

func (e *Editor) addRegion(r Region) { e.regions = append(e.regions, r) }

func (e *Editor) hitTest(x, y int) (Region, bool) {
	// Iterate in reverse so later (topmost) regions win.
	for i := len(e.regions) - 1; i >= 0; i-- {
		if e.regions[i].hit(x, y) {
			return e.regions[i], true
		}
	}
	return Region{}, false
}

// rollSelRect returns the inclusive selection rectangle (rows r0..r1, beats
// b0..b1). With no active selection it is just the cursor cell.
func (e *Editor) rollSelRect() (r0, b0, r1, b1 int) {
	r0, r1 = e.editBlock, e.editBlock
	b0, b1 = e.rollBeat, e.rollBeat
	if e.selActive {
		r0, r1 = minMaxInt(e.selRow, e.editBlock)
		b0, b1 = minMaxInt(e.selBeat, e.rollBeat)
	}
	return
}

// trkSelRect returns the inclusive tracker selection rectangle (tracks t0..t1,
// ticks k0..k1). With no active selection it is just the cursor cell.
func (e *Editor) trkSelRect() (t0, k0, t1, k1 int) {
	t0, t1 = e.curTrack, e.curTrack
	k0, k1 = e.curTick, e.curTick
	if e.tSelActive {
		t0, t1 = minMaxInt(e.tSelTrack, e.curTrack)
		k0, k1 = minMaxInt(e.tSelTick, e.curTick)
	}
	return
}

func minMaxInt(a, b int) (int, int) {
	if a > b {
		return b, a
	}
	return a, b
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
