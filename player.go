package main

import (
	"sync"
	"time"
)

// LoopMode controls what the transport plays.
type LoopMode int

const (
	LoopSong  LoopMode = iota // play the piano roll left-to-right, looping
	LoopBlock                 // loop the currently edited block (live looping)
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
	pTick    int // global tick (song timeline) or tick within block (block loop)
	playBeat int // current roll beat (song mode), -1 otherwise
	playBlk  int // block index currently sounding in the tracker view (-1 none)
	playTick int // local tick within playBlk
	held     map[*Track]held

	editBlock int // block the user is editing; LoopBlock loops it

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

// state is a consistent transport snapshot for the renderer.
func (p *Player) state() (beat, block, tick int, playing bool, loop LoopMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playBeat, p.playBlk, p.playTick, p.playing, p.loop
}

func (p *Player) setLoopMode(m LoopMode) {
	p.mu.Lock()
	p.loop = m
	p.pTick = 0
	p.mu.Unlock()
}

func (p *Player) loopMode() LoopMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.loop
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
		var spt float64
		if p.loop == LoopBlock {
			ops, spt = p.stepBlockLoop(ops)
		} else {
			ops, spt = p.stepSongRoll(ops)
		}
		p.mu.Unlock()
		p.song.mu.Unlock()

		for _, op := range ops {
			if op.on {
				p.midi.noteOn(op.ch, op.note, op.vel)
			} else {
				p.midi.noteOff(op.ch, op.note)
			}
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

// stepBlockLoop plays one tick of the edited block, looping. Caller holds both
// locks.
func (p *Player) stepBlockLoop(ops []midiOp) ([]midiOp, float64) {
	s := p.song
	if p.editBlock < 0 || p.editBlock >= len(s.Blocks) {
		return ops, 0
	}
	blk := s.Blocks[p.editBlock]
	if blk.Length == 0 {
		return ops, 0
	}
	if p.pTick >= blk.Length {
		p.pTick = 0
	}
	p.playBeat = -1
	p.playBlk = p.editBlock
	p.playTick = p.pTick
	for _, t := range blk.Tracks {
		if p.pTick < len(t.Steps) {
			ops = p.applyStep(t, t.Steps[p.pTick], ops)
		}
	}
	p.pTick++
	if p.pTick >= blk.Length {
		p.pTick = 0
	}
	return ops, s.secondsPerTick()
}

// stepSongRoll plays one tick of the piano roll. Caller holds both locks.
func (p *Player) stepSongRoll(ops []midiOp) ([]midiOp, float64) {
	s := p.song
	tpb := s.ticksPerBeat()
	totalBeats := s.totalBeats()
	if totalBeats < 1 {
		// Empty roll: keep the playhead parked and release any held notes.
		p.playBeat = 0
		p.playBlk = -1
		ops = p.releaseAll(ops)
		return ops, 0
	}
	totalTicks := totalBeats * tpb
	if p.pTick >= totalTicks {
		p.pTick = 0
	}
	beat := p.pTick / tpb
	sub := p.pTick % tpb
	p.playBeat = beat
	p.playBlk = -1

	for bi, blk := range s.Blocks {
		if !s.rollGet(bi, beat) {
			// Block silent this beat: release its tracks' held notes.
			for _, t := range blk.Tracks {
				if h := p.held[t]; h.active {
					ops = append(ops, midiOp{ch: h.chan_, note: h.note})
					p.held[t] = held{}
				}
			}
			continue
		}
		// Find the start of this contiguous run so the block's pattern plays
		// from its beginning (and restarts after any erased gap).
		runStart := beat
		for runStart > 0 && s.rollGet(bi, runStart-1) {
			runStart--
		}
		bb := blk.Length / tpb
		if bb < 1 {
			bb = 1
		}
		localBeat := (beat - runStart) % bb
		localTick := localBeat*tpb + sub
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

	p.pTick++
	if p.pTick >= totalTicks {
		p.pTick = 0
	}
	return ops, s.secondsPerTick()
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
