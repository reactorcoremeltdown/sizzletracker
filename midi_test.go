package main

import "testing"

func TestClassifyMidi(t *testing.T) {
	cases := []struct {
		name         string
		status, data int
		want         midiKind
	}{
		{"note-on", 0x90, 100, kindNoteOn},
		{"note-on ch5", 0x95, 64, kindNoteOn},
		{"note-on vel0 => off", 0x90, 0, kindNoteOff},
		{"note-off", 0x80, 0, kindNoteOff},
		{"control change", 0xB0, 7, kindCC},
		{"control change ch10", 0xBA, 64, kindCC},
		{"program change", 0xC0, 0, kindPC},
		{"program change ch3", 0xC3, 0, kindPC},
		{"pitch bend (ignored)", 0xE0, 0, kindOther},
		{"aftertouch (ignored)", 0xD0, 0, kindOther},
	}
	for _, c := range cases {
		if got := classifyMidi(c.status, c.data); got != c.want {
			t.Errorf("%s: classifyMidi(%#x,%d) = %d, want %d", c.name, c.status, c.data, got, c.want)
		}
	}
}

// TestForwardNoOutputSafe ensures CC/PC passthrough is a no-op (no panic) when
// no output port is open.
func TestForwardNoOutputSafe(t *testing.T) {
	m := &MidiEngine{} // outStr == nil
	m.forward(0xB0, 7, 127)
	m.forward(0xC0, 5, 0)
	m.noteOn(0, 60, 100)
	m.noteOff(0, 60)
}
