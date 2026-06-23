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

// TestPolyphonicPunch checks that simultaneously-held incoming notes spread
// across tracks, that new tracks are created when all are busy, and that the
// step-recorder cursor only advances once the whole chord is released.
func TestPolyphonicPunch(t *testing.T) {
	s := newSong()
	app := &App{song: s, player: newPlayer(s, &MidiEngine{}), ed: newEditor()}
	app.ed.armed = true
	app.ed.editBlock = 0
	app.ed.curTrack = 0
	app.ed.curTick = 0

	blk := s.Blocks[0]
	for len(blk.Tracks) > 1 { // start with a single track to force auto-create
		blk.removeTrack(len(blk.Tracks) - 1)
	}

	// Hold a three-note chord (stopped => step recorder).
	app.applyPunch(true, 60, 100)
	app.applyPunch(true, 64, 100)
	app.applyPunch(true, 67, 100)

	if len(blk.Tracks) != 3 {
		t.Fatalf("expected 3 tracks after 3-note chord, got %d", len(blk.Tracks))
	}
	for note, tr := range map[int]int{60: 0, 64: 1, 67: 2} {
		if got := blk.Tracks[tr].Steps[0].Note; got != note {
			t.Errorf("note %d expected on track %d tick 0, got %d", note, tr, got)
		}
	}
	if app.ed.curTick != 0 {
		t.Errorf("cursor advanced while chord still held: %d", app.ed.curTick)
	}

	app.applyPunch(false, 60, 0)
	app.applyPunch(false, 64, 0)
	if app.ed.curTick != 0 {
		t.Errorf("cursor advanced before chord fully released: %d", app.ed.curTick)
	}
	app.applyPunch(false, 67, 0)
	if app.ed.curTick != 1 {
		t.Errorf("cursor should advance to 1 after chord release, got %d", app.ed.curTick)
	}

	// A subsequent single note reuses a now-free track (no growth).
	app.applyPunch(true, 72, 100)
	if len(blk.Tracks) != 3 {
		t.Errorf("single note after release should reuse a track, tracks=%d", len(blk.Tracks))
	}
	if blk.Tracks[0].Steps[1].Note != 72 {
		t.Errorf("single note expected on track 0 tick 1, got %d", blk.Tracks[0].Steps[1].Note)
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
