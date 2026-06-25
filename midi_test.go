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

// fakeEngine builds a MidiEngine with named (but unopened) ports for testing
// the routing logic without touching real MIDI hardware.
func fakeEngine(outs, ins []string) *MidiEngine {
	m := &MidiEngine{}
	for _, n := range outs {
		m.outs = append(m.outs, portDevice{name: n})
	}
	for _, n := range ins {
		m.ins = append(m.ins, portDevice{name: n})
	}
	m.initMatrix()
	return m
}

func eqInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestPatchRouting(t *testing.T) {
	m := fakeEngine([]string{"A", "B"}, []string{"K"}) // inputs: Trk(0), K(1); outputs A(0), B(1)

	if m.numInputs() != 2 || m.numOutputs() != 2 {
		t.Fatalf("topology: %d inputs, %d outputs", m.numInputs(), m.numOutputs())
	}
	if m.inputName(0) != trackerInputName || m.inputName(1) != "K" {
		t.Errorf("input names: %q %q", m.inputName(0), m.inputName(1))
	}

	m.toggleRoute(0, 0) // Tracker -> A
	if !m.route(0, 0) || m.route(0, 1) {
		t.Errorf("route Tracker->A not set correctly")
	}
	if got := m.trackerOuts(0); !eqInts(got, []int{0}) {
		t.Errorf("trackerOuts(ch0) = %v, want [0]", got)
	}

	// Channel filter: drop channel 5 on output A.
	m.toggleFilter(0, 5)
	if m.filterOn(0, 5) {
		t.Errorf("channel 5 should be filtered out on A")
	}
	if got := m.trackerOuts(5); len(got) != 0 {
		t.Errorf("trackerOuts(ch5) = %v, want [] (filtered)", got)
	}
	if got := m.trackerOuts(0); !eqInts(got, []int{0}) {
		t.Errorf("trackerOuts(ch0) after filter = %v, want [0]", got)
	}

	// All / none on output B.
	m.setFilterAll(1, false)
	if m.filterOn(1, 3) {
		t.Errorf("setFilterAll(false) did not clear channels")
	}
	m.setFilterAll(1, true)
	if !m.filterOn(1, 3) {
		t.Errorf("setFilterAll(true) did not set channels")
	}

	// Persist and restore.
	m.toggleRoute(1, 1) // K -> B
	m.toggleClock(0)    // disable clock on A
	routes, filters, clockOff := m.exportPatch()
	m2 := fakeEngine([]string{"A", "B"}, []string{"K"})
	m2.applyPatch(routes, filters, clockOff)
	if !m2.route(0, 0) || !m2.route(1, 1) || m2.route(0, 1) {
		t.Errorf("applyPatch did not restore routes")
	}
	if m2.filterOn(0, 5) {
		t.Errorf("applyPatch did not restore filter (ch5 on A should stay off)")
	}
	if m2.clockOn(0) || !m2.clockOn(1) {
		t.Errorf("applyPatch did not restore clock state (A off, B on)")
	}
}

func TestClockRouting(t *testing.T) {
	m := fakeEngine([]string{"A", "B"}, []string{"K"})
	m.toggleRoute(0, 0) // Tracker -> A
	m.toggleRoute(0, 1) // Tracker -> B

	// Clock defaults on: both routed outputs receive it.
	got := m.clockOuts()
	if len(got) != 2 {
		t.Fatalf("clockOuts default = %v, want both outputs", got)
	}

	// Disabling clock on A drops it from the realtime fan-out but keeps the
	// note routing intact.
	m.toggleClock(0)
	if m.clockOn(0) {
		t.Errorf("clock on A should be off")
	}
	got = m.clockOuts()
	if len(got) != 1 || got[0] != 1 {
		t.Errorf("clockOuts after disabling A = %v, want [1]", got)
	}
	if outs := m.trackerOuts(0); len(outs) != 2 {
		t.Errorf("note routing must be unaffected by the clock toggle: %v", outs)
	}
}

func TestSameNames(t *testing.T) {
	if !sameNames([]string{"A", "B"}, []string{"A", "B"}) {
		t.Errorf("identical name lists should compare equal")
	}
	if sameNames([]string{"A", "B"}, []string{"A", "C"}) {
		t.Errorf("differing names should not compare equal")
	}
	if sameNames([]string{"A"}, []string{"A", "B"}) {
		t.Errorf("different lengths should not compare equal")
	}
	if got := deviceNames([]portDevice{{name: "X"}, {name: "Y"}}); !eqStr(got, []string{"X", "Y"}) {
		t.Errorf("deviceNames = %v, want [X Y]", got)
	}
}

func eqStr(a, b []string) bool { return sameNames(a, b) }

// TestRescanUnavailable verifies a rescan on an engine without PortMidi is a
// safe no-op (returns false, touches nothing).
func TestRescanUnavailable(t *testing.T) {
	m := fakeEngine([]string{"A"}, []string{"K"}) // available defaults to false
	if m.rescan() {
		t.Errorf("rescan on unavailable engine should report no change")
	}
}

// TestPatchSurvivesDeviceReorder mirrors what rescan relies on: routing and
// filters are restored by device *name* even when device indices change (e.g.
// a device was unplugged so the others shifted position).
func TestPatchSurvivesDeviceReorder(t *testing.T) {
	m := fakeEngine([]string{"A", "B", "C"}, []string{"K"})
	m.toggleRoute(0, 1) // Tracker -> B
	m.toggleRoute(1, 2) // K -> C
	m.setFilterAll(2, false)
	m.toggleFilter(2, 0) // C: only channels 0 and 1
	m.toggleFilter(2, 1)
	routes, filters, clockOff := m.exportPatch()

	// Same devices, different order (as after a rescan re-enumeration).
	m2 := fakeEngine([]string{"C", "A", "B"}, []string{"K"}) // B=2, C=0
	m2.applyPatch(routes, filters, clockOff)

	if !m2.route(0, 2) {
		t.Errorf("Tracker->B not restored at B's new index")
	}
	if !m2.route(1, 0) {
		t.Errorf("K->C not restored at C's new index")
	}
	if m2.route(0, 0) || m2.route(1, 2) {
		t.Errorf("stale routes leaked to wrong indices")
	}
	if !m2.filterOn(0, 0) || !m2.filterOn(0, 1) || m2.filterOn(0, 2) {
		t.Errorf("C channel filter not restored by name")
	}
}
