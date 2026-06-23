package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// Project files use a small, line-oriented, human-readable text format:
//
//	# comment
//	version 1
//	bpm 120
//	sig 4 4
//
//	block A 16 4          # name length numTracks
//	roll ####....         # one char per beat: '#' = marker, '.' = empty
//	track T1 0            # name channel(0-based)
//	0 C-4 64 01           # tick note vel chan  (note OFF; vel/chan '..' = default)
//	4 E-4 .. ..
//	endblock
//
// Note tokens use the tracker convention (C-4 = MIDI 60); notes below the
// named range fall back to a raw integer so every value round-trips exactly.

var pitchNames = []string{"C-", "C#", "D-", "D#", "E-", "F-", "F#", "G-", "G#", "A-", "A#", "B-"}

func noteIndex(name string) int {
	for i, n := range pitchNames {
		if n == name {
			return i
		}
	}
	return -1
}

// noteToken renders a note value for the project file (exact round-trip).
func noteToken(n int) string {
	if n == NoteOff {
		return "OFF"
	}
	oct := n/12 - 1
	if oct < 0 { // below C-0: store the raw number so nothing is lost
		return strconv.Itoa(n)
	}
	return pitchNames[n%12] + strconv.Itoa(oct)
}

func parseNote(tok string) int {
	switch tok {
	case "OFF", "===":
		return NoteOff
	case "---", "":
		return NoteEmpty
	}
	if tok[0] >= '0' && tok[0] <= '9' { // raw integer fallback
		return atoiDef(tok, NoteEmpty)
	}
	if len(tok) < 3 {
		return NoteEmpty
	}
	idx := noteIndex(tok[:2])
	if idx < 0 {
		return NoteEmpty
	}
	n := (atoiDef(tok[2:], 0)+1)*12 + idx
	if n < 0 || n > 127 {
		return NoteEmpty
	}
	return n
}

func parseVel(tok string) int {
	if tok == ".." {
		return ValEmpty
	}
	v, err := strconv.ParseInt(tok, 16, 32)
	if err != nil {
		return ValEmpty
	}
	return int(v) & 0x7f
}

func parseChan(tok string) int {
	if tok == ".." {
		return ValEmpty
	}
	return clampInt(atoiDef(tok, 1)-1, 0, 15)
}

func atoiDef(s string, def int) int {
	if v, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return v
	}
	return def
}

func rollString(row []bool) string {
	last := -1
	for i, v := range row {
		if v {
			last = i
		}
	}
	if last < 0 {
		return ""
	}
	b := make([]byte, last+1)
	for i := 0; i <= last; i++ {
		if row[i] {
			b[i] = '#'
		} else {
			b[i] = '.'
		}
	}
	return string(b)
}

func setRollFromString(row []bool, s string) {
	for i := 0; i < len(s) && i < len(row); i++ {
		row[i] = s[i] == '#'
	}
}

// --- encode / decode ------------------------------------------------------

func encodeProject(s *Song) string {
	var b strings.Builder
	b.WriteString("# sizzletracker song\n")
	b.WriteString("# step line: <tick> <note> <vel> <chan>  (note OFF=note-off; vel hex; chan 1-based; .. = default)\n")
	b.WriteString("version 1\n")
	fmt.Fprintf(&b, "bpm %s\n", strconv.FormatFloat(s.BPM, 'f', -1, 64))
	fmt.Fprintf(&b, "sig %d %d\n", s.Sig.Num, s.Sig.Den)
	for i, blk := range s.Blocks {
		fmt.Fprintf(&b, "\nblock %s %d %d\n", blk.Name, blk.Length, len(blk.Tracks))
		fmt.Fprintf(&b, "roll %s\n", rollString(s.Roll[i]))
		for _, t := range blk.Tracks {
			fmt.Fprintf(&b, "track %s %d\n", t.Name, t.Channel)
			for tick, st := range t.Steps {
				if st.Note == NoteEmpty {
					continue
				}
				fmt.Fprintf(&b, "%d %s %s %s\n", tick, noteToken(st.Note), velName(st.Vel), chanName(st.Chan))
			}
		}
		b.WriteString("endblock\n")
	}
	return b.String()
}

