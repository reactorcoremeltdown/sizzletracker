package main

import (
	"fmt"
	"strings"
	"sync"
)

// maxBlockNameLen caps a block's display name length (in characters).
const maxBlockNameLen = 16

// sanitizeBlockName trims a user-entered block name to printable ASCII and at
// most maxBlockNameLen characters.
func sanitizeBlockName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	n := 0
	for _, r := range s {
		if r < 0x20 || r >= 0x7f {
			continue
		}
		b.WriteRune(r)
		if n++; n >= maxBlockNameLen {
			break
		}
	}
	return b.String()
}

// Step value sentinels for the Note field.
const (
	NoteEmpty = -1 // nothing happens on this tick
	NoteOff   = -2 // explicit note-off (cuts whatever is sounding on the track)
)

// Sentinels for Vel / Chan meaning "inherit the track / global default".
const (
	ValEmpty = -1
)

// rollBeats is the initial width of a piano-roll lane, in beats (16 bars at
// 4 beats/bar). Lanes grow on demand as markers are placed further out, up to
// maxRollBeats.
const rollBeats = 64

// maxRollBeats caps the piano-roll timeline (1024 beats = 256 bars), bounding
// memory while being effectively unlimited for a song.
const maxRollBeats = 1024

// Step is a single cell for one track at one tick. A tracker row.
type Step struct {
	Note int // NoteEmpty, NoteOff, or 0..127
	Vel  int // ValEmpty or 0..127
	Chan int // ValEmpty or 0..15 (overrides the track's default channel)
}

func emptyStep() Step { return Step{Note: NoteEmpty, Vel: ValEmpty, Chan: ValEmpty} }

// Track is a vertical lane of steps inside a block.
type Track struct {
	Name    string
	Channel int // default MIDI channel 0..15
	Steps   []Step
}

func newTrack(name string, ch, length int) *Track {
	t := &Track{Name: name, Channel: ch, Steps: make([]Step, length)}
	for i := range t.Steps {
		t.Steps[i] = emptyStep()
	}
	return t
}

// resize grows or shrinks the step slice, preserving existing content.
func (t *Track) resize(n int) {
	if n == len(t.Steps) {
		return
	}
	old := t.Steps
	t.Steps = make([]Step, n)
	for i := range t.Steps {
		if i < len(old) {
			t.Steps[i] = old[i]
		} else {
			t.Steps[i] = emptyStep()
		}
	}
}

func (t *Track) clone() *Track {
	c := &Track{Name: t.Name, Channel: t.Channel, Steps: make([]Step, len(t.Steps))}
	copy(c.Steps, t.Steps)
	return c
}

// Block is a pattern: a fixed number of ticks across a set of tracks.
type Block struct {
	Name   string
	Length int // number of ticks (rows)
	Tracks []*Track
}

func newBlock(name string, length, numTracks int) *Block {
	b := &Block{Name: name, Length: length}
	for i := 0; i < numTracks; i++ {
		b.Tracks = append(b.Tracks, newTrack(fmt.Sprintf("T%d", i+1), i%16, length))
	}
	return b
}

func (b *Block) clone() *Block {
	c := &Block{Name: b.Name + "*", Length: b.Length}
	for _, t := range b.Tracks {
		c.Tracks = append(c.Tracks, t.clone())
	}
	return c
}

// maxBlockLen caps how long a single block may grow (defensive bound).
const maxBlockLen = 1024

func (b *Block) setLength(n int) {
	if n < 1 {
		n = 1
	}
	if n > maxBlockLen {
		n = maxBlockLen
	}
	b.Length = n
	for _, t := range b.Tracks {
		t.resize(n)
	}
}

func (b *Block) addTrack() {
	idx := len(b.Tracks)
	b.Tracks = append(b.Tracks, newTrack(fmt.Sprintf("T%d", idx+1), idx%16, b.Length))
}

func (b *Block) removeTrack(idx int) {
	if idx < 0 || idx >= len(b.Tracks) || len(b.Tracks) <= 1 {
		return
	}
	b.Tracks = append(b.Tracks[:idx], b.Tracks[idx+1:]...)
}

// TimeSig is a musical time signature. The app supports a fixed set.
type TimeSig struct {
	Num int
	Den int
}

