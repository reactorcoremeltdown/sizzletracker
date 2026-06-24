package main

import (
	"sync"
	"time"
)

// LoopMode controls what the transport plays.
type LoopMode int

const (
	LoopSong   LoopMode = iota // play the roll once to the last marked beat, then stop
	LoopRegion                 // loop the marked bar region
)

// held tracks a sounding note so it can be cut when a new note replaces it.
// Tracker behaviour is monophonic per track: one note per lane at a time.
type held struct {
	active bool
	note   int
	chan_  int
}

// Player owns the timing goroutine and the MIDI note lifecycle. It reads the
// Song under the song mutex on every tick, so edits made live are picked up.
type Player struct {
	song *Song
	midi *MidiEngine

	mu       sync.Mutex
	playing  bool
	loop     LoopMode
	pTick    int // global tick along the roll timeline
	playBeat int // current roll beat
	playBlk  int // block index currently sounding in the tracker view (-1 none)
	playTick int // local tick within playBlk
	held     map[*Track]held

	// pendingMode is a mode change requested while playing; it is applied at the
	// next bar boundary so the current block finishes first (no abrupt jump).
	pendingMode LoopMode
	hasPending  bool

	editBlock int // block the user is editing (for tracker playhead display)

	stopCh chan struct{}
}

func newPlayer(s *Song, m *MidiEngine) *Player {
	return &Player{
		song:     s,
		midi:     m,
		loop:     LoopSong,
		playBlk:  -1,
		playBeat: -1,
		held:     make(map[*Track]held),
	}
}

func (p *Player) isPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// playhead reports the block/tick sounding in the tracker (for punch-in).
func (p *Player) playhead() (block, tick int, playing bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playBlk, p.playTick, p.playing
}

// state is a consistent transport snapshot for the renderer. The reported loop
// is the *target* mode (a pending change shows immediately in the UI).
func (p *Player) state() (beat, block, tick int, playing bool, loop LoopMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playBeat, p.playBlk, p.playTick, p.playing, p.effectiveLoopLocked()
}

func (p *Player) effectiveLoopLocked() LoopMode {
	if p.hasPending {
		return p.pendingMode
	}
	return p.loop
}

// setLoopMode changes the loop mode. While playing it is deferred to the next
// bar boundary (the current block finishes first); while stopped it applies at
// once. The playhead is never reset here, so toggling does not jump.
func (p *Player) setLoopMode(m LoopMode) {
	p.mu.Lock()
	if p.playing {
		p.pendingMode = m
		p.hasPending = true
	} else {
		p.loop = m
		p.hasPending = false
	}
	p.mu.Unlock()
}

// loopMode reports the target loop mode (pending change included).
func (p *Player) loopMode() LoopMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.effectiveLoopLocked()
}

func (p *Player) setEditBlock(i int) {
	p.mu.Lock()
	p.editBlock = i
	p.mu.Unlock()
}

func (p *Player) start() {
	p.mu.Lock()
	if p.playing {
		p.mu.Unlock()
		return
	}
	p.playing = true
	p.pTick = 0
	p.playBeat = 0
	p.stopCh = make(chan struct{})
	stop := p.stopCh
	p.mu.Unlock()

	go p.run(stop)
	go p.clock(stop)
}

// clock emits MIDI clock (24 PPQN) plus Start/Stop to whatever outputs the
// Tracker source is patched to. It runs independently of the tick loop because
// 24 PPQN does not divide evenly into the per-signature tick rate.
func (p *Player) clock(stop chan struct{}) {
	p.midi.sendStart()
	next := time.Now()
	for {
		select {
		case <-stop:
			p.midi.sendStop()
			return
		default:
		}
		p.midi.sendClock()

		p.song.mu.Lock()
		bpm := p.song.BPM
		p.song.mu.Unlock()
		if bpm < 1 {
			bpm = 120
		}
		next = next.Add(time.Duration(60.0 / bpm / 24.0 * float64(time.Second)))
		d := time.Until(next)
		if d < 0 {
			next = time.Now()
			d = 0
		}
		select {
		case <-stop:
			p.midi.sendStop()
			return
		case <-time.After(d):
		}
	}
}

func (p *Player) stop() {
	p.mu.Lock()
	if !p.playing {
		p.mu.Unlock()
		return
	}
	p.playing = false
	close(p.stopCh)
	p.mu.Unlock()

	p.allOff()
	p.midi.allNotesOff()
}

// playFrom (re)starts playback from the beginning of the timeline.
func (p *Player) playFrom() {
	p.stop()
	p.start()
}

func (p *Player) allOff() {
	p.mu.Lock()
	for t, h := range p.held {
		if h.active {
			p.midi.noteOff(h.chan_, h.note)
		}
		delete(p.held, t)
	}
	p.mu.Unlock()
}

// midiOp is one note event to be emitted after the locks are released, so the
// (potentially blocking) MIDI I/O never happens while holding song.mu.
type midiOp struct {
	on   bool
	ch   int
	note int
	vel  int
}