func decodeProject(r io.Reader) (*Song, error) {
	s := &Song{BPM: 120, Sig: TimeSig{4, 4}}
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)

	var curBlock *Block
	curIdx := -1
	var curTrack *Track

	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "version":
			// reserved for future migrations
		case "bpm":
			if len(f) >= 2 {
				if v, err := strconv.ParseFloat(f[1], 64); err == nil {
					s.BPM = v
				}
			}
		case "sig":
			if len(f) >= 3 {
				s.Sig = TimeSig{atoiDef(f[1], 4), atoiDef(f[2], 4)}
			}
		case "block":
			if len(f) < 3 {
				continue
			}
			length := atoiDef(f[2], 16)
			if length < 1 {
				length = 1
			}
			b := &Block{Name: f[1], Length: length}
			s.Blocks = append(s.Blocks, b)
			s.Roll = append(s.Roll, newRollRow())
			curBlock, curIdx, curTrack = b, len(s.Blocks)-1, nil
		case "roll":
			if curIdx >= 0 && len(f) >= 2 {
				setRollFromString(s.Roll[curIdx], f[1])
			}
		case "track":
			if curBlock == nil || len(f) < 2 {
				continue
			}
			ch := 0
			if len(f) >= 3 {
				ch = clampInt(atoiDef(f[2], 0), 0, 15)
			}
			t := newTrack(f[1], ch, curBlock.Length)
			curBlock.Tracks = append(curBlock.Tracks, t)
			curTrack = t
		case "endblock":
			curBlock, curTrack = nil, nil
		default:
			// step line: tick note vel chan
			if curTrack == nil || len(f) < 4 {
				continue
			}
			tick := atoiDef(f[0], -1)
			if tick < 0 || tick >= len(curTrack.Steps) {
				continue
			}
			curTrack.Steps[tick] = Step{
				Note: parseNote(f[1]),
				Vel:  parseVel(f[2]),
				Chan: parseChan(f[3]),
			}
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	// Validate / repair.
	if !sigSupported(s.Sig) {
		s.Sig = TimeSig{4, 4}
	}
	if len(s.Blocks) == 0 {
		return nil, fmt.Errorf("no blocks found")
	}
	for i, b := range s.Blocks {
		if len(b.Tracks) == 0 {
			b.Tracks = append(b.Tracks, newTrack("T1", 0, b.Length))
		}
		if b.Name == "" {
			b.Name = blockName(i)
		}
	}
	return s, nil
}

func sigSupported(ts TimeSig) bool {
	for _, o := range timeSigs {
		if o == ts {
			return true
		}
	}
	return false
}

// loadProject reads and parses a project file.
func loadProject(path string) (*Song, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return decodeProject(f)
}

// writeFile writes bytes to a path (used by the UI, which encodes under the
// song lock and then writes the result without holding it).
func writeFile(path string, data []byte) error {
	return os.WriteFile(path, data, 0o644)
}

// --- MIDI export (Standard MIDI File, format 0) ---------------------------

// midiPPQ is the MIDI file's ticks-per-quarter-note. 480 is divisible by every
// supported ticksPerBeat (3, 4, 5), so each tracker row maps to a whole number
// of MIDI ticks.
const midiPPQ = 480

type smfEvent struct {
	tick   int // absolute MIDI ticks
	order  int // 0 = note-off (sorts first), 1 = note-on, at equal tick
	status int
	d1, d2 int
}