// timeSigs are the three supported signatures, shown in the dropdown.
var timeSigs = []TimeSig{{3, 4}, {4, 4}, {5, 4}}

func (ts TimeSig) String() string { return fmt.Sprintf("%d/%d", ts.Num, ts.Den) }

// ticksPerBeat is the number of tracker lines that make up one beat — and the
// per-signature interlacing granularity: 3 lines/beat for 3/4, 4 for 4/4,
// 5 for 5/4.
func (ts TimeSig) ticksPerBeat() int { return ts.Num }

// beatsPerBar is fixed at 4, which yields the required ticks-per-bar of
// 12 (3/4), 16 (4/4) and 20 (5/4).
func (ts TimeSig) beatsPerBar() int { return 4 }

// ticksPerBar = ticksPerBeat * beatsPerBar = 12 / 16 / 20.
func (ts TimeSig) ticksPerBar() int { return ts.ticksPerBeat() * ts.beatsPerBar() }

// Song is the whole document. It is shared between the UI goroutine and the
// playback goroutine, so every access must hold mu.
//
// The arrangement is a piano roll: each Block is a row, and Roll[i] is a lane
// of beat-markers along the timeline. A marker means "this block plays during
// that beat"; a contiguous run of markers plays the block's pattern across it.
type Song struct {
	mu sync.Mutex

	BPM    float64
	Sig    TimeSig
	Blocks []*Block
	Roll   [][]bool // Roll[i] is the beat lane for Blocks[i]; grows on demand

	// Loop region, as an inclusive bar range, used when the transport is in
	// loop ("region") mode. Defaults to the first bar.
	LoopBar0 int
	LoopBar1 int
}

func newRollRow() []bool { return make([]bool, rollBeats) }

func blockName(n int) string { return string(rune('A' + n%26)) }

func newSong() *Song {
	s := &Song{BPM: 120, Sig: TimeSig{4, 4}}
	bar := s.ticksPerBar() // 16
	s.Blocks = []*Block{
		newBlock("A", bar, 4),
		newBlock("B", bar, 4),
	}
	s.Roll = [][]bool{newRollRow(), newRollRow()}
	// Demo: A plays the first bar (beats 0-3), B plays the second (beats 4-7).
	for i := 0; i < 4; i++ {
		s.Roll[0][i] = true
		s.Roll[1][4+i] = true
	}
	return s
}

// loopRegionTicks returns the [lo, hi) tick range of the loop region, snapped
// to whole bars. Falls back to the first bar for an invalid range.
func (s *Song) loopRegionTicks() (lo, hi int) {
	bpb := s.Sig.beatsPerBar()
	tpb := s.ticksPerBeat()
	b0, b1 := s.LoopBar0, s.LoopBar1
	if b0 < 0 || b1 < b0 {
		b0, b1 = 0, 0
	}
	return b0 * bpb * tpb, (b1 + 1) * bpb * tpb
}

// replaceWith adopts another song's contents in place (so existing *Song
// references held by the player stay valid). Caller holds s.mu.
func (s *Song) replaceWith(o *Song) {
	s.BPM = o.BPM
	s.Sig = o.Sig
	s.Blocks = o.Blocks
	s.Roll = o.Roll
	s.LoopBar0 = o.LoopBar0
	s.LoopBar1 = o.LoopBar1
}

func (s *Song) ticksPerBeat() int { return s.Sig.ticksPerBeat() }
func (s *Song) ticksPerBar() int  { return s.Sig.ticksPerBar() }

// secondsPerTick is the wall-clock duration of one tracker row.
func (s *Song) secondsPerTick() float64 {
	return 60.0 / s.BPM / float64(s.ticksPerBeat())
}

// blockBeats is how many beats Block i spans on the roll.
func (s *Song) blockBeats(i int) int {
	if i < 0 || i >= len(s.Blocks) {
		return 1
	}
	bb := s.Blocks[i].Length / s.ticksPerBeat()
	if bb < 1 {
		bb = 1
	}
	return bb
}

// totalBeats is the furthest marked beat + 1 (song length, no repeat).
func (s *Song) totalBeats() int {
	last := -1
	for _, row := range s.Roll {
		for b := len(row) - 1; b >= 0; b-- {
			if row[b] {
				if b > last {
					last = b
				}
				break
			}
		}
	}
	return last + 1
}

