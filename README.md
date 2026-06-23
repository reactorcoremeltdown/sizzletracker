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
  Tracks are unlimited — add/remove with the `+trk` / `-trk` controls; the
  grid scrolls horizontally. The tracker takes all vertical space left by the
  fixed-height arrangement segment and **scrolls vertically** when the block
  is taller than the screen (following the playhead during playback).
* **Adjustable block length.** A controls row exposes `len - [ N ] +`:
  `-` halves and `+` doubles the number of lines, and clicking the number
  lets you type an arbitrary length. All tracks in the block resize together.
* **Arrangement (lower half).** A fixed-height step sequencer where each slot
  references a block (a whole set of tracks), with a toolbar —
  **Add / Remove / Cut / Copy / Paste** — plus select, move, insert, and
  create / duplicate / remove blocks. A **song-time** readout (`time
  elapsed / total`, computed for one pass with no repeat) and the live
  **playhead position** (`arr i/n · row`) are shown here.
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

Requires Go ≥ 1.24 and the native **portmidi** library (the TUI is built
on [tcell](https://github.com/gdamore/tcell), pure Go, so ncurses is no
longer needed). Supported and CI-verified targets: **macOS Apple Silicon**,
**macOS Intel**, **Linux amd64**, **Linux arm64**.

Plain `go build` works on all of them with no environment variables — the
cgo directives in [`internal/portmidi/portmidi.go`](internal/portmidi/portmidi.go)
already list every standard prefix (`/opt/homebrew`, `/usr/local`, and the
system default search path).

**macOS** (Apple Silicon or Intel):

```sh
brew install portmidi
make build && make run        # or just: go build -o sizzletracker .
```

**Debian / Ubuntu** (amd64 or arm64):

```sh
sudo apt-get install -y build-essential libportmidi-dev pkg-config
make build && make run        # or just: go build -o sizzletracker .
```

If your portmidi lives somewhere non-standard, export `CGO_CFLAGS` /
`CGO_LDFLAGS` and they are merged in.

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
| `+trk` / `-trk` controls | add / delete the cursor track |
| `len  -` / `+` controls | halve / double the block length |
| click the `len` number | type an arbitrary block length |

(Large blocks scroll vertically to keep the cursor/playhead visible; the
controls row also has `<` / `>` buttons mirroring `[` / `]` for block
navigation.)

Arrangement focus (toolbar buttons: **Add Remove Cut Copy Paste**):

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
| `player.go` | Timing goroutine; each tick it *collects* note events under lock and emits MIDI after releasing the lock, so playback timing is unaffected by rendering or input. |
| `editor.go` | UI/interaction state and the clickable-region hit-test system. |
| `ui.go` | tcell rendering; copies a per-frame snapshot of the song under a brief lock, then draws from the copy. UTF-8 / wide-char aware. |
| `input.go` | tcell `EventKey` / `EventMouse` handling and punch-in. |
| `main.go` | Wiring and the event-driven loop with a ~30 fps redraw ticker. |

Concurrency / smoothness: the player runs in its own goroutine and, on each
tick, reads the song under `Song.mu` only long enough to gather the events to
play — the actual (potentially blocking) MIDI sends happen with no lock held.
The renderer likewise holds `Song.mu` only for a brief snapshot copy and then
draws without it, and the main loop redraws on a fixed ~30 fps schedule with
input processing bounded per frame. The net effect is that no amount of user
input can stall MIDI playback or screen updates. The editor state is owned
solely by the UI goroutine; MIDI input arrives on a separate goroutine and is
forwarded to the UI loop over a channel. Verified clean under `go build -race`.
