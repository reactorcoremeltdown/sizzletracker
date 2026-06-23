package main

import "testing"

func TestTimeSigTicks(t *testing.T) {
	cases := []struct {
		sig             TimeSig
		ticksPerBeat    int
		ticksPerBar     int
		linesPerBeatDoc int // "3/4 has 3 lines/beat", etc.
	}{
		{TimeSig{3, 4}, 3, 12, 3},
		{TimeSig{4, 4}, 4, 16, 4},
		{TimeSig{5, 4}, 5, 20, 5},
	}
	for _, c := range cases {
		if got := c.sig.ticksPerBeat(); got != c.ticksPerBeat {
			t.Errorf("%s ticksPerBeat = %d, want %d", c.sig, got, c.ticksPerBeat)
		}
		if got := c.sig.ticksPerBeat(); got != c.linesPerBeatDoc {
			t.Errorf("%s lines per beat = %d, want %d", c.sig, got, c.linesPerBeatDoc)
		}
		if got := c.sig.ticksPerBar(); got != c.ticksPerBar {
			t.Errorf("%s ticksPerBar = %d, want %d", c.sig, got, c.ticksPerBar)
		}
	}
}

// TestBlockLengthScale checks the per-signature length scale: 12/24/48 (3/4),
// 16/32/64 (4/4), 20/40/80 (5/4) — i.e. doubling from one bar.
func TestBlockLengthScale(t *testing.T) {
	want := map[TimeSig][]int{
		{3, 4}: {12, 24, 48},
		{4, 4}: {16, 32, 64},
		{5, 4}: {20, 40, 80},
	}
	for sig, seq := range want {
		bar := sig.ticksPerBar()
		for i, exp := range seq {
			got := bar << i // bar, bar*2, bar*4
			if got != exp {
				t.Errorf("%s length step %d = %d, want %d", sig, i, got, exp)
			}
		}
	}
}

// TestSetSigRescale checks a one-bar block stays one bar across signatures.
func TestSetSigRescale(t *testing.T) {
	s := newSong() // 4/4, blocks default to 16 ticks (one bar)
	if got := s.Blocks[0].Length; got != 16 {
		t.Fatalf("default block length = %d, want 16", got)
	}
	s.setSig(TimeSig{3, 4})
	if got := s.Blocks[0].Length; got != 12 {
		t.Errorf("after 3/4, length = %d, want 12 (one bar)", got)
	}
	s.setSig(TimeSig{5, 4})
	if got := s.Blocks[0].Length; got != 20 {
		t.Errorf("after 5/4, length = %d, want 20 (one bar)", got)
	}
	if got := s.blockBeats(0); got != 4 {
		t.Errorf("blockBeats = %d, want 4 (one bar = 4 beats)", got)
	}
}

// TestRollPaintErase checks placing a block paints its bar-length of beats and
// that individual beats can be erased.
func TestRollPaintErase(t *testing.T) {
	s := newSong() // block A: 16 ticks => 4 beats
	for b := 0; b < rollBeats; b++ {
		s.Roll[0][b] = false
	}
	s.rollPaint(0, 2)
	for b := 2; b < 6; b++ {
		if !s.rollGet(0, b) {
			t.Errorf("beat %d should be marked after paint", b)
		}
	}
	if s.rollGet(0, 6) {
		t.Errorf("beat 6 should be empty (block spans 4 beats)")
	}
	s.rollSet(0, 3, false)
	if s.rollGet(0, 3) {
		t.Errorf("beat 3 should be erased")
	}
}
