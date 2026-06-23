package main

import (
	"fmt"
	"sync"
)

// Step value sentinels for the Note field.
const (
	NoteEmpty = -1 // nothing happens on this tick
	NoteOff   = -2 // explicit note-off (cuts whatever is sounding on the track)
)

// Sentinels for Vel / Chan meaning "inherit the track / global default".
const (
	ValEmpty = -1
)

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

func (b *Block) setLength(n int) {
	if n < 1 {
		n = 1
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

// TimeSig is a musical time signature.
type TimeSig struct {
	Num int
	Den int
}

func (ts TimeSig) String() string { return fmt.Sprintf("%d/%d", ts.Num, ts.Den) }

// Song is the whole document. It is shared between the UI goroutine and the
// playback goroutine, so every access must hold mu.
type Song struct {
	mu sync.Mutex

	BPM          float64
	Sig          TimeSig
	TicksPerBeat int // tracker rows per beat (e.g. 4 == 16th notes in 4/4)

	Blocks      []*Block // the palette of available patterns
	Arrangement []int    // sequence of indices into Blocks (the song timeline)
}

func newSong() *Song {
	s := &Song{
		BPM:          120,
		Sig:          TimeSig{4, 4},
		TicksPerBeat: 4,
	}
	s.Blocks = []*Block{
		newBlock("A", 16, 4),
		newBlock("B", 16, 4),
	}
	s.Arrangement = []int{0, 1}
	return s
}

// ticksPerBar returns how many tracker rows make up one bar.
func (s *Song) ticksPerBar() int {
	return s.TicksPerBeat * s.Sig.Num
}

// secondsPerTick is the wall-clock duration of one tracker row.
func (s *Song) secondsPerTick() float64 {
	return 60.0 / s.BPM / float64(s.TicksPerBeat)
}

// addBlock creates a fresh block modelled on the dimensions of an existing one
// and returns its index.
func (s *Song) addBlock(length, numTracks int) int {
	name := string(rune('A' + len(s.Blocks)%26))
	s.Blocks = append(s.Blocks, newBlock(name, length, numTracks))
	return len(s.Blocks) - 1
}

// duplicateBlock copies block i and returns the new index.
func (s *Song) duplicateBlock(i int) int {
	if i < 0 || i >= len(s.Blocks) {
		return i
	}
	s.Blocks = append(s.Blocks, s.Blocks[i].clone())
	return len(s.Blocks) - 1
}

// removeBlock deletes block i and rewrites the arrangement so references stay
// valid (slots pointing at the removed block are dropped).
func (s *Song) removeBlock(i int) {
	if i < 0 || i >= len(s.Blocks) || len(s.Blocks) <= 1 {
		return
	}
	s.Blocks = append(s.Blocks[:i], s.Blocks[i+1:]...)
	var arr []int
	for _, b := range s.Arrangement {
		switch {
		case b == i:
			// drop the slot entirely
		case b > i:
			arr = append(arr, b-1)
		default:
			arr = append(arr, b)
		}
	}
	s.Arrangement = arr
}

// --- Arrangement editing helpers ---

func (s *Song) arrInsert(at, blockIdx int) {
	if at < 0 {
		at = 0
	}
	if at > len(s.Arrangement) {
		at = len(s.Arrangement)
	}
	s.Arrangement = append(s.Arrangement, 0)
	copy(s.Arrangement[at+1:], s.Arrangement[at:])
	s.Arrangement[at] = blockIdx
}

func (s *Song) arrDelete(from, to int) {
	from, to = clampRange(from, to, len(s.Arrangement))
	if from < 0 {
		return
	}
	s.Arrangement = append(s.Arrangement[:from], s.Arrangement[to+1:]...)
}

// arrMove shifts the slot range [from,to] by delta positions.
func (s *Song) arrMove(from, to, delta int) (newFrom, newTo int) {
	from, to = clampRange(from, to, len(s.Arrangement))
	if from < 0 {
		return from, to
	}
	dst := from + delta
	if dst < 0 || dst+(to-from) >= len(s.Arrangement) {
		return from, to
	}
	seg := make([]int, to-from+1)
	copy(seg, s.Arrangement[from:to+1])
	rest := append([]int{}, s.Arrangement[:from]...)
	rest = append(rest, s.Arrangement[to+1:]...)
	out := make([]int, 0, len(s.Arrangement))
	out = append(out, rest[:dst]...)
	out = append(out, seg...)
	out = append(out, rest[dst:]...)
	s.Arrangement = out
	return dst, dst + (to - from)
}

func clampRange(a, b, n int) (int, int) {
	if a > b {
		a, b = b, a
	}
	if a < 0 {
		a = 0
	}
	if b >= n {
		b = n - 1
	}
	if n == 0 || a > b {
		return -1, -1
	}
	return a, b
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
