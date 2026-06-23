package main

import (
	"sync"
	"time"
)

// LoopMode controls what the transport does when it reaches the end of a block.
type LoopMode int

const (
	LoopSong  LoopMode = iota // play the arrangement, loop back to its start
	LoopBlock                 // loop the currently playing block forever (live looping)
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

	mu      sync.Mutex
	playing bool
	loop    LoopMode
	arrPos  int // index into Arrangement currently sounding
	tick    int // tick within the current block
	playBlk int // resolved block index currently sounding (for the UI)
	held    map[*Track]held

	// editBlock is the block index the user is editing; LoopBlock loops it.
	editBlock int

	stopCh chan struct{}
}

func newPlayer(s *Song, m *MidiEngine) *Player {
	return &Player{
		song: s,
		midi: m,
		loop: LoopSong,
		held: make(map[*Track]held),
	}
}

func (p *Player) isPlaying() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playing
}

// playhead returns the currently sounding block index and tick (for the UI).
func (p *Player) playhead() (block, tick int, playing bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playBlk, p.tick, p.playing
}

// state returns a consistent snapshot of the transport for the renderer.
func (p *Player) state() (block, tick, arrPos int, playing bool, loop LoopMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.playBlk, p.tick, p.arrPos, p.playing, p.loop
}

func (p *Player) setLoopMode(m LoopMode) {
	p.mu.Lock()
	p.loop = m
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

// start begins playback from the given arrangement position.
func (p *Player) start(fromArrPos int) {
	p.mu.Lock()
	if p.playing {
		p.mu.Unlock()
		return
	}
	p.playing = true
	p.arrPos = fromArrPos
	p.tick = 0
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

func (p *Player) toggle(fromArrPos int) {
	if p.isPlaying() {
		p.stop()
	} else {
		p.start(fromArrPos)
	}
}

// playFrom (re)starts playback at the given arrangement position.
func (p *Player) playFrom(fromArrPos int) {
	p.stop()
	p.start(fromArrPos)
}

// allOff cuts every held note (used on stop / panic).
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

// resolveBlock returns the block that should sound for the current arrPos,
// honouring the loop mode. Returns nil if there is nothing to play.
func (p *Player) resolveBlock() *Block {
	s := p.song
	if p.loop == LoopBlock {
		if p.editBlock >= 0 && p.editBlock < len(s.Blocks) {
			p.playBlk = p.editBlock
			return s.Blocks[p.editBlock]
		}
		return nil
	}
	if len(s.Arrangement) == 0 {
		return nil
	}
	if p.arrPos < 0 || p.arrPos >= len(s.Arrangement) {
		p.arrPos = 0
	}
	bi := s.Arrangement[p.arrPos]
	if bi < 0 || bi >= len(s.Blocks) {
		return nil
	}
	p.playBlk = bi
	return s.Blocks[bi]
}

// midiOp is one note event to be emitted after the locks are released, so the
// (potentially blocking) MIDI I/O never happens while holding song.mu — that
// keeps playback timing independent of UI rendering and user input.
type midiOp struct {
	on   bool
	ch   int
	note int
	vel  int
}

// run is the timing loop. It uses an absolute target clock to avoid drift.
// Each tick the song is read under lock to *collect* the events to emit; the
// actual MIDI sends happen afterwards with no lock held.
func (p *Player) run(stop chan struct{}) {
	next := time.Now()
	lastBlk := -1
	ops := make([]midiOp, 0, 32)
	for {
		select {
		case <-stop:
			return
		default:
		}

		ops = ops[:0]

		p.song.mu.Lock()
		p.mu.Lock()

		blk := p.resolveBlock()
		if blk == nil || blk.Length == 0 {
			p.mu.Unlock()
			p.song.mu.Unlock()
			time.Sleep(20 * time.Millisecond)
			next = time.Now()
			continue
		}
		if p.tick >= blk.Length {
			p.tick = 0
		}

		// When the sounding block changes, release any notes still held from
		// the previous block so they don't hang (their tracks no longer
		// exist in the new block). Within a block, notes sustain until an
		// explicit note-off or a retrigger.
		if p.playBlk != lastBlk {
			for t, h := range p.held {
				if h.active {
					ops = append(ops, midiOp{on: false, ch: h.chan_, note: h.note})
				}
				delete(p.held, t)
			}
			lastBlk = p.playBlk
		}

		spt := p.song.secondsPerTick()
		ops = p.collectTick(blk, p.tick, ops)

		// Advance position.
		p.tick++
		if p.tick >= blk.Length {
			p.tick = 0
			if p.loop == LoopSong {
				p.arrPos++
				if p.arrPos >= len(p.song.Arrangement) {
					p.arrPos = 0
				}
			}
		}

		p.mu.Unlock()
		p.song.mu.Unlock()

		// Emit MIDI with no lock held. Order is preserved so note-off before
		// note-on (retrigger) stays correct.
		for _, op := range ops {
			if op.on {
				p.midi.noteOn(op.ch, op.note, op.vel)
			} else {
				p.midi.noteOff(op.ch, op.note)
			}
		}

		// Sleep until the next absolute tick boundary.
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

// collectTick gathers the note events for one row across all tracks, updating
// the held-note bookkeeping. Caller holds both song.mu and p.mu. The actual
// MIDI sends are performed by run() after releasing the locks.
func (p *Player) collectTick(blk *Block, tick int, ops []midiOp) []midiOp {
	for _, t := range blk.Tracks {
		if tick >= len(t.Steps) {
			continue
		}
		st := t.Steps[tick]
		switch st.Note {
		case NoteEmpty:
			// sustain
		case NoteOff:
			if h := p.held[t]; h.active {
				ops = append(ops, midiOp{on: false, ch: h.chan_, note: h.note})
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
				ops = append(ops, midiOp{on: false, ch: h.chan_, note: h.note})
			}
			ops = append(ops, midiOp{on: true, ch: ch, note: st.Note, vel: vel})
			p.held[t] = held{active: true, note: st.Note, chan_: ch}
		}
	}
	return ops
}
