package main

import "testing"

func TestRollToggleBar(t *testing.T) {
	s := newSong() // 4 beats per bar
	app := &App{song: s, player: newPlayer(s, &MidiEngine{}), ed: newEditor()}
	for b := range s.Roll[0] {
		s.Roll[0][b] = false
	}
	app.rollToggleBar(0, 1) // bar 0 (beats 0-3) -> fill
	for k := 0; k < 4; k++ {
		if !s.rollGet(0, k) {
			t.Errorf("beat %d should be set after toggling bar on", k)
		}
	}
	if s.rollGet(0, 4) {
		t.Errorf("next bar must be untouched")
	}
	app.rollToggleBar(0, 3) // bar fully set -> clear
	for k := 0; k < 4; k++ {
		if s.rollGet(0, k) {
			t.Errorf("beat %d should be cleared after toggling bar off", k)
		}
	}
}

func TestRollToggleSelection(t *testing.T) {
	s := newSong()
	app := &App{song: s, player: newPlayer(s, &MidiEngine{}), ed: newEditor()}
	for b := range s.Roll[0] {
		s.Roll[0][b] = false
	}
	app.ed.focus = FocusArrange

	// Select beats 2..5 on block 0 (cursor at one corner, anchor at the other).
	app.ed.editBlock, app.ed.rollBeat = 0, 5
	app.ed.selActive = true
	app.ed.selRow, app.ed.selBeat = 0, 2

	// First '.' marks every selected beat (none were set).
	app.rollToggle()
	for b := 0; b < len(s.Roll[0]); b++ {
		want := b >= 2 && b <= 5
		if s.rollGet(0, b) != want {
			t.Errorf("after mark: beat %d = %v, want %v", b, s.rollGet(0, b), want)
		}
	}
	if !app.ed.selActive {
		t.Errorf("selection should remain active after toggling")
	}

	// Second '.' clears them (all were set).
	app.rollToggle()
	for b := 2; b <= 5; b++ {
		if s.rollGet(0, b) {
			t.Errorf("after clear: beat %d should be off", b)
		}
	}

	// A partially-marked selection fills (does not clear).
	s.rollSet(0, 3, true)
	app.rollToggle()
	for b := 2; b <= 5; b++ {
		if !s.rollGet(0, b) {
			t.Errorf("partial selection should fill: beat %d should be on", b)
		}
	}

	// With no selection, '.' toggles only the cursor cell.
	app.ed.selActive = false
	app.ed.rollBeat = 10
	app.rollToggle()
	if !s.rollGet(0, 10) || s.rollGet(0, 11) {
		t.Errorf("no-selection toggle should affect only the cursor beat")
	}
}

func TestLoopRegionTicks(t *testing.T) {
	s := newSong() // 4/4: beatsPerBar=4, ticksPerBeat=4 -> barTicks=16
	s.LoopBar0, s.LoopBar1 = 1, 2
	if lo, hi := s.loopRegionTicks(); lo != 16 || hi != 48 {
		t.Errorf("bars 1-2 -> %d,%d, want 16,48", lo, hi)
	}
	s.LoopBar0, s.LoopBar1 = 5, 2 // invalid -> first bar
	if lo, hi := s.loopRegionTicks(); lo != 0 || hi != 16 {
		t.Errorf("invalid range -> %d,%d, want 0,16", lo, hi)
	}
}

func TestSetLoopModeDeferred(t *testing.T) {
	p := newPlayer(newSong(), &MidiEngine{})

	// Stopped: applies immediately.
	p.setLoopMode(LoopRegion)
	if p.loop != LoopRegion || p.hasPending {
		t.Errorf("stopped change should be immediate (loop=%v pending=%v)", p.loop, p.hasPending)
	}

	// Playing: deferred until a bar boundary, but reported as the target.
	p.playing = true
	p.setLoopMode(LoopSong)
	if p.loop != LoopRegion || !p.hasPending || p.pendingMode != LoopSong {
		t.Errorf("playing change should defer (loop=%v pending=%v target=%v)", p.loop, p.hasPending, p.pendingMode)
	}
	if p.loopMode() != LoopSong {
		t.Errorf("loopMode() should report the pending target")
	}
	p.playing = false
}

func TestStepRollSongStopsAtEnd(t *testing.T) {
	s := newSong()
	for i := range s.Roll {
		for b := range s.Roll[i] {
			s.Roll[i][b] = false
		}
	}
	s.Roll[0][0] = true // one marked beat -> totalTicks = ticksPerBeat
	p := newPlayer(s, &MidiEngine{})
	p.loop = LoopSong

	done := false
	for i := 0; i < 100 && !done; i++ {
		_, _, done = p.stepRoll(nil)
	}
	if !done {
		t.Errorf("song mode never reported done at the end")
	}
}

func TestStepRollRegionLoops(t *testing.T) {
	s := newSong()
	s.LoopBar0, s.LoopBar1 = 0, 0 // first bar: ticks [0,16)
	p := newPlayer(s, &MidiEngine{})
	p.loop = LoopRegion

	maxTick, done := 0, false
	for i := 0; i < 50; i++ {
		_, _, d := p.stepRoll(nil)
		done = done || d
		if p.pTick > maxTick {
			maxTick = p.pTick
		}
	}
	if done {
		t.Errorf("region mode should never finish")
	}
	if maxTick >= 16 {
		t.Errorf("region playhead escaped the region: maxTick=%d, want < 16", maxTick)
	}
}

// TestPendingAppliedAtBar checks a deferred switch to loop mode takes effect at
// a bar boundary and jumps to the region start.
func TestPendingAppliedAtBar(t *testing.T) {
	s := newSong()
	s.LoopBar0, s.LoopBar1 = 2, 2 // region = bar 2: ticks [32,48)
	p := newPlayer(s, &MidiEngine{})
	p.loop = LoopSong
	p.playing = true
	p.pTick = 5 // mid bar 0
	p.setLoopMode(LoopRegion)

	for i := 0; i < 64; i++ {
		p.stepRoll(nil)
		if !p.hasPending { // applied
			break
		}
	}
	if p.hasPending {
		t.Fatalf("pending loop change was never applied")
	}
	if p.loop != LoopRegion {
		t.Errorf("mode not switched to region")
	}
	if p.pTick < 32 || p.pTick >= 48 {
		t.Errorf("after applying region, pTick=%d, want within [32,48)", p.pTick)
	}
}
