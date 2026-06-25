# sizzletracker — developer guide (handover)

This document is written for someone who has never seen the source before and
needs to understand, maintain, and extend it. It explains the architecture, the
data model, the concurrency design, the file formats, and how to make the most
common kinds of changes safely.

Read it alongside the code: file references look like
[`player.go`](../player.go) and are accurate as of this writing.

**Contents**
1. [Mental model](#1-mental-model)
2. [Repository layout](#2-repository-layout)
3. [The data model](#3-the-data-model)
4. [Time and the tick system](#4-time-and-the-tick-system)
5. [Concurrency model](#5-concurrency-model)
6. [The main loop and rendering](#6-the-main-loop-and-rendering)
7. [Input handling](#7-input-handling)
8. [The player (timing and note lifecycle)](#8-the-player-timing-and-note-lifecycle)
9. [The MIDI engine (patchbay)](#9-the-midi-engine-patchbay)
10. [Persistence and file formats](#10-persistence-and-file-formats)
11. [Build, cgo, and PortMidi](#11-build-cgo-and-portmidi)
12. [Testing](#12-testing)
13. [How to extend it](#13-how-to-extend-it)
14. [Invariants and gotchas](#14-invariants-and-gotchas)

---

## 1. Mental model

sizzletracker is a single Go package (`package main`) plus one vendored
sub-package (`internal/portmidi`). It is a TUI (terminal UI) built on
[tcell](https://github.com/gdamore/tcell), and it produces music by emitting
MIDI through [PortMidi](https://github.com/PortMidi/portmidi) (via cgo).

There are three long-lived actors:

- **The UI goroutine** (the "main loop") — owns all editor/interaction state,
  reads input, and draws frames. It is the *only* place editor state is touched.
- **The player** — two goroutines (a tick loop and a MIDI-clock loop) that read
  the shared document and emit MIDI in time.
- **The MIDI engine** — opens MIDI ports, runs one listener goroutine per input
  device, and routes/forwards messages according to a patchbay matrix.

The shared, mutable document is the `Song`. Everything that both the UI and the
player touch goes through `Song.mu`. Editor-only state lives in `Editor` and is
never locked because only the UI goroutine sees it.

```
                 ┌─────────────┐      reads/writes (Song.mu)
   keyboard ───► │  UI / main  │ ◄──────────────────────────┐
   mouse    ───► │   loop      │                             │
                 │  (Editor)   │ ─ snapshot() ─► frame ─► draw()
                 └──────┬──────┘                             │
                        │ commands                           │
                        ▼                                    │
                 ┌─────────────┐   reads (Song.mu)     ┌─────┴─────┐
                 │   Player    │ ────────────────────► │   Song    │
                 │ run + clock │                       │ (document)│
                 └──────┬──────┘                       └───────────┘
                        │ noteOn/Off, clock (sendMu)
                        ▼
                 ┌─────────────┐  per-input listener goroutines
   MIDI in  ───► │ MidiEngine  │ ──► onIn callback ──► app.midiIn ──► UI
   MIDI out ◄─── │ (patchbay)  │
                 └─────────────┘
```

---

## 2. Repository layout

Top-level, every `*.go` file is `package main`:

| File | Responsibility |
|---|---|
| [`main.go`](../main.go) | Program entry, `App` struct, startup wiring, the event/render loop, autosave, and the MIDI-rescan coordination. |
| [`model.go`](../model.go) | The document: `Song`, `Block`, `Track`, `Step`, `TimeSig`; the piano-roll grid; tick/beat math; block/roll editing helpers. All guarded by `Song.mu`. |
| [`player.go`](../player.go) | The transport: timing goroutines, loop modes, and the note on/off lifecycle (`stepRoll` → `playAt` → `applyStep`). |
| [`midi.go`](../midi.go) | The MIDI engine / patchbay: device discovery, opening streams, the routing matrix + channel filters, thru forwarding, MIDI clock, live rescan, and patch persistence. |
| [`editor.go`](../editor.go) | UI/interaction *state* only: the `Editor` struct, enums for views/focus/dialogs/region-actions, and small selection-rectangle helpers. No behaviour. |
| [`ui.go`](../ui.go) | All drawing: `snapshot()` → `frame`, `draw()`, and the per-area renderers (top bar, tracker, piano roll, patchbay, settings, dialogs, help). Defines clickable `Region`s. |
| [`input.go`](../input.go) | All input handling: keyboard dispatch by focus, mouse hit-testing against regions, dialogs/dropdowns, and the editing commands they invoke. |
| [`config.go`](../config.go) | Per-user config directory, `config.json` load/save, recovery-file path. |
| [`project.go`](../project.go) | The `.sng` text format (encode/decode) and Standard MIDI File export. |
| `internal/portmidi/` | Vendored PortMidi cgo bindings (Apache-2.0). Only change from upstream is platform-aware build tags. |
| `*_test.go` | Unit tests (see [§12](#12-testing)). |

There is intentionally **no behaviour in `editor.go`** and **no state in
`input.go`/`ui.go`** beyond locals — state lives in `Editor`/`Song`/`Player`,
drawing in `ui.go`, behaviour in `input.go`. Keeping that separation makes the
code navigable.

---

## 3. The data model

All in [`model.go`](../model.go).

```
Song
├── BPM, Sig (TimeSig)
├── Blocks []*Block
│     └── Block{ Name, Length, Tracks []*Track }
│            └── Track{ Name, Channel, Steps []Step }
│                   └── Step{ Note, Vel, Chan }
├── Roll [][]bool          // Roll[i] is the beat lane for Blocks[i]
└── LoopBar0, LoopBar1     // inclusive bar range for loop mode
```

- **`Step`** is one tracker cell. Its fields use sentinels:
  - `Note`: `NoteEmpty` (-1, sustain/nothing), `NoteOff` (-2, cut the sounding
    note), or a MIDI note `0..127`.
  - `Vel`, `Chan`: `ValEmpty` (-1) means "inherit" — the track's default channel
    and a default velocity of 100 are used at playback.
- **`Track`** is one column/lane; monophonic (one sounding note at a time).
- **`Block`** is a pattern: a fixed number of ticks (`Length`) across its
  tracks. `Name` is shown in the tracker title and roll gutter, up to
  `maxBlockNameLen` (16) chars (`sanitizeBlockName`). `Length` is bounded by
  `maxBlockLen` (1024).
- **`Roll`** is the arrangement. `Roll[i]` is a `[]bool` lane of beat-markers for
  block `i`. A `true` at beat `b` means block `i` plays during beat `b`. Lanes
  start `rollBeats` (64) wide and grow on demand up to `maxRollBeats` (1024).
  `rollSet`/`rollGet` grow and read the lane.

The default new document (`newSong`) has two 1-bar blocks (`A`, `B`) in 4/4 at
120 BPM, with the first bar of `A` marked.

Important: **the `Song` is the single source of truth shared with the player.**
Every read or write must hold `Song.mu`. The renderer copies what it needs into
an immutable `frame` once per draw so it never holds the lock during drawing.

---

## 4. Time and the tick system

This trips people up, so it is worth being precise. Defined on `TimeSig` in
[`model.go`](../model.go):

- Supported signatures: **3/4, 4/4, 5/4** (`timeSigs`).
- `ticksPerBeat()` = the **numerator** → 3, 4, or 5 tracker rows per beat.
- `beatsPerBar()` = **fixed 4**.
- `ticksPerBar()` = `ticksPerBeat * 4` → **12, 16, or 20**.

So a "tick" is one tracker row, and the number of ticks in a beat is the time
signature's numerator. This is why a 4/4 block defaults to 16 rows (4 beats ×
4). Changing the signature (`setSig`) rescales every block so musical lengths are
preserved.

The piano roll is indexed in **beats**, not ticks. A block occupies
`blockBeats(i)` = `Length / ticksPerBeat` beats. The player converts between the
global roll tick and per-block local ticks in `playAt` (see [§8](#8-the-player-timing-and-note-lifecycle)).

`secondsPerTick()` derives the wall-clock tick duration from BPM and
`ticksPerBeat`; the player uses it to schedule.

---

## 5. Concurrency model

This is the most safety-critical part. There are four mutexes / sync points:

| Lock | Owner | Guards |
|---|---|---|
| `Song.mu` | document | everything in `Song` (blocks, tracks, steps, roll, BPM, sig, loop range) |
| `Player.mu` | player | transport state (`playing`, tick counters, `held`, loop mode) |
| `MidiEngine.mu` | engine | device lists, stream slices, routing matrix, filters, `noThru`, callback |
| `MidiEngine.sendMu` | engine | serializes **writes** to output streams (PortMidi streams are not safe for concurrent writes) |
| `MidiEngine.inWG` | engine | a `WaitGroup` tracking live input-listener goroutines |

### Rules that keep it correct

1. **MIDI I/O never happens while holding `Song.mu`.** The player collects the
   events to emit into a `[]midiOp` *under* the locks, releases them, *then*
   sends. See `Player.run` in [`player.go`](../player.go). This prevents a
   blocking MIDI write from stalling the UI (which also needs `Song.mu`).

2. **Editor state is single-threaded.** `Editor` (in [`editor.go`](../editor.go))
   is touched only by the UI goroutine. Incoming MIDI that must affect editor
   state is marshalled over the `app.midiIn` channel and applied in the UI loop
   (`applyPunch`), never directly from a listener goroutine.

3. **Lock ordering for sends.** `sendTo` takes `sendMu` then `mu`. `rescan` must
   respect this. It also must *not* hold `sendMu` while waiting for listeners
   (a listener may be blocked in `forwardFrom`→`sendTo` waiting for `sendMu`,
   which would deadlock `inWG.Wait`). Hence `rescan` stops listeners **first**
   (no `sendMu`), then takes `sendMu` for the stream swap. This ordering is
   load-bearing — see the comments in `rescan`/`stopInputs`.

4. **Snapshots for rendering.** `snapshot()` ([`ui.go`](../ui.go)) copies the
   document into a `frame` under `Song.mu` (deep-copying the roll lanes and the
   edited block's steps), then drawing happens lock-free against that immutable
   `frame`.

The whole thing is regularly checked with `go test -race`, and a live
`-race` smoke run exercises rescans against real PortMidi. Keep it that way:
if you add cross-goroutine state, add a lock and a race test.

---

## 6. The main loop and rendering

### Startup (`run` in [`main.go`](../main.go))
Parses flags, resolves the starting song (explicit `-load` > crash recovery >
fresh), handles headless `-export`, then initialises tcell, the `MidiEngine`,
the `Player`, and the `Editor`, applies saved config, wires the MIDI input
callback to the `app.midiIn` channel, starts the tcell event-pump goroutine and
the device-rescan poller, and enters `app.loop()`.

### The loop (`App.loop`)
A `select` over:
- `a.events` — tcell key/mouse/resize events (`processEvent`);
- `ticker.C` — a `frameInterval` (33 ms ≈ 30 fps) tick so the playhead animates
  while idle;
- `a.midiIn` — incoming MIDI notes (`applyPunch`);
- `a.rescanTick` / `a.rescanDone` — the device-rescan signals.

After handling one event it **drains** any queued events (bounded) so a burst
collapses into a single redraw, calls `a.draw()`, and periodically autosaves the
recovery file (`autosaveEvery`, 10 s).

### Drawing (`draw` in [`ui.go`](../ui.go))
Each frame: clear the screen, reset the region list, build a `frame` via
`snapshot()`, draw the top bar, then dispatch on `Editor.view`
(`drawTracker`+`drawPianoRoll`, `drawPatchbay`, or `drawSettings`), then any
overlays (dropdowns, dialogs, help).

### Clickable regions
The UI is mouse-driven via a simple immediate-mode pattern. While drawing, each
interactive element appends a `Region{x,y,w,h, action, data1..3}` to
`Editor.regions`. On a click, `hitTest` walks the list **in reverse** (so the
topmost/last-drawn region wins) and returns the `Region`; `input.go` switches on
its `action`. To make something clickable, draw it and `addRegion` — that's all.

The `frame`/`blockView`/`trackView` structs ([`ui.go`](../ui.go)) are the
read-only copies the renderer consumes; they exist so drawing never touches the
live `Song`.

---

## 7. Input handling

All in [`input.go`](../input.go).

- **`handleKey`** dispatches by precedence: help overlay → Esc-closes-dropdowns
  → modal dialog (`FocusDialog`) → global keys (suspended during text-field
  entry) → then by `Editor.focus` (`FocusTracker`, `FocusArrange`, `FocusBPM`,
  `FocusLen`) and `Editor.view`.
- **`handleMouse`** decodes tcell's raw button snapshots into press/drag/release
  transitions, manages drag-select state for the roll, tracker, and the
  pane separator, tracks double-clicks (`dblClickWindow`, 400 ms), then
  hit-tests and switches on the region action.
- **Focus** (`Editor.focus`) decides which pane gets keys; **view**
  (`Editor.view`) decides which screen is shown. They are independent.
- **Dialogs** are modal text inputs (`openDialog`/`handleDialogKey`/
  `executeDialog`) reused for Save / Open / Export / default-folder / rename.
  The `DialogAction` enum selects what `executeDialog` does with the typed text.
- **Dropdowns** (time-sig, step, file menu, channel filter) are semi-modal: they
  set a `show*` flag and are dismissed by a click outside or Esc.

---

## 8. The player (timing and note lifecycle)

All in [`player.go`](../player.go). Two goroutines start on `start()`:

- **`run`** — the tick loop. Each iteration: lock `Song.mu`+`Player.mu`, call
  `stepRoll` to compute the events for this tick and the time until the next,
  unlock, emit the events, sleep. If `stepRoll` reports `done`, the transport
  stops.
- **`clock`** — emits **MIDI clock at 24 PPQN** plus Start/Stop to whatever the
  `Trk` source is patched to. It runs separately because 24 PPQN doesn't divide
  evenly into the per-signature tick rate.

### How a tick is played
- **`stepRoll`** decides timeline behaviour:
  - applies a **deferred loop-mode change** at a bar boundary (so the current
    block finishes before switching — see below);
  - in **`LoopRegion`** mode, clamps the global tick into the marked region
    (`loopRegionTicks`) and wraps it;
  - in **`LoopSong`** mode, plays from tick 0 to the last marked beat
    (`totalTicks`) then returns `done=true`.
- **`playAt`** emits everything sounding at a global roll tick: for each block,
  if its lane is marked at that beat, it finds the **start of the contiguous
  run** of marks, computes the **local tick** within the block (so the pattern
  plays from its beginning and repeats/restarts correctly), and applies each
  track's step. Blocks not marked at that beat get their held notes released.
- **`applyStep`** turns one `Step` into note events with mono-per-track
  bookkeeping (`held`): a new note cuts the previous one on that track; `NoteOff`
  cuts it; `NoteEmpty` sustains. Velocity/channel sentinels resolve to defaults
  here.

### Deferred mode switching
Toggling the loop mode while playing sets `pendingMode`/`hasPending`; `stepRoll`
applies it only at a bar boundary. The UI shows the *target* mode immediately
(`effectiveLoopLocked`) so the button feels responsive without the audio
jumping.

### Live punch-in
`applyPunch` (in [`input.go`](../input.go), called from the UI loop) decides what
to do with incoming MIDI from the two toggles `armed` (Rec) and `thru` (Latch).
Forwarding to outputs ("play") is handled separately by the MIDI engine (thru);
`applyPunch` only decides whether/where to *record*:

| Rec (`armed`) | Latch (`thru`) | Behaviour |
|---|---|---|
| on | on | record at the playhead; view follows |
| on | off | record at the playhead; view follows |
| off | on | **record nothing** (play-only; early return) |
| off | off | record at the **edit cursor** (punch-in, no follow) |

So the write position uses `atPlayhead = armed && playing`, and the one case that
records nothing is `!armed && thru`. The tracker view follows the playhead only
while `armed` (`computeTickScroll` gates follow on it). Recording is polyphonic:
each held input note is tracked (`Editor.punch`) and assigned a track,
overflowing to new tracks for chords. `latchMode()` ([`editor.go`](../editor.go))
names the four states Both / Record / Playback / Punch-in.

---

## 9. The MIDI engine (patchbay)

All in [`midi.go`](../midi.go).

### Topology
- A list of MIDI `ins` and `outs` (`portDevice` = id + name).
- The patchbay exposes **inputs as `len(ins)+1`**: index 0 is the special
  **`Tracker`** source (the sequencer's notes + clock); index `i>=1` maps to
  `ins[i-1]`.
- `routes[in][out]` is the connection matrix; `filter[out][ch]` is a per-output
  16-channel pass filter (default all-on); `clock[out]` is a per-output flag for
  whether MIDI clock / Start / Stop is sent there (default on).

### Forwarding and sending
- Each input device has a **listener goroutine** (`listen`) that reads events,
  feeds the recorder callback (`onIn`, for punch-in), and forwards to routed
  outputs via `forwardFrom` (channel-filtered).
- `noThru` (set by `setNoteThru`) gates **note** forwarding for the latch modes;
  CC/PC and the Tracker source are unaffected.
- The Tracker source sends notes via `noteOn`/`noteOff` (to `trackerOuts`,
  channel-filtered) and clock/start/stop via `trackerRealtime` (to `clockOuts`,
  the routed outputs whose `clock[out]` is on). All output writes go through
  **`sendTo`**, serialized by `sendMu`.

### Live rescan (hot-plug)
PortMidi only sees devices present at `Initialize`. To pick up
connected/disconnected gear, `rescan()` tears down (stop listeners → close
streams) and re-enumerates (`Terminate` + `Initialize` + `openDevices`),
**preserving routing, filters and clock state by device name**
(`exportPatch`/`applyPatch`).
See [§5](#5-concurrency-model) for the lock ordering it relies on. The UI calls
it from a goroutine via `App.tryRescan` (automatic while the patchbay is open and
the transport is stopped; manual with the Rescan button / `r`).

### Persistence
`exportPatch` returns routes as `"input>>output"` name pairs, the non-default
channel filters, and the names of outputs with clock disabled; `applyPatch`
restores all three by name. This is what survives a rescan and what is written
to `config.json`.

---

## 10. Persistence and file formats

### Projects — `.sng` (text), in [`project.go`](../project.go)
A small, line-oriented, human-readable format (so it diffs well and can be
hand-edited). Header lines (`version`, `bpm`, `sig`) followed by per-block
sections:

```
block <name...> <length> <numTracks>   # name may contain spaces
roll  ####....                          # one char per beat: '#'=marker '.'=empty
track <name> <channel0based>
<tick> <note> <vel> <chan>              # e.g. "0 C-4 64 01"; ".." = default
endblock
```

Notes use tracker tokens (`C-4` = MIDI 60) via `noteToken`/`parseNote`, with a
raw-integer fallback below `C-0` so every value round-trips exactly. Block names
may contain spaces: the parser reads the two trailing integers from the right
and joins the middle as the name (backward-compatible with single-word names).
`encodeProject`/`decodeProject` are the entry points.

### MIDI export — Standard MIDI File, in [`project.go`](../project.go)
`encodeMIDI` renders the arrangement to a **type-0 SMF** at `midiPPQ` = **480**
ticks/quarter (divisible by every supported signature). `writeMIDIFile` writes
it; the CLI `-export` flag uses it headlessly.

### Preferences and recovery — in [`config.go`](../config.go)
- `config.json` in `os.UserConfigDir()/sizzletracker` holds the `Config` struct
  (pane split, last path, default save folder, latch `NoThru`, patchbay routes,
  channel filters, and the `ClockOff` set of outputs with clock disabled).
- `recovery.sng` in the same directory is an autosave of the working song,
  written every 10 s and on exit. On startup, if no project was given, it is
  restored so a crash loses little.

---

## 11. Build, cgo, and PortMidi

- `make build` → `CGO_ENABLED=1 go build -o sizzletracker .`. cgo is required
  because PortMidi is a C library.
- The vendored [`internal/portmidi`](../internal/portmidi) sets platform-aware
  cgo directives (Homebrew `/opt/homebrew` and `/usr/local` on macOS; system
  paths on Linux). Override with `CGO_CFLAGS` / `CGO_LDFLAGS` for non-standard
  installs.
- Install PortMidi first: `make deps-mac` (Homebrew) or `make deps-debian`
  (apt). Targets are documented in the [`Makefile`](../Makefile).
- CI builds via `.github/workflows/build.yml`.

The only native dependency is PortMidi; everything else is pure Go.

---

## 12. Testing

Run `go test ./...` (add `-race` — the concurrency design depends on it). Tests
are plain Go unit tests, no hardware required:

| File | Covers |
|---|---|
| [`model_test.go`](../model_test.go) | tracker/roll editing, selections, polyphonic punch, tick math. |
| [`player_test.go`](../player_test.go) | loop-region ticks, deferred mode switch, song-stops-at-end, region looping. |
| [`midi_test.go`](../midi_test.go) | `classifyMidi`, routing/filters, patch export/apply, name-based preservation across device reorder, unavailable-engine no-op. Uses `fakeEngine` (named but unopened ports). |
| [`project_test.go`](../project_test.go) | `.sng` round-trip, note-token round-trip, block names with spaces, name sanitisation. |
| [`config_test.go`](../config_test.go) | config round-trip, latch-mode naming, note-thru, recovery round-trip. |
| [`ui_test.go`](../ui_test.go) | settings render (via a tcell `SimulationScreen`), roll gutter sizing, rename dialog flow. |
| [`layout_test.go`](../layout_test.go) | pane layout math. |

Two manual verification techniques are used during development and are worth
knowing: a tcell `SimulationScreen` to assert rendered cell content/colour
without a terminal, and a PTY smoke run under `-race` to exercise real PortMidi.

---

## 13. How to extend it

**Add a clickable control.** Draw it in the relevant `draw*` function and
`a.ed.addRegion(Region{... action: ActFoo, data1: ...})`. Add `ActFoo` to the
`RegionAction` enum in [`editor.go`](../editor.go) and a `case ActFoo:` in
`handleMouse` ([`input.go`](../input.go)).

**Add a keyboard shortcut.** Add a `case` in the appropriate place in
`handleKey` — global keys (works everywhere), or a per-focus handler
(`handlePatchKey`, the tracker/roll branches). Update the `helpLines` reference
in [`ui.go`](../ui.go) so it shows in Help and Settings.

**Add a persisted preference.** Add a field to `Config` in
[`config.go`](../config.go) (with a `json:"...,omitempty"` tag), set it in
`saveAppConfig` and read it at startup in `run` ([`main.go`](../main.go)). Extend
`TestConfigRoundTrip`.

**Add a new view/tab.** Add a value to the `View` enum, a tab button +
`ActTab*` region in `drawTopBar`, a `case` in `draw` dispatch and in
`toggleView`, and a `draw<View>` renderer.

**Touch document state.** Any new read/write of `Song` must hold `Song.mu`; any
new cross-goroutine field needs its own lock and a `-race` test. MIDI sends must
happen outside `Song.mu`.

**Change the document model.** Update `model.go`, then the three things that
serialize it: `encodeProject`/`decodeProject`, `encodeMIDI`, and `snapshot()`'s
`frame`. Add a round-trip test.

---

## 14. Invariants and gotchas

- **Never emit MIDI while holding `Song.mu`.** Collect ops, unlock, then send.
- **Only the UI goroutine touches `Editor`.** Route everything else through
  channels (`app.midiIn`, `app.rescanDone`).
- **`rescan` lock order is load-bearing**: stop listeners (no `sendMu`) → take
  `sendMu` → swap streams → re-enumerate. Reordering risks a deadlock against a
  listener blocked in `sendTo`.
- **A "tick" is a tracker row; ticks-per-beat = the time-signature numerator.**
  The roll is indexed in beats; conversions live in `playAt`.
- **Roll lanes grow lazily** to `maxRollBeats`; always go through
  `rollSet`/`rollGet`, never index `Roll[i]` past its length.
- **The renderer reads a `frame`, not the `Song`.** If you add document fields
  the UI must show, copy them into `frame` in `snapshot()`.
- **Regions are rebuilt every frame** and matched newest-first; overlapping
  controls resolve to the last one drawn.
- **PortMidi enumerates once at `Initialize`.** New hardware requires a `rescan`
  (it re-inits PortMidi); this is the only way to detect hot-plug with PortMidi.
- **Help/Settings text is ASCII on purpose** to avoid wide-glyph rendering
  problems across terminals; only the top-bar transport uses symbols.
