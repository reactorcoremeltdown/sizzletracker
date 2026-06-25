// sizzletracker — a TUI MIDI tracker / step sequencer.
//
// Upper half: a vertical tracker for the currently selected block. Rows are
// ticks scanned top to bottom during playback; columns per track are
// note(+octave), velocity and MIDI channel.
//
// Lower half: an arrangement view that sequences blocks into a song — a larger
// step sequencer where each slot references a block (a set of tracks).
//
// Rendering and input use tcell (pure Go, UTF-8 / wide-char aware). The only
// remaining native dependency is PortMidi (vendored under internal/portmidi).
package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/gdamore/tcell/v2"
)

// inEvent carries a MIDI note event from the input goroutine to the UI.
type inEvent struct {
	on   bool
	note int
	vel  int
	ch   int // 0-based MIDI channel from the status byte
}

// App wires together the document, the MIDI engine, the player and the editor.
type App struct {
	screen tcell.Screen
	song   *Song
	midi   *MidiEngine
	player *Player
	ed     *Editor

	midiIn chan inEvent
	events chan tcell.Event

	// recPath is the crash-recovery autosave file ("" disables recovery).
	recPath string

	// Previous mouse button mask, for detecting press/drag/release transitions
	// (tcell delivers raw button-state snapshots), plus double-click tracking.
	prevBtn     tcell.ButtonMask
	lastClickAt time.Time
	lastClickX  int
	lastClickY  int

	// Piano-roll drag-select state.
	rollDrag  bool
	dragRow   int
	dragBeat  int
	dragMoved bool

	// Tracker drag-select state.
	trkDrag      bool
	trkDragT     int
	trkDragK     int
	trkDragMoved bool

	// Tracker/piano-roll separator drag state.
	sepDrag bool

	// quit is set by the File > Exit menu item to end the event loop.
	quit bool

	// Live MIDI device rescanning. A background ticker requests rescans; the
	// main loop runs at most one at a time in a goroutine (rescanInFlight) and
	// applies the result via rescanDone.
	rescanTick     chan struct{}
	rescanDone     chan bool
	rescanInFlight bool
	rescanWG       sync.WaitGroup
}

// dblClickWindow is how close two clicks at the same cell must be to count as
// a double-click.
const dblClickWindow = 400 * time.Millisecond

// frameInterval bounds the UI redraw cadence. The event loop redraws on every
// event and on each tick of this interval, so the playhead keeps moving on
// screen even when the user isn't pressing anything.
const frameInterval = 33 * time.Millisecond

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sizzletracker:", err)
		os.Exit(1)
	}
}

func run() error {
	loadPath := flag.String("load", "", "project file (.sng) to open at startup")
	exportPath := flag.String("export", "", "render the (loaded or default) song to this MIDI file and exit")
	flag.Parse()
	if *loadPath == "" && flag.NArg() > 0 {
		*loadPath = flag.Arg(0) // allow a positional project path
	}

	cfg := loadConfig()
	recPath := recoveryPath()

	// Resolve the starting song: an explicit -load wins; otherwise restore the
	// crash-recovery autosave from the previous session; otherwise start fresh.
	song := newSong()
	recovered := false
	switch {
	case *loadPath != "":
		o, err := loadProject(*loadPath)
		if err != nil {
			return fmt.Errorf("load %s: %w", *loadPath, err)
		}
		song = o
	case fileExists(recPath):
		if o, err := loadProject(recPath); err == nil {
			song = o
			recovered = true
		}
	}

	// Headless MIDI export: render and exit without launching the TUI.
	if *exportPath != "" {
		if err := writeMIDIFile(song, *exportPath); err != nil {
			return fmt.Errorf("export %s: %w", *exportPath, err)
		}
		fmt.Println("exported MIDI to", *exportPath)
		return nil
	}

	screen, err := tcell.NewScreen()
	if err != nil {
		return fmt.Errorf("tcell new screen: %w", err)
	}
	if err := screen.Init(); err != nil {
		return fmt.Errorf("tcell init: %w", err)
	}
	defer screen.Fini()

	initStyles()
	screen.SetStyle(styNormal)
	screen.EnableMouse(tcell.MouseDragEvents) // button + drag (for roll selection)
	screen.HideCursor()
	screen.Clear()

	mid := newMidiEngine()
	// Restore the patchbay routing from the previous session, if present.
	if len(cfg.Patch) > 0 || len(cfg.Filters) > 0 || len(cfg.ClockOff) > 0 {
		mid.applyPatch(cfg.Patch, cfg.Filters, cfg.ClockOff)
	}

	player := newPlayer(song, mid)
	ed := newEditor()
	if cfg.LowerH > 0 {
		ed.lowerH = cfg.LowerH
	}
	ed.saveDir = cfg.SaveDir
	ed.thru = !cfg.NoThru
	mid.setNoteThru(ed.thru)
	switch {
	case *loadPath != "":
		ed.projPath = *loadPath
		ed.status = "Loaded " + *loadPath
	case recovered:
		ed.projPath = cfg.LastPath // Save targets the real project, if any
		ed.status = "Recovered previous session (autosave)"
	}

	app := &App{
		screen:     screen,
		song:       song,
		midi:       mid,
		player:     player,
		ed:         ed,
		midiIn:     make(chan inEvent, 128),
		events:     make(chan tcell.Event, 128),
		recPath:    recPath,
		rescanTick: make(chan struct{}, 1),
		rescanDone: make(chan bool, 1),
	}
	player.setEditBlock(ed.editBlock)

	// Route incoming MIDI notes to the UI goroutine over a channel so the
	// editor state is only ever touched from one place.
	mid.setInputCallback(func(on bool, note, vel, ch int) {
		select {
		case app.midiIn <- inEvent{on, note, vel, ch}:
		default:
		}
	})

	defer player.stop()
	defer mid.close()
	// Wait for any in-flight rescan goroutine before tearing the engine down,
	// so we never run Terminate from two goroutines at once.
	defer app.rescanWG.Wait()

	// Periodically nudge the main loop to rescan MIDI devices while the
	// patchbay is open, so hot-plugged gear appears without a restart.
	pollStop := make(chan struct{})
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-pollStop:
				return
			case <-t.C:
				select {
				case app.rescanTick <- struct{}{}:
				default:
				}
			}
		}
	}()
	defer close(pollStop)

	// tcell event pump.
	quitPump := make(chan struct{})
	go func() {
		for {
			ev := screen.PollEvent()
			if ev == nil {
				return
			}
			select {
			case app.events <- ev:
			case <-quitPump:
				return
			}
		}
	}()
	defer close(quitPump)

	app.loop()

	// Clean exit: persist the working song (recovery) and preferences. The
	// loop also autosaves recovery periodically so a crash loses little.
	app.saveRecovery()
	saveAppConfig(app)
	return nil
}