// totalTicks is the song length in ticks (one pass, no repeat).
func (s *Song) totalTicks() int { return s.totalBeats() * s.ticksPerBeat() }

// --- block list editing (kept in sync with Roll rows) ---

// addBlockAt inserts a fresh one-bar block immediately below index i and
// returns the new block's index.
func (s *Song) addBlockAt(i int) int {
	at := i + 1
	if at < 0 {
		at = 0
	}
	if at > len(s.Blocks) {
		at = len(s.Blocks)
	}
	b := newBlock(blockName(len(s.Blocks)), s.ticksPerBar(), 4)
	s.Blocks = append(s.Blocks, nil)
	copy(s.Blocks[at+1:], s.Blocks[at:])
	s.Blocks[at] = b

	s.Roll = append(s.Roll, nil)
	copy(s.Roll[at+1:], s.Roll[at:])
	s.Roll[at] = newRollRow()
	return at
}

// duplicateBlockAt clones block i (with its roll lane) just below it.
func (s *Song) duplicateBlockAt(i int) int {
	if i < 0 || i >= len(s.Blocks) {
		return i
	}
	at := i + 1
	clone := s.Blocks[i].clone()
	row := append([]bool(nil), s.Roll[i]...)

	s.Blocks = append(s.Blocks, nil)
	copy(s.Blocks[at+1:], s.Blocks[at:])
	s.Blocks[at] = clone

	s.Roll = append(s.Roll, nil)
	copy(s.Roll[at+1:], s.Roll[at:])
	s.Roll[at] = row
	return at
}

// removeBlockAt deletes block i and its roll lane.
func (s *Song) removeBlockAt(i int) {
	if i < 0 || i >= len(s.Blocks) || len(s.Blocks) <= 1 {
		return
	}
	s.Blocks = append(s.Blocks[:i], s.Blocks[i+1:]...)
	s.Roll = append(s.Roll[:i], s.Roll[i+1:]...)
}

// setSig changes the time signature, rescaling every block so it keeps the
// same number of bars under the new ticks-per-bar.
func (s *Song) setSig(ns TimeSig) {
	old := s.ticksPerBar()
	s.Sig = ns
	nbar := s.ticksPerBar()
	if old <= 0 || nbar <= 0 {
		return
	}
	for _, b := range s.Blocks {
		bars := (b.Length + old/2) / old
		if bars < 1 {
			bars = 1
		}
		b.setLength(bars * nbar)
	}
}

// --- roll marker helpers (caller holds s.mu) ---

func (s *Song) rollGet(row, beat int) bool {
	return row >= 0 && row < len(s.Roll) &&
		beat >= 0 && beat < len(s.Roll[row]) && s.Roll[row][beat]
}

func (s *Song) rollSet(row, beat int, v bool) {
	if row < 0 || row >= len(s.Roll) || beat < 0 || beat >= maxRollBeats {
		return
	}
	// Grow the lane on demand when marking a beat past its current end.
	if beat >= len(s.Roll[row]) {
		if !v {
			return
		}
		grown := make([]bool, beat+1)
		copy(grown, s.Roll[row])
		s.Roll[row] = grown
	}
	s.Roll[row][beat] = v
}

// rollPaint marks a whole block-length run of beats starting at start.
func (s *Song) rollPaint(row, start int) {
	if row < 0 || row >= len(s.Roll) {
		return
	}
	for k := 0; k < s.blockBeats(row); k++ {
		s.rollSet(row, start+k, true)
	}
}

// noteName renders a MIDI note (or sentinel) as a 3-char tracker cell.
func noteName(n int) string {
	switch n {
	case NoteEmpty:
		return "---"
	case NoteOff:
		return "==="
	}
	names := []string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-"}
	oct := n/12 - 1
	name := names[n%12]
	if oct < 0 {
		oct = 0
	}
	if oct > 9 {
		oct = 9
	}
	return fmt.Sprintf("%s%d", name, oct)
}

func velName(v int) string {
	if v == ValEmpty {
		return ".."
	}
	return fmt.Sprintf("%02X", v&0x7f)
}

func chanName(c int) string {
	if c == ValEmpty {
		return ".."
	}
	return fmt.Sprintf("%02d", c+1)
}