// run is the timing loop. Each tick the song is read under lock to collect the
// events to emit; the actual MIDI sends happen afterwards with no lock held.
func (p *Player) run(stop chan struct{}) {
	next := time.Now()
	ops := make([]midiOp, 0, 64)
	for {
		select {
		case <-stop:
			return
		default:
		}

		ops = ops[:0]
		p.song.mu.Lock()
		p.mu.Lock()
		ops2, spt, done := p.stepRoll(ops)
		ops = ops2
		p.mu.Unlock()
		p.song.mu.Unlock()

		for _, op := range ops {
			if op.on {
				p.midi.noteOn(op.ch, op.note, op.vel)
			} else {
				p.midi.noteOff(op.ch, op.note)
			}
		}

		if done { // song reached its end -> stop the transport
			p.stop()
			return
		}

		if spt <= 0 { // nothing to play; idle without busy-spinning
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
			}
			next = time.Now()
			continue
		}

		next = next.Add(time.Duration(spt * float64(time.Second)))
		d := time.Until(next)
		if d < 0 {
			next = time.Now()
			d = 0
		}
		select {
		case <-stop:
			return
		case <-time.After(d):
		}
	}
}

// stepRoll plays one tick of the piano roll. Returns the events to emit, the
// seconds until the next tick, and whether the song has finished (song mode
// reaching the end). Caller holds both locks.
func (p *Player) stepRoll(ops []midiOp) ([]midiOp, float64, bool) {
	s := p.song
	tpb := s.ticksPerBeat()
	barTicks := s.Sig.beatsPerBar() * tpb
	if barTicks < 1 {
		barTicks = 1
	}

	// Apply a deferred mode change at a bar boundary so the current block plays
	// out before the new mode takes over.
	if p.hasPending && p.pTick%barTicks == 0 {
		p.loop = p.pendingMode
		p.hasPending = false
		if p.loop == LoopRegion {
			lo, _ := s.loopRegionTicks()
			p.pTick = lo // start the loop at the region's beginning
		}
	}

	if p.loop == LoopRegion {
		lo, hi := s.loopRegionTicks()
		if hi <= lo {
			lo, hi = 0, barTicks
		}
		if p.pTick < lo || p.pTick >= hi {
			p.pTick = lo
		}
		ops = p.playAt(s, p.pTick, ops)
		p.pTick++
		if p.pTick >= hi {
			p.pTick = lo
		}
		return ops, s.secondsPerTick(), false
	}

	// Song mode: play once to the last marked beat, then stop.
	totalTicks := s.totalBeats() * tpb
	if totalTicks < 1 {
		// Nothing marked yet: idle (stay armed) until the user adds markers.
		p.playBeat = 0
		p.playBlk = -1
		ops = p.releaseAll(ops)
		return ops, 0, false
	}
	if p.pTick >= totalTicks {
		ops = p.releaseAll(ops)
		return ops, 0, true // reached the end
	}
	ops = p.playAt(s, p.pTick, ops)
	p.pTick++
	return ops, s.secondsPerTick(), false
}

// playAt emits the events for the global roll tick pt across all blocks active
// on that beat, and updates the tracker playhead. Caller holds both locks.
func (p *Player) playAt(s *Song, pt int, ops []midiOp) []midiOp {
	tpb := s.ticksPerBeat()
	beat := pt / tpb
	sub := pt % tpb
	p.playBeat = beat
	p.playBlk = -1

	for bi, blk := range s.Blocks {
		if !s.rollGet(bi, beat) {
			for _, t := range blk.Tracks {
				if h := p.held[t]; h.active {
					ops = append(ops, midiOp{ch: h.chan_, note: h.note})
					p.held[t] = held{}
				}
			}
			continue
		}
		// Start of this contiguous run, so the block's pattern plays from its
		// beginning (and restarts after any erased gap).
		runStart := beat
		for runStart > 0 && s.rollGet(bi, runStart-1) {
			runStart--
		}
		bb := blk.Length / tpb
		if bb < 1 {
			bb = 1
		}
		localTick := ((beat-runStart)%bb)*tpb + sub
		if bi == p.editBlock {
			p.playBlk = bi
			p.playTick = localTick
		}
		for _, t := range blk.Tracks {
			if localTick < len(t.Steps) {
				ops = p.applyStep(t, t.Steps[localTick], ops)
			}
		}
	}
	return ops
}

// applyStep emits the note events for one step on one track (mono per track),
// updating the held-note bookkeeping. Caller holds both locks.
func (p *Player) applyStep(t *Track, st Step, ops []midiOp) []midiOp {
	switch st.Note {
	case NoteEmpty:
		// sustain
	case NoteOff:
		if h := p.held[t]; h.active {
			ops = append(ops, midiOp{ch: h.chan_, note: h.note})
			p.held[t] = held{}
		}
	default:
		ch := t.Channel
		if st.Chan != ValEmpty {
			ch = st.Chan
		}
		vel := 100
		if st.Vel != ValEmpty {
			vel = st.Vel
		}
		if h := p.held[t]; h.active {
			ops = append(ops, midiOp{ch: h.chan_, note: h.note})
		}
		ops = append(ops, midiOp{on: true, ch: ch, note: st.Note, vel: vel})
		p.held[t] = held{active: true, note: st.Note, chan_: ch}
	}
	return ops
}

func (p *Player) releaseAll(ops []midiOp) []midiOp {
	for t, h := range p.held {
		if h.active {
			ops = append(ops, midiOp{ch: h.chan_, note: h.note})
		}
		delete(p.held, t)
	}
	return ops
}
