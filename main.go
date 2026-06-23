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
	"fmt"
	"os"
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

	// Previous mouse button mask, for detecting press/drag/release transitions
	// (tcell delivers raw button-state snapshots).
	prevBtn tcell.ButtonMask

	// Piano-roll drag-select state.
	rollDrag  bool
	dragRow   int
	dragBeat  int
	dragMoved bool
}

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

	song := newSong()
	mid := newMidiEngine()
	player := newPlayer(song, mid)
	ed := newEditor()

	app := &App{
		screen: screen,
		song:   song,
		midi:   mid,
		player: player,
		ed:     ed,
		midiIn: make(chan inEvent, 128),
		events: make(chan tcell.Event, 128),
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
	return nil
}

// loop is the main event/render loop. tcell events drive UI updates, the
// frame ticker guarantees a redraw cadence (so the playhead animates even
// when the user is idle), and any pending MIDI input is drained per iteration.
func (a *App) loop() {
	ticker := time.NewTicker(frameInterval)
	defer ticker.Stop()

	for {
		select {
		case ev := <-a.events:
			if !a.processEvent(ev) {
				return
			}
		case <-ticker.C:
		case mev := <-a.midiIn:
			a.applyPunch(mev.on, mev.note, mev.vel, mev.ch)
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
	}
}

func (a *App) processEvent(ev tcell.Event) bool {
	switch ev := ev.(type) {
	case *tcell.EventKey:
		return a.handleKey(ev)
	case *tcell.EventMouse:
		a.handleMouse(ev)
	case *tcell.EventResize:
		a.screen.Sync()
	}
	return true
}
