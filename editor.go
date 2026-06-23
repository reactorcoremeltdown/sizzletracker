package main

// Focus identifies which pane currently receives keyboard input.
type Focus int

const (
	FocusTracker Focus = iota // upper half: editing the steps of a block
	FocusArrange              // lower half: arranging blocks into a song
	FocusBPM                  // editing the BPM text field in the top bar
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
	ActTimeSig
	ActMidiOut
	ActMidiIn
	ActTrackerCell // data1=track, data2=tick, data3=column
	ActArrSlot     // data1=arrangement index
	ActBlockPick   // data1=block index (palette)
	ActAddTrack
	ActDelTrack // data1=track index to delete
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

	// Tracker cursor (which block we edit, and the cell within it).
	editBlock int
	curTrack  int
	curTick   int
	curCol    int
	octave    int // base octave for keyboard note entry
	step      int // cursor advance amount after entering a note
	follow    bool

	// Arrangement cursor + selection.
	arrCursor int
	selActive bool
	selAnchor int

	// Block palette cursor (which block "new from here" / pick uses).
	paletteCursor int

	// Clipboards.
	arrClip   []int
	blockClip *Block

	// Recording / punch-in.
	armed bool
	// punch tracks currently-held MIDI input notes so a note-off can be
	// written on the same track (and after) the matching note-on.
	punch map[int]punchInfo

	// Modal help overlay.
	showHelp bool

	// Top-bar BPM text field editing buffer.
	bpmBuf string

	// Layout bookkeeping for scrolling.
	trackScroll int // first visible track index (horizontal)

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

// selRange returns the inclusive arrangement selection range, or the cursor.
func (e *Editor) selRange() (int, int) {
	if !e.selActive {
		return e.arrCursor, e.arrCursor
	}
	a, b := e.selAnchor, e.arrCursor
	if a > b {
		a, b = b, a
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