// renderEvents simulates one pass of the piano roll, producing absolute-timed
// note events. Caller must hold no lock issues — it only reads s.
func renderEvents(s *Song) []smfEvent {
	tpb := s.ticksPerBeat()
	rowTicks := midiPPQ / tpb
	totalBeats := s.totalBeats()
	totalRows := totalBeats * tpb

	type voice struct {
		on   bool
		note int
		ch   int
	}
	held := make(map[*Track]voice)
	var evs []smfEvent

	off := func(mt, ch, note int) {
		evs = append(evs, smfEvent{tick: mt, order: 0, status: 0x80 | (ch & 0x0f), d1: note & 0x7f, d2: 0})
	}
	on := func(mt, ch, note, vel int) {
		evs = append(evs, smfEvent{tick: mt, order: 1, status: 0x90 | (ch & 0x0f), d1: note & 0x7f, d2: vel & 0x7f})
	}

	for row := 0; row < totalRows; row++ {
		beat := row / tpb
		sub := row % tpb
		mt := row * rowTicks
		for bi, blk := range s.Blocks {
			if !s.rollGet(bi, beat) {
				for _, t := range blk.Tracks {
					if h := held[t]; h.on {
						off(mt, h.ch, h.note)
						held[t] = voice{}
					}
				}
				continue
			}
			runStart := beat
			for runStart > 0 && s.rollGet(bi, runStart-1) {
				runStart--
			}
			bb := blk.Length / tpb
			if bb < 1 {
				bb = 1
			}
			localTick := ((beat-runStart)%bb)*tpb + sub
			for _, t := range blk.Tracks {
				if localTick >= len(t.Steps) {
					continue
				}
				st := t.Steps[localTick]
				switch st.Note {
				case NoteEmpty:
				case NoteOff:
					if h := held[t]; h.on {
						off(mt, h.ch, h.note)
						held[t] = voice{}
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
					if h := held[t]; h.on {
						off(mt, h.ch, h.note)
					}
					on(mt, ch, st.Note, vel)
					held[t] = voice{on: true, note: st.Note, ch: ch}
				}
			}
		}
	}
	// Release anything still sounding at the end.
	endTick := totalRows * rowTicks
	for _, h := range held {
		if h.on {
			off(endTick, h.ch, h.note)
		}
	}

	// Stable sort by (tick, order): offs before ons at the same tick.
	for i := 1; i < len(evs); i++ {
		for j := i; j > 0; j-- {
			a, b := evs[j-1], evs[j]
			if a.tick < b.tick || (a.tick == b.tick && a.order <= b.order) {
				break
			}
			evs[j-1], evs[j] = evs[j], evs[j-1]
		}
	}
	return evs
}

func writeVarLen(buf *bytes.Buffer, n uint32) {
	var b [4]byte
	i := 3
	b[i] = byte(n & 0x7f)
	for n >>= 7; n > 0; n >>= 7 {
		i--
		b[i] = byte(n&0x7f) | 0x80
	}
	buf.Write(b[i:])
}

// encodeMIDI renders the song to a Standard MIDI File (format 0).
func encodeMIDI(s *Song) []byte {
	evs := renderEvents(s)

	var tb bytes.Buffer
	// Tempo meta at delta 0.
	mpq := 60000000 / s.BPM
	usPerQ := uint32(mpq)
	writeVarLen(&tb, 0)
	tb.Write([]byte{0xFF, 0x51, 0x03, byte(usPerQ >> 16), byte(usPerQ >> 8), byte(usPerQ)})

	prev := 0
	for _, e := range evs {
		writeVarLen(&tb, uint32(e.tick-prev))
		tb.Write([]byte{byte(e.status), byte(e.d1), byte(e.d2)})
		prev = e.tick
	}
	// End of track.
	writeVarLen(&tb, 0)
	tb.Write([]byte{0xFF, 0x2F, 0x00})

	var out bytes.Buffer
	out.WriteString("MThd")
	out.Write([]byte{0, 0, 0, 6, 0, 0, 0, 1, byte(midiPPQ >> 8), byte(midiPPQ & 0xff)})
	out.WriteString("MTrk")
	l := uint32(tb.Len())
	out.Write([]byte{byte(l >> 24), byte(l >> 16), byte(l >> 8), byte(l)})
	out.Write(tb.Bytes())
	return out.Bytes()
}

func writeMIDIFile(s *Song, path string) error {
	return os.WriteFile(path, encodeMIDI(s), 0o644)
}
