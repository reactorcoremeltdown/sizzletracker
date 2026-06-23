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
	stdscr.Timeout(40) // ms; lets the UI refresh while idle
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

func (a *App) loop() {
	for {
		// Drain any punched-in notes.
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

		ch := a.win.GetChar()
		switch {
		case ch == 0:
			// timeout: just refresh (keeps the playhead moving on screen)
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
}
