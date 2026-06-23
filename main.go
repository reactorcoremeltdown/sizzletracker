// sizzletracker — a TUI MIDI tracker / step sequencer.
//
// Upper half: a vertical tracker for the currently selected block. Rows are
// ticks scanned top to bottom during playback; columns per track are
// note(+octave), velocity and MIDI channel.
//
// Lower half: an arrangement view that sequences blocks into a song — a larger
// step sequencer where each slot references a block (a set of tracks).
package main

import (
	"fmt"
	"os"
	"time"

	gc "github.com/rthornton128/goncurses"
)

// inEvent carries a MIDI note event from the input goroutine to the UI.
type inEvent struct {
	on   bool
	note int
	vel  int
}

// App wires together the document, the MIDI engine, the player and the editor.
type App struct {
	win    *gc.Window
	song   *Song
	midi   *MidiEngine
	player *Player
	ed     *Editor

	midiIn chan inEvent
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "sizzletracker:", err)
		os.Exit(1)
	}
}

func run() error {
	stdscr, err := gc.Init()
	if err != nil {
		return fmt.Errorf("ncurses init: %w", err)
	}
	defer gc.End()

	gc.Echo(false)
	gc.CBreak(true)
	gc.Cursor(0)
	stdscr.Keypad(true)
	stdscr.Timeout(0) // non-blocking input; the loop paces its own frames
	gc.MouseMask(gc.M_ALL, nil)
	if gc.HasColors() {
		initColors()
	}

	song := newSong()
	mid := newMidiEngine()
	player := newPlayer(song, mid)
	ed := newEditor()

	app := &App{
		win:    stdscr,
		song:   song,
		midi:   mid,
		player: player,
		ed:     ed,
		midiIn: make(chan inEvent, 128),
	}
	player.setEditBlock(ed.editBlock)

	// Route incoming MIDI notes to the UI goroutine (non-blocking) so the
	// editor state is only ever touched from one goroutine.
	mid.setInputCallback(func(on bool, note, vel int) {
		select {
		case app.midiIn <- inEvent{on, note, vel}:
		default:
		}
	})

	defer player.stop()
	defer mid.close()

	app.loop()
	return nil
}

// frameInterval is the UI redraw cadence (~30 fps). Rendering runs on this
// fixed schedule independent of how fast the user types, so a flood of input
// can never stall the display — and because draw() only holds the song lock
// for a brief snapshot copy, it never disturbs the playback goroutine either.
const frameInterval = 33 * time.Millisecond

func (a *App) loop() {
	for {
		start := time.Now()

		// Drain pending input without blocking. Bounded per frame so a key
		// auto-repeat flood can't starve rendering; leftovers wait one frame.
		for i := 0; i < 128; i++ {
			ch := a.win.GetChar()
			if ch == 0 {
				break
			}
			switch {
			case ch == gc.KEY_MOUSE:
				a.handleMouse()
			case ch == gc.KEY_RESIZE:
				// next draw() re-reads MaxYX
			default:
				if !a.handleKey(ch) {
					return
				}
			}
		}

		// Drain punched-in MIDI notes.
		for {
			select {
			case ev := <-a.midiIn:
				a.applyPunch(ev.on, ev.note, ev.vel)
				continue
			default:
			}
			break
		}

		a.draw()

		if d := frameInterval - time.Since(start); d > 0 {
			time.Sleep(d)
		}
	}
}
