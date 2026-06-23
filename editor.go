package main

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
	ActTimeSig    // open the time-signature dropdown
	ActSigOption  // data1=index into timeSigs
	ActFileMenu   // open the File dropdown
	ActFileOption // data1=index into fileMenu
	ActMidiOut
	ActMidiIn
	ActTrackerCell // data1=track, data2=tick, data3=column
	ActAddTrack
	ActDelTrack  // data1=track index to delete
	ActBlockPrev // edit previous block
	ActBlockNext // edit next block
	ActLenHalf   // halve block length
	ActLenDouble // double block length
	ActLenField  // edit block length text field
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

	// Marker clipboard (rectangle of beat-markers).
	markClip [][]bool

	// Recording / punch-in.
	armed bool
	punch map[int]punchInfo

	// Modal overlays.
	showHelp bool
	showSig  bool
	sigX     int // x of the Sig field (so the dropdown can align under it)
	showFile bool
	fileX    int // x of the File menu (for dropdown alignment)

	// Modal file dialog.
	showDialog bool
	dlgAction  DialogAction
	dlgPrompt  string
	dlgBuf     string

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
		focus:     FocusTracker,
		editBlock: 0,
		octave:    4,
		step:      1,
		follow:    true,
		punch:     make(map[int]punchInfo),
		status:    "Ready. Press F1 for help.",
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
