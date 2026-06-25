# sizzletracker — user guide

A step-by-step guide to making music with sizzletracker, a terminal-based MIDI
tracker / step sequencer. No prior tracker experience is assumed.

**Contents**
1. [What sizzletracker is](#1-what-sizzletracker-is)
2. [Install and run](#2-install-and-run)
3. [The screen at a glance](#3-the-screen-at-a-glance)
4. [Tutorial: your first song](#4-tutorial-your-first-song)
5. [The tracker (top half)](#5-the-tracker-top-half)
6. [The piano roll / arrangement (bottom half)](#6-the-piano-roll--arrangement-bottom-half)
7. [Playback and looping](#7-playback-and-looping)
8. [Connecting MIDI gear: the patchbay](#8-connecting-midi-gear-the-patchbay)
9. [Recording live from a controller](#9-recording-live-from-a-controller)
10. [Settings](#10-settings)
11. [Saving, loading and exporting](#11-saving-loading-and-exporting)
12. [Command line](#12-command-line)
13. [Where your files live](#13-where-your-files-live)
14. [Troubleshooting](#14-troubleshooting)
15. [Keyboard cheat-sheet](#15-keyboard-cheat-sheet)

---

## 1. What sizzletracker is

sizzletracker arranges music as **blocks** (short patterns) placed along a
timeline. There are two editing surfaces, stacked vertically:

- The **tracker** (top) edits one block at a time: a grid where time runs
  downward and each track is a column of notes.
- The **piano roll** (bottom) arranges blocks into a song: each block is a row,
  the timeline runs left-to-right in beats, and you "paint" where each block
  plays.

It plays through **MIDI**: it does not make sound on its own. You route it to a
synth, a software instrument, or external hardware (see
[§8](#8-connecting-midi-gear-the-patchbay)). It also sends **MIDI clock**, so it
can drive other gear in time.

---

## 2. Install and run

### Prerequisites
- **Go 1.24+** and a C toolchain (cgo is used for MIDI).
- **PortMidi** installed on your system:
  - macOS: `brew install portmidi` (or `make deps-mac`)
  - Debian/Ubuntu: `sudo apt-get install build-essential libportmidi-dev pkg-config` (or `make deps-debian`)

### Build and start
```sh
make build      # produces ./sizzletracker
./sizzletracker
```
or simply `make run`. A terminal at least ~80×24 is recommended.

You can also open a project directly:
```sh
./sizzletracker mysong.sng
```

To leave the program at any time, press **F10**.

---

## 3. The screen at a glance

```
┌───────────────────────────────────────────────────────────┐
│ File  Edit Patchbay Settings   ▶ ■ ● » ⟲ ⚠   BPM Sig Step  │  top bar
├───────────────────────────────────────────────────────────┤
│ BLK A [1/2]  < >  len - 16 +  oct4 step1     +trk -trk      │  tracker controls
│ 000 | C-4 64 01   ...                                       │
│ 001 | --- .. ..   ...                                       │  tracker grid
│ ...                                                         │
├──────────────────────── (drag to resize) ──────────────────┤  separator
│ ROLL  Add Remove Cut Copy Paste                             │  roll toolbar
│ bar    1   2   3   4 ...                                    │  roll ruler
│ A    │ # # # # . . . .                                      │  block rows
│ B    │ . . . . # # # #                                      │
├───────────────────────────────────────────────────────────┤
│ status line — messages, hints                              │
└───────────────────────────────────────────────────────────┘
```

- **Top bar** — the `File` menu, the view tabs (`Edit` / `Patchbay` /
  `Settings`), transport buttons, and the `BPM`, time-signature (`Sig`) and
  punch-in `Step` controls. Everything here is clickable.
- **Transport glyphs**: `▶` play/stop · `■` stop · `●` record-arm · `»` MIDI
  thru · `⟲`/`⟳` loop mode · `⚠` panic (all notes off).
- **Tabs**: `Edit` is the tracker + piano roll; `Patchbay` is MIDI routing;
  `Settings` is preferences and the hotkey reference. Cycle them with **F4**.
- The bar between the tracker and the roll can be **dragged** to resize the two
  panes.

The mouse works throughout (click, drag, double-click, right-click), and so do
keyboard shortcuts. Press **F1** at any time for the in-app help overlay.

---

## 4. Tutorial: your first song

This walkthrough makes a two-block loop in a few minutes.

1. **Start the program.** You begin on the `Edit` tab with two empty blocks,
   `A` and `B`, in 4/4 at 120 BPM.

2. **Pick an output** so you can hear something. Press **F4** to open the
   `Patchbay`. You'll see a grid of inputs (columns) against outputs (rows). The
   first column, `Trk`, is sizzletracker itself. Move the cursor with the arrow
   keys to the cell where `Trk` meets your synth/output and press **Enter** to
   connect it (a `*` appears). Press **F4** twice more to return to `Edit`.
   *(If a connection to your default output already exists, you can skip this.)*

3. **Enter a few notes.** The tracker shows block `A`. The cursor is on the
   first row's note column. Tracker keys are laid out like a piano:
   - bottom row `z s x d c v g b h n j m` = C, C#, D … up a octave,
   - top row `q 2 w 3 e r …` = the octave above.

   Press **`z`** to write `C-4`, move down with **↓**, press **`c`** for `E-4`,
   down again, **`b`** for `G-4`. You've made a little arpeggio.

4. **Hear the block.** Press **Space** to play. The playhead scrolls through the
   block. Press **Space** again to stop.

5. **Arrange it.** Press **Tab** to move focus to the piano roll. The row for
   block `A` is highlighted. **Double-click** the first few beat cells (or move
   the cursor with arrows and press **`.`**) to mark where `A` should play. A
   contiguous run of marks plays the block's pattern across those beats.

6. **Add the second block.** Click block `B`'s row (or press **↓**), switch
   focus back to the tracker with **Tab**, enter a different pattern, then mark
   `B` on later beats in the roll.

7. **Loop while you work.** Select a range of beats in the roll by
   **shift-clicking** or dragging, then press **`l`** to mark those bars as the
   loop region. Press **F6** to switch the transport to *loop* mode and **Space**
   to play — it repeats the marked bars.

8. **Name the blocks.** Press **Ctrl+R** (or double-click a block's name) to
   rename the current block, e.g. "Intro". Names can be up to 16 characters.

9. **Save.** Press **Ctrl+S**, type a path ending in `.sng`, and press
   **Enter**.

That's the whole loop: write patterns in the tracker, arrange them in the roll,
route MIDI in the patchbay, and play.

---

## 5. The tracker (top half)

The tracker edits the **currently selected block**. Time runs top-to-bottom in
*ticks* (rows); each track is three columns: **note**, **velocity**, **channel**.

### Moving around
- **Arrow keys** move the cursor. Left/Right cross between the columns of a track
  and on to the next track.
- **Home / End** jump to the top / bottom of the block.
- **PgUp / PgDn** move by a beat.

### Entering notes
- **Note keys** `z..m` (lower octave) and `q..i` (upper octave) write a note in
  the note column and advance the cursor.
- **`-` / `=`** lower / raise the base octave (shown as `octN` in the controls).
- **Velocity / channel** columns accept digits.
- **`` ` ``** (backtick) writes an explicit **note-off** (cuts the sounding note
  on that track).
- **`.`** clears the current cell; **Backspace** clears and steps back.

### Step skip (punch-in spacing)
The **Step** dropdown in the top bar sets how far the cursor advances after each
note (1 = every row, 2 = every other row, …). Useful for laying down notes on a
coarser grid.

### Selecting, cutting, copying, pasting
- **Shift+arrows** or **drag** select a rectangle of cells (across tracks and
  ticks). The cursor corner is the top-left of a paste.
- **Ctrl+C / Ctrl+X / Ctrl+V** copy / cut / paste; **Delete** clears the
  selection. Note, velocity and channel are pasted into their respective
  columns.

### Block controls (the line above the grid)
- **`<` / `>`** (or **`[` / `]`**) switch to the previous / next block.
- **len `-` / `+`** halve / double the block length; click the number to type an
  exact length.
- **`+trk` / `-trk`** add or delete a track.
- **Ctrl+R** (or double-click the `BLK <name>` label) renames the block.

---

## 6. The piano roll / arrangement (bottom half)

The roll turns blocks into a song. **Each row is a block; each column is a beat**
along the timeline. A marked cell means "this block plays during that beat". A
*contiguous run* of marks plays the block's pattern from its start, repeating to
fill the run (and restarting after any gap).

### Placing blocks
- **Double-click** a cell toggles a single **beat**.
- **Right-click** at the start of a bar toggles the **whole bar**.
- With the keyboard: move the cursor and press **`.`** (toggle beat) or
  **Enter** (place the block across its length).
- **Delete** erases; **c / x / v** copy / cut / paste a rectangle of markers.

### Managing block rows
- The **Add** / **Remove** toolbar buttons (keys **`a`** / **`D`**) insert or
  delete a block row.
- Click a block's **name** to select it for editing; **double-click** the name
  to rename it.

### Marking a loop region
- Select a range of beats (drag or shift-click), then press **`l`**. The
  selection snaps out to whole bars and those bar numbers turn **red**.
- This region is what *loop* mode repeats (see next section).

### Resizing the panes
Drag the separator bar between the tracker and the roll to give either one more
room. The split is remembered between sessions.

---

## 7. Playback and looping

- **Space** starts/stops playback from the beginning of the timeline.
- **F8** (or the `⚠` button) is **panic**: an immediate all-notes-off if a note
  ever hangs.
- **F7** toggles **follow** — whether the tracker scrolls to keep the playhead
  in view.

There are two transport modes, toggled with **F6** (or the `⟲`/`⟳` button):

- **Song mode** — plays the arrangement once, from the start to the last marked
  beat, then stops.
- **Loop mode** — repeats the marked **loop region** (the red bars) forever.

Switching modes **while playing** takes effect at the next bar boundary, so the
current block always finishes cleanly instead of jumping. The button shows the
*target* mode immediately even if the change is still pending.

The tempo (**BPM**) and time signature (**Sig**, one of 3/4, 4/4, 5/4) are set
from the top bar — click them, or press **F9** to edit BPM. Changing the
signature rescales existing blocks so they keep their musical length.

---

## 8. Connecting MIDI gear: the patchbay

Open it with **F4** (the `Patchbay` tab). It is a routing matrix:

- **Columns are MIDI inputs.** The first, `Trk`, is sizzletracker's own output
  (its notes *and* its MIDI clock). The rest are your connected input devices.
- **Rows are MIDI outputs** (synths, hardware, virtual ports like IAC).
- A **`*`** at an intersection means "route this input to this output".

### Using it
- Move the cursor with the **arrow keys**; **Enter**, **`*`**, or a **click**
  toggles a connection.
- Each output row has a **channel filter** button `[all]`. Click it (or press
  **`c`**) to open a dropdown where you choose **All / None / individual
  channels** that may pass to that output. The dropdown stays open while you
  toggle channels; click outside or press **Esc** to close it.

### Hot-plugging devices
While the patchbay is open, sizzletracker **rescans automatically** so newly
connected or disconnected gear appears without a restart. You can force a rescan
any time with the **Rescan** button or the **`r`** key. Your routing and channel
filters are preserved **by device name**, so reconnecting a controller restores
its patches.

> Routing `Trk` to an output is what makes the sequencer audible, and also what
> sends MIDI clock / Start / Stop to that output.

---

## 9. Recording live from a controller

You can punch notes in from a connected MIDI controller while the transport
plays. Two independent toggles control how incoming notes are treated — the
"MIDI latch":

- **Record** — `●` button, **F5**: write incoming notes into the tracker at the
  playhead.
- **Thru** — `»` button, **Ctrl+T**: forward incoming notes to the patched
  outputs (so you can hear the controller).

Together they form four modes, shown on the toolbar and the Settings tab:

| Record | Thru | Mode | Meaning |
|---|---|---|---|
| off | on | **Playback** (default) | monitor the controller, don't record |
| on | off | **Record** | record only, no monitoring |
| on | on | **Both** | record and monitor |
| off | off | **Off** | ignore notes |

### How to record
1. In the patchbay, route your controller's input column to an output (for
   monitoring) — `Trk` need not be involved for thru.
2. Press **F5** to arm **Record**.
3. Press **Space** to play, and perform.

Recording is **polyphonic**: each held note takes its own track, chords overflow
to free tracks, and new tracks are created as needed. Note-on and note-off are
both captured, so held notes get the right length. The incoming MIDI **channel**
is preserved (defaulting to channel 1 if none is present), and **CC / Program
Change** messages pass through to routed outputs.

---

## 10. Settings

The `Settings` tab (**F4** to reach it) has three sections:

- **Project** — the *default save folder* used to pre-fill the path in
  Save / Export dialogs. Click the field to set it.
- **MIDI input latch** — the **Record** and **Thru** toggles described above,
  plus the resulting mode name.
- **Hotkeys** — a scrollable reference of every shortcut (scroll with the arrow
  keys / PgUp / PgDn).

Settings persist between sessions.

---

## 11. Saving, loading and exporting

From the **File** menu, or with shortcuts:

- **Ctrl+S** — Save (asks for a path the first time, then saves in place).
- **Ctrl+O** — Open a project.
- **Ctrl+E** — Export to a Standard MIDI File (`.mid`).

Projects are saved as **`.sng`** — a small, human-readable, line-oriented text
format you can diff and hand-edit. MIDI export renders the arrangement to a
type-0 SMF for use in any DAW.

In any dialog, type the path and press **Enter** (or **Esc** to cancel).

---

## 12. Command line

```
sizzletracker [flags] [project.sng]

  -load <file>     open a project at startup (same as the positional argument)
  -export <file>   render the loaded (or default) song to a MIDI file and exit
```

Examples:
```sh
sizzletracker song.sng                 # open a project
sizzletracker -load song.sng -export song.mid   # headless: convert .sng -> .mid
```

The `-export` form never launches the UI, which makes it handy for scripting /
batch conversion.

---

## 13. Where your files live

sizzletracker stores preferences and a crash-recovery copy of your work in the
per-user config directory for your OS:

- **Linux/BSD**: `~/.config/sizzletracker/`
- **macOS**: `~/Library/Application Support/sizzletracker/`
- **Windows**: `%AppData%\sizzletracker\`

Inside it:
- `config.json` — preferences (MIDI routing, pane split, default save folder,
  last file, latch state).
- `recovery.sng` — an autosaved copy of the working song. It is written
  periodically and on exit, so if the program ever crashes you can recover your
  latest edits the next time you start.

---

## 14. Troubleshooting

**No sound / no outputs in the patchbay.**
Make sure PortMidi is installed and at least one MIDI output exists. On macOS you
can create a virtual port with *Audio MIDI Setup → MIDI Studio → IAC Driver*
(enable the device), then route a software synth to it. Then route `Trk` to that
output in the patchbay.

**A newly plugged controller doesn't appear.**
Open the patchbay (it auto-rescans), or press **`r`** / the **Rescan** button.

**A note is stuck on.**
Press **F8** (panic) to send all-notes-off to every output.

**Glyphs look wrong / boxes instead of symbols.**
Use a UTF-8 terminal with a font that has the transport glyphs. The help text
itself is plain ASCII to stay readable everywhere.

**"Terminal too small".**
Enlarge the window; the UI needs roughly 40×8 minimum and is comfortable at
80×24+.

---

## 15. Keyboard cheat-sheet

### Global
| Key | Action |
|---|---|
| Space | Play / stop |
| Tab | Switch focus: tracker ↔ piano roll |
| F1 | Help overlay |
| F2 / F3 | Focus tracker / piano roll |
| F4 | Cycle views: Edit → Patchbay → Settings |
| F5 | Toggle record-arm |
| Ctrl+T | Toggle MIDI note thru |
| F6 | Toggle loop mode (song ↔ region) |
| F7 | Toggle follow-playhead |
| F8 | Panic (all notes off) |
| F9 | Edit BPM |
| F10 | Quit |
| Ctrl+R | Rename current block |
| Ctrl+S / Ctrl+O / Ctrl+E | Save / Open / Export MIDI |

### Tracker
| Key | Action |
|---|---|
| Arrows | Move cursor (L/R cross columns & tracks) |
| Home / End | Top / bottom of block |
| PgUp / PgDn | Move by a beat |
| `z`..`m`, `q`..`i` | Enter notes (two octaves) |
| `-` / `=` | Lower / raise octave |
| `` ` `` | Note-off |
| `.` / Backspace | Clear cell / clear and step back |
| `[` `]` or `<` `>` | Previous / next block |
| Shift+arrows or drag | Select a region |
| Ctrl+C / X / V | Copy / cut / paste (cursor = top-left) |
| Delete | Clear selection |

### Piano roll
| Key | Action |
|---|---|
| Arrows | Move cursor |
| Double-click | Toggle a beat |
| Right-click | Toggle a whole bar |
| Enter | Place block across its length |
| `.` | Toggle beat |
| Delete | Erase |
| `c` / `x` / `v` | Copy / cut / paste markers |
| `l` | Mark selected bars as the loop region |
| `a` / `D` | Add / remove a block row |
| Shift+arrows or drag | Select a region |

### Patchbay
| Key | Action |
|---|---|
| Arrows | Move cursor |
| Enter / `*` / click | Toggle a connection |
| `c` | Open the channel-filter dropdown |
| `r` | Rescan MIDI devices |
| Esc | Close a dropdown |