// tryRescan launches a background MIDI device rescan unless one is already
// running. Automatic (ticker-driven) rescans only run while the patchbay is
// open and the transport is stopped, so they never glitch a live performance;
// a manual rescan (force=true) runs regardless. The rescan itself happens in a
// goroutine so the UI never stalls on the PortMidi re-enumeration.
func (a *App) tryRescan(force bool) {
	if a.rescanInFlight || !a.midi.isAvailable() {
		return
	}
	if !force && (a.ed.view != ViewPatch || a.player.isPlaying()) {
		return
	}
	a.rescanInFlight = true
	a.rescanWG.Add(1)
	go func() {
		defer a.rescanWG.Done()
		a.rescanDone <- a.midi.rescan() // buffered (cap 1); never blocks
	}()
}

// finishRescan applies the result of a completed rescan to the UI.
func (a *App) finishRescan(changed bool) {
	a.rescanInFlight = false
	if changed {
		a.ed.chanMenuOut = -1 // a removed output may no longer exist
		a.ed.status = fmt.Sprintf("MIDI devices updated: %d in, %d out",
			a.midi.numInputs()-1, a.midi.numOutputs())
	}
}

// saveRecovery writes the current song to the crash-recovery file. Encodes
// under the song lock, writes without it.
func (a *App) saveRecovery() {
	if a.recPath == "" {
		return
	}
	a.song.mu.Lock()
	data := encodeProject(a.song)
	a.song.mu.Unlock()
	_ = writeFile(a.recPath, []byte(data))
}

// saveAppConfig persists the current preferences.
func saveAppConfig(a *App) {
	routes, filters, clockOff := a.midi.exportPatch()
	cfg := Config{
		LowerH:   a.ed.lowerH,
		LastPath: a.ed.projPath,
		SaveDir:  a.ed.saveDir,
		NoThru:   !a.ed.thru,
		Patch:    routes,
		Filters:  filters,
		ClockOff: clockOff,
	}
	_ = cfg.save()
}

// loop is the main event/render loop. tcell events drive UI updates, the
// frame ticker guarantees a redraw cadence (so the playhead animates even
// when the user is idle), and any pending MIDI input is drained per iteration.
// autosaveEvery bounds how much work a crash can lose: the working song is
// written to the recovery file at least this often.
const autosaveEvery = 10 * time.Second

func (a *App) loop() {
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()
	nextAutosave := time.Now().Add(autosaveEvery)

	for {
		select {
		case ev := <-a.events:
			if !a.processEvent(ev) {
				return
			}
		case <-ticker.C:
		case mev := <-a.midiIn:
			a.applyPunch(mev.on, mev.note, mev.vel, mev.ch)
		case <-a.rescanTick:
			a.tryRescan(false)
		case changed := <-a.rescanDone:
			a.finishRescan(changed)
		}

		// Drain anything else that's queued so a key-repeat flood collapses
		// into a single frame. Bounded so we never block on draw.
		drain := true
		for i := 0; drain && i < 64; i++ {
			select {
			case ev := <-a.events:
				if !a.processEvent(ev) {
					return
				}
			case mev := <-a.midiIn:
				a.applyPunch(mev.on, mev.note, mev.vel, mev.ch)
			default:
				drain = false
			}
		}

		a.draw()

		if time.Now().After(nextAutosave) {
			a.saveRecovery()
			nextAutosave = time.Now().Add(autosaveEvery)
		}
	}
}

func (a *App) processEvent(ev tcell.Event) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		if !a.handleKey(ev) {
			return false
		}
	case *tcell.EventMouse:
		a.handleMouse(ev)
	case *tcell.EventResize:
		a.screen.Sync()
	}
	return !a.quit // File > Exit sets a.quit from the mouse handler
}
