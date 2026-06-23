package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestProjectRoundTrip(t *testing.T) {
	s := newSong()
	// Put some content in: a few notes with channels, a note-off, a marker.
	blk := s.Blocks[0]
	blk.Tracks[0].Steps[0] = Step{Note: 60, Vel: 100, Chan: 0}
	blk.Tracks[0].Steps[4] = Step{Note: 67, Vel: ValEmpty, Chan: 9}
	blk.Tracks[0].Steps[8] = Step{Note: NoteOff, Vel: ValEmpty, Chan: ValEmpty}
	blk.Tracks[1].Steps[2] = Step{Note: 36, Vel: 0x7f, Chan: ValEmpty}
	s.setSig(TimeSig{3, 4}) // exercises non-default signature (rescales blocks)

	text := encodeProject(s)
	got, err := decodeProject(strings.NewReader(text))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.BPM != s.BPM {
		t.Errorf("BPM = %v, want %v", got.BPM, s.BPM)
	}
	if got.Sig != s.Sig {
		t.Errorf("Sig = %v, want %v", got.Sig, s.Sig)
	}
	if len(got.Blocks) != len(s.Blocks) {
		t.Fatalf("blocks = %d, want %d", len(got.Blocks), len(s.Blocks))
	}
	for i := range s.Blocks {
		if got.Blocks[i].Length != s.Blocks[i].Length {
			t.Errorf("block %d length = %d, want %d", i, got.Blocks[i].Length, s.Blocks[i].Length)
		}
		if len(got.Blocks[i].Tracks) != len(s.Blocks[i].Tracks) {
			t.Errorf("block %d tracks = %d, want %d", i, len(got.Blocks[i].Tracks), len(s.Blocks[i].Tracks))
		}
	}
	// Steps survived exactly.
	g0 := got.Blocks[0].Tracks[0]
	if g0.Steps[0] != (Step{Note: 60, Vel: 100, Chan: 0}) {
		t.Errorf("step0 = %+v", g0.Steps[0])
	}
	if g0.Steps[4] != (Step{Note: 67, Vel: ValEmpty, Chan: 9}) {
		t.Errorf("step4 = %+v", g0.Steps[4])
	}
	if g0.Steps[8].Note != NoteOff {
		t.Errorf("step8 note = %d, want NoteOff", g0.Steps[8].Note)
	}
	// Roll markers survived.
	for b := 0; b < 4; b++ {
		if got.Roll[0][b] != s.Roll[0][b] {
			t.Errorf("roll[0][%d] = %v, want %v", b, got.Roll[0][b], s.Roll[0][b])
		}
	}
}

func TestNoteTokenRoundTrip(t *testing.T) {
	for n := 0; n <= 127; n++ {
		if got := parseNote(noteToken(n)); got != n {
			t.Errorf("note %d -> %q -> %d", n, noteToken(n), got)
		}
	}
	if parseNote(noteToken(NoteOff)) != NoteOff {
		t.Errorf("note-off did not round-trip")
	}
}

func TestExportMIDIHeader(t *testing.T) {
	s := newSong()
	data := encodeMIDI(s)
	if !bytes.HasPrefix(data, []byte("MThd")) {
		t.Fatalf("missing MThd header")
	}
	if !bytes.Contains(data, []byte("MTrk")) {
		t.Errorf("missing MTrk track chunk")
	}
	// Header length field must be 6 and division must be midiPPQ.
	if data[7] != 6 {
		t.Errorf("MThd length = %d, want 6", data[7])
	}
	div := int(data[12])<<8 | int(data[13])
	if div != midiPPQ {
		t.Errorf("division = %d, want %d", div, midiPPQ)
	}
}
