# sizzletracker

A terminal (TUI) MIDI tracker / step sequencer written in Go on top of
ncurses, with live pattern editing for MIDI "looping".

```
┌ top bar ─────────────────────────────────────────────────────────────────┐
│ SIZZLE ▶Play ■Stop ●Rec Loop:Song Panic  BPM:120  Sig:4/4  Out:… In:…      │
├ tracker (upper half) ──────────────────────────────────────────────────────┤
│ BLK A [1/2] len 16  oct4 step1                                              │
│      T1   c01    T2   c02    T3   c03    T4   c04   +trk                     │
│ 000 |C-4 7F 01  ---  .. ..  ...                                             │  ← bar  (bold)
│ 001 |---  .. ..  ...                                                        │
│ 002 |---  .. ..  ...                                                        │
│ 003 |---  .. ..  ...                                                        │
│ 004 |E-4 64 01  ...                                                         │  ← beat (cyan)
│ ...                                                                         │
├ arrangement (lower half) ──────────────────────────────────────────────────┤
│ ARRANGEMENT   [A] [B] [C]                                                   │
│ 00:A  01:B  02:A  03:A                                                      │
└ status ────────────────────────────────────────────────────────────────────┘
```

## What it does

* **Tracker (upper half).** A vertical pattern editor for the currently
  selected *block*. Each row is a tick; playback scans rows top to bottom.
  Rows are interlaced and **beats / bars are highlighted** (bar rows bold
  yellow, beat rows cyan). Each track has three columns:
  **note (+octave)**, **velocity** (hex), and **MIDI channel**.
  Tracks are unlimited — add more with the `+trk` header button; the grid
  scrolls horizontally.
* **Arrangement (lower half).** A larger step sequencer where each slot
  references a block (a whole set of tracks). Select, copy, paste, move,
  insert, delete slots; create / duplicate / remove blocks.
* **Transport in the top bar** with glyph buttons — `|>` play/stop, `[]`
  stop, `()` record-arm, `<>`/`@@` loop song/block, `!!` panic — plus an
  editable **BPM** field, a clickable **time-signature** cycle, and **MIDI
  output / input** selectors.
* **F1 help overlay** listing every hotkey; any key or click dismisses it.
* **Live editing while playing.** Edits to the model are picked up by the
  playback engine on the next tick, so you can build loops in real time.
  Set **Loop:Block** to repeat the edited block indefinitely for live
  looping.
* **MIDI punch-in with note length.** Arm record (`()` / `F5`) and select a
  MIDI input. When **playing**, controller note-on writes a note at the
  playhead on the cursor track and the matching **note-off** writes a
  NOTE-OFF event — so the recorded note sustains exactly as long as you held
  the key. When **stopped** it acts as a step recorder (note-on writes and
  advances). A note sounds until a note-off (or a retrigger) on its track.
* **Mouse support** throughout: click transport buttons, click a tracker
  cell to move the cursor, right-click a cell to clear it, click arrangement
  slots (shift-click to extend a selection, double-click to jump to editing
  that block), click palette blocks to select/insert.

## Build

Requires Go (cgo enabled) and the native **portmidi** and **ncurses**
libraries. Supported and CI-verified targets: **macOS Apple Silicon**,
**macOS Intel**, **Linux amd64**, **Linux arm64**.

Plain `go build` works on all of them with no environment variables — the
cgo directives in [`internal/portmidi/portmidi.go`](internal/portmidi/portmidi.go)
already list every standard prefix (`/opt/homebrew`, `/usr/local`, and the
system default search path).

**macOS** (Apple Silicon or Intel):

```sh
brew install portmidi ncurses
make build && make run        # or just: go build -o sizzletracker .
```

**Debian / Ubuntu** (amd64 or arm64):

```sh
sudo apt-get install -y build-essential libportmidi-dev libncurses-dev pkg-config
make build && make run        # or just: go build -o sizzletracker .
```

ncurses itself needs no special flags: goncurses links the system ncurses
on macOS and uses `pkg-config` on Linux. If your libraries live somewhere
non-standard, export `CGO_CFLAGS` / `CGO_LDFLAGS` and they are merged in.

If portmidi can't be initialised the app still runs (the output selector
shows `<no portmidi>`); everything works except sound.

> The PortMidi binding under `internal/portmidi` is a small vendored copy of
> [`rakyll/portmidi`](https://github.com/rakyll/portmidi) (Apache-2.0), with
> only the cgo build directives changed to be platform-aware so no Homebrew
> prefix shimming is needed.

## Keyboard

Global:

| Key | Action |
|-----|--------|
| `Space` | Play / Stop from the arrangement cursor |
| `Tab` | Toggle focus tracker ↔ arrangement |
| `F2` / `F3` | Focus tracker / arrangement |
| `F5` | Toggle record-arm (punch-in) |
| `F6` | Toggle loop mode (Song ↔ Block) |
| `F7` | Toggle follow-playhead |
| `F8` | Panic (all notes off) |
| `F9` | Edit BPM field (type, `Enter` confirm, `Esc` cancel) |
| `F1` | Open the help overlay (any key/click closes it) |
| `F10` | Quit |

Tracker focus:

| Key | Action |
|-----|--------|
| Arrows | Move cursor (←/→ cross columns/tracks) |
| `Shift`+←/→ | Previous / next track |
| `PgUp` / `PgDn` | Jump one beat |
| `Home` / `End` | Top / bottom of block |
| `[` / `]` | Previous / next block to edit |
| `z s x d c v g b h n j m` | Enter notes (lower octave) |
| `q 2 w 3 e r 5 t 6 y 7 u i` | Enter notes (upper octave) |
| `` ` `` | Note-off (the note sustains until this, or a new note) |
| `.` / `Del` | Clear cell |
| `Backspace` | Clear cell and step back |
| `-` / `=` | Octave down / up |
| velocity column | hex digits `0-9 a-f` (two-nibble entry) |
| channel column | decimal digits (1–16) |
| `+trk` / `-trk` header buttons | add / delete the cursor track |

Arrangement focus:

| Key | Action |
|-----|--------|
| ←/→ | Move slot cursor |
| `Shift`+←/→ | Extend selection |
| ↑/↓ | Cycle the block referenced by the slot |
| `Enter` | Jump to editing the slot's block |
| `i` / `Ins` | Insert current block at cursor |
| `a` | Append current block |
| `x` / `Del` | Delete slot(s) |
| `c` / `v` | Copy / paste selection |
| `,` `.` (or `<` `>`) | Move selection left / right |
| `n` | New block (added to palette) |
| `d` | Duplicate current block |
| `D` | Remove current block from palette |

## Architecture

| File | Responsibility |
|------|----------------|
| `model.go` | Data model: `Song` → `Block` → `Track` → `Step`, plus arrangement edits. Guarded by `Song.mu`. |
| `midi.go` | PortMidi output/input wrapper (port selection, note on/off, punch-in listener). |
| `internal/portmidi/` | Vendored PortMidi cgo binding with platform-aware build flags. |
| `player.go` | Timing goroutine; reads the song under lock each tick (so live edits apply) and manages per-track note lifecycles. |
| `editor.go` | UI/interaction state and the clickable-region hit-test system. |
| `ui.go` | ncurses rendering of top bar, tracker and arrangement. |
| `input.go` | Keyboard + mouse handling and punch-in. |
| `main.go` | Wiring and the event loop. |

Concurrency: the player runs in its own goroutine and touches only the song
(under `Song.mu`) and MIDI. The editor state is owned solely by the UI
goroutine; MIDI input arrives on a separate goroutine and is forwarded to the
UI loop over a channel, so the editor is never accessed concurrently.
