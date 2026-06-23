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
  Rows are interlaced and **beats / bars are highlighted**. The number of
  lines per beat follows the time signature — **3 for 3/4, 4 for 4/4, 5 for
  5/4** — so a bar is 12 / 16 / 20 lines. Each track has three columns:
  **note (+octave)**, **velocity** (hex), and **MIDI channel**.
  Tracks are unlimited — add/remove with the `+trk` / `-trk` controls; the
  grid scrolls horizontally, and vertically when the block is taller than the
  screen (following the playhead during playback).
* **Adjustable block length** in bar-sized steps. `len - [ N ] +`: `-` halves
  and `+` doubles the number of lines (12 / 24 / 48 for 3/4, 16 / 32 / 64 for
  4/4, 20 / 40 / 80 for 5/4), and clicking the number lets you type a length.
* **Piano roll (lower half).** A 2D arrangement: each **row is a block**, each
  **column is a beat**, and a **marker** means the block plays that beat.
  Placing a block paints a whole bar's worth of markers; you can erase
  individual beats. The playhead scans **left → right**. Blocks interlace
  vertically and bars interlace horizontally (gridlines every 4 beats).
  **Cut / Copy / Paste** act on a rectangular marker selection (drag or
  shift+arrows to select; paste lands with its top-left at the cursor).
  **Add / Remove** add a block row below / remove the selected one. A
  song-time and bar-count readout is shown on the toolbar. **Drag the
  separator bar** between the tracker and the roll to resize the two panes.
* **Transport in the top bar** with glyph buttons — `▶` play, `■` stop,
  `●` record-arm, `⟲`/`⟳` loop song/block, `⚠` panic — plus an editable
  **BPM** field, a **time-signature dropdown** (3/4, 4/4, 5/4), and **Edit /
  Patchbay** view tabs.
* **MIDI patchbay** (the **Patchbay** tab, or `F4`). A routing matrix where
  columns are MIDI inputs and rows are MIDI outputs; a `*` at an intersection
  (click or `Enter`) connects that input to that output. A special **Trk**
  input carries the sequencer's notes **and its MIDI clock** (24 PPQN +
  start/stop). Each output has a **channel-filter dropdown** (`All` / `None`
  / toggle individual channels) — placing channel marks keeps it open;
  clicking outside or `Esc` closes it. Hardware-input rows are live MIDI thru.
  The routing is saved with your preferences.
* **Save / load / export.** Projects are stored as a small, human-readable
  plain-text format (`.sng`); songs can also be exported to a Standard MIDI
  File (`.mid`). Use the **File** menu (Save / Save As / Open / Export MIDI)
  with a path dialog, the keys **Ctrl+S / Ctrl+O / Ctrl+E**, or the
  command-line flags `-load` and `-export`.
* **F1 help overlay** listing every hotkey; any key or click dismisses it.
* **Live editing while playing.** Edits to the model are picked up by the
  playback engine on the next tick, so you can build loops in real time.
  Set **Loop:Block** to repeat the edited block indefinitely for live
  looping.
* **MIDI punch-in with note length.** Arm record (`●` / `F5`) and play any
  connected controller. When **playing**, controller note-on writes a note at
  the playhead and the matching **note-off** writes a NOTE-OFF event — so the
  recorded note sustains exactly as long as you held the key; held notes spread
  across tracks (polyphonic), creating tracks as needed. When **stopped** it
  acts as a chord step recorder. A note sounds until a note-off (or retrigger).
* **Mouse support** throughout: click transport buttons, click a tracker
  cell to move the cursor, right-click a cell to clear it; in the piano roll,
  click a marker to toggle it, right-click to erase, and **drag to select** a
  region of markers. Click a block row's label to edit it in the tracker.

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

## Command line

```sh
sizzletracker                       # start empty
sizzletracker song.sng              # open a project (also: -load song.sng)
sizzletracker -load song.sng -export song.mid   # render to MIDI and exit
```

`-load FILE` opens a `.sng` project at startup; `-export FILE` renders the
loaded (or default) song to a Standard MIDI File and exits without launching
the UI.

## File format

A project (`.sng`) is line-oriented plain text — easy to read, diff, and
hand-edit:

```
version 1
bpm 120
sig 4 4

block A 16 4
roll ####
track T1 0
0 C-4 64 01
4 E-4 .. ..
8 OFF .. ..
endblock
```

Step lines are `<tick> <note> <vel> <chan>`: note as `C-4` (or `OFF`), velocity
in hex (`..` = default), channel 1-based (`..` = inherit). The `roll` line is
one character per beat (`#` = the block plays that beat).

## Config & crash recovery

Application state lives in the per-user config directory each OS expects
(`os.UserConfigDir` + `sizzletracker`):

| OS | Location |
|----|----------|
| Linux / BSD | `~/.config/sizzletracker` (or `$XDG_CONFIG_HOME`) |
| macOS | `~/Library/Application Support/sizzletracker` |
| Windows | `%AppData%\sizzletracker` |

It contains:

- **`config.json`** — preferences saved on exit: the selected MIDI out/in
  ports (reconnected by name next launch), the tracker/roll pane split, and
  the last project path.
- **`recovery.sng`** — the working song, **autosaved every 10 s and on exit**.
  On the next launch (when no file is given with `-load`) it is restored
  automatically, so unsaved edits survive a crash or an unclean exit. Use
  `Ctrl+S` to write your work to a real project file.

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
| `F4` | Toggle Edit / Patchbay view |
| `Ctrl+S` / `Ctrl+O` | Save / Open project (path dialog) |
| `Ctrl+E` | Export to MIDI (path dialog) |
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

Piano-roll focus (toolbar buttons: **Add Remove Cut Copy Paste**):

| Key | Action |
|-----|--------|
| Arrows | Move the cursor (row = block, column = beat) |
| `Shift`+Arrows | Extend the rectangular selection |
| `Enter` / `p` | Place block (paint a bar-length run of markers) |
| `.` | Toggle a single beat marker |
| `Del` / `Backspace` | Erase markers (cursor or selection) |
| `c` / `x` / `v` | Copy / cut / paste markers (paste top-left at cursor) |
| `a` | Add a block row below |
| `d` | Duplicate the current block |
| `D` | Remove the current block |

Time signature is chosen from a **dropdown** — click the `Sig:` field in the
top bar and pick 3/4, 4/4 or 5/4 (`Esc` closes it). Changing it rescales every
block to keep its bar count.

## Architecture

| File | Responsibility |
|------|----------------|
| `model.go` | Data model: `Song` → `Block` → `Track` → `Step`, the piano-roll `Roll` grid, and time-signature tick math. Guarded by `Song.mu`. |
| `midi.go` | PortMidi patchbay: opens all in/out ports, routes inputs→outputs through a matrix + per-output channel filters, fans the tracker source (notes+clock) out to connected outputs, and feeds the punch-in recorder. |
| `project.go` | Plain-text `.sng` save/load and Standard MIDI File export. |
| `config.go` | Per-user config dir, preferences (`config.json`) and crash-recovery autosave path. |
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
