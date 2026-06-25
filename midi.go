package main

import (
	"strings"
	"sync"
	"time"

	"sizzletracker/internal/portmidi"
)

// portDevice pairs a PortMidi device id with its display name.
type portDevice struct {
	id   portmidi.DeviceID
	name string
}

// trackerInputName is the label of the special patchbay input (input index 0)
// that carries the sequencer's notes and its MIDI clock.
const trackerInputName = "Tracker"

// midiKind classifies an incoming channel-voice message for the input router.
type midiKind int

const (
	kindOther midiKind = iota
	kindNoteOn
	kindNoteOff
	kindCC // Control Change
	kindPC // Program Change
)

// classifyMidi maps a status byte (and note-on velocity) to a midiKind. A
// note-on with zero velocity is treated as a note-off, per the MIDI spec.
func classifyMidi(status, data2 int) midiKind {
	switch status & 0xf0 {
	case 0x90:
		if data2 > 0 {
			return kindNoteOn
		}
		return kindNoteOff
	case 0x80:
		return kindNoteOff
	case 0xB0:
		return kindCC
	case 0xC0:
		return kindPC
	}
	return kindOther
}

// MidiEngine is a routing patchbay. Every available output and input is opened;
// a routing matrix (inputs × outputs) plus a per-output 16-channel filter
// decide what is forwarded where. Input 0 is the special "Tracker" source: the
// sequencer's notes (see noteOn/noteOff) and its MIDI clock.
type MidiEngine struct {
	mu sync.Mutex
	// sendMu serializes all writes to output streams (PortMidi streams are not
	// safe for concurrent writes; the player and input goroutines both send).
	sendMu sync.Mutex

	available bool

	ins  []portDevice
	outs []portDevice

	inStr  []*portmidi.Stream
	inStop []chan struct{}
	outStr []*portmidi.Stream

	// routes[i][o]: input i routed to output o. i==0 is the Tracker source;
	// i>=1 maps to ins[i-1]. filter[o][c]: channel c (0..15) passes to output o.
	routes [][]bool
	filter [][]bool

	// noThru disables forwarding incoming *notes* to outputs (the latch
	// "record only" / "off" modes). CC/PC and the Tracker source are unaffected.
	noThru bool

	onIn func(on bool, note, vel, ch int)
}

// setNoteThru enables/disables forwarding of incoming notes to outputs.
func (m *MidiEngine) setNoteThru(on bool) {
	m.mu.Lock()
	m.noThru = !on
	m.mu.Unlock()
}

func newMidiEngine() *MidiEngine {
	m := &MidiEngine{}
	if err := portmidi.Initialize(); err != nil {
		m.initMatrix()
		return m
	}
	m.available = true
	m.discover()
	m.openAll()
	m.initMatrix()
	m.defaultRouting()
	return m
}

func (m *MidiEngine) discover() {
	n := portmidi.CountDevices()
	for i := 0; i < n; i++ {
		info := portmidi.Info(portmidi.DeviceID(i))
		if info == nil {
			continue
		}
		if info.IsOutputAvailable {
			m.outs = append(m.outs, portDevice{id: portmidi.DeviceID(i), name: info.Name})
		}
		if info.IsInputAvailable {
			m.ins = append(m.ins, portDevice{id: portmidi.DeviceID(i), name: info.Name})
		}
	}
}

func (m *MidiEngine) openAll() {
	m.outStr = make([]*portmidi.Stream, len(m.outs))
	for o := range m.outs {
		if str, err := portmidi.NewOutputStream(m.outs[o].id, 1024, 0); err == nil {
			m.outStr[o] = str
		}
	}
	m.inStr = make([]*portmidi.Stream, len(m.ins))
	m.inStop = make([]chan struct{}, len(m.ins))
	for i := range m.ins {
		str, err := portmidi.NewInputStream(m.ins[i].id, 1024)
		if err != nil {
			continue
		}
		m.inStr[i] = str
		stop := make(chan struct{})
		m.inStop[i] = stop
		go m.listen(i, str, stop)
	}
}

// initMatrix sizes routes (numInputs × numOutputs) and the per-output channel
// filter (default: all channels pass).
func (m *MidiEngine) initMatrix() {
	ni := len(m.ins) + 1 // +1 for the Tracker source
	no := len(m.outs)
	m.routes = make([][]bool, ni)
	for i := range m.routes {
		m.routes[i] = make([]bool, no)
	}
	m.filter = make([][]bool, no)
	for o := range m.filter {
		m.filter[o] = make([]bool, 16)
		for c := range m.filter[o] {
			m.filter[o][c] = true
		}
	}
}

// defaultRouting connects the Tracker source to the default output so the app
// makes sound out of the box (matching the old single-output behaviour).
func (m *MidiEngine) defaultRouting() {
	if len(m.outs) == 0 {
		return
	}
	def := portmidi.DefaultOutputDeviceID()
	sel := 0
	for o, d := range m.outs {
		if d.id == def {
			sel = o
			break
		}
	}
	m.routes[0][sel] = true
}

// --- patchbay topology (read by the UI) ---

func (m *MidiEngine) numInputs() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.ins) + 1
}

func (m *MidiEngine) numOutputs() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.outs)
}

func (m *MidiEngine) inputName(i int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if i == 0 {
		return trackerInputName
	}
	if i-1 >= 0 && i-1 < len(m.ins) {
		return m.ins[i-1].name
	}
	return "?"
}

func (m *MidiEngine) outputName(o int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if o >= 0 && o < len(m.outs) {
		return m.outs[o].name
	}
	return "?"
}

// --- routing matrix (mutated by the UI) ---

func (m *MidiEngine) route(in, out int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return in >= 0 && in < len(m.routes) && out >= 0 && out < len(m.routes[in]) && m.routes[in][out]
}

func (m *MidiEngine) toggleRoute(in, out int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if in >= 0 && in < len(m.routes) && out >= 0 && out < len(m.routes[in]) {
		m.routes[in][out] = !m.routes[in][out]
	}
}

func (m *MidiEngine) filterOn(out, ch int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return out >= 0 && out < len(m.filter) && ch >= 0 && ch < 16 && m.filter[out][ch]
}

func (m *MidiEngine) toggleFilter(out, ch int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if out >= 0 && out < len(m.filter) && ch >= 0 && ch < 16 {
		m.filter[out][ch] = !m.filter[out][ch]
	}
}

func (m *MidiEngine) setFilterAll(out int, v bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if out >= 0 && out < len(m.filter) {
		for c := range m.filter[out] {
			m.filter[out][c] = v
		}
	}
}

// filterSummary returns a short label like "all", "none" or "5ch".
func (m *MidiEngine) filterSummary(out int) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if out < 0 || out >= len(m.filter) {
		return "?"
	}
	n := 0
	for _, v := range m.filter[out] {
		if v {
			n++
		}
	}
	switch n {
	case 16:
		return "all"
	case 0:
		return "none"
	}
	return strings.TrimSpace(strconvI(n)) + "ch"
}

func strconvI(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}

// --- sending (Tracker source) ---

func (m *MidiEngine) setInputCallback(cb func(on bool, note, vel, ch int)) {
	m.mu.Lock()
	m.onIn = cb
	m.mu.Unlock()
}

// trackerOuts returns outputs the Tracker source reaches for channel ch
// (ch < 0 ignores the filter, e.g. for clock).
func (m *MidiEngine) trackerOuts(ch int) []int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.routes) == 0 {
		return nil
	}
	var r []int
	for o := 0; o < len(m.outs); o++ {
		if m.routes[0][o] && (ch < 0 || m.filter[o][ch]) {
			r = append(r, o)
		}
	}
	return r
}

func (m *MidiEngine) noteOn(ch, note, vel int) {
	for _, o := range m.trackerOuts(ch & 0x0f) {
		m.sendTo(o, 0x90|(ch&0x0f), note&0x7f, vel&0x7f)
	}
}

func (m *MidiEngine) noteOff(ch, note int) {
	for _, o := range m.trackerOuts(ch & 0x0f) {
		m.sendTo(o, 0x80|(ch&0x0f), note&0x7f, 0)
	}
}

// trackerRealtime sends a 1-byte realtime message (clock/start/stop) to every
// output the Tracker source is connected to (no channel filter).
func (m *MidiEngine) trackerRealtime(status int) {
	for _, o := range m.trackerOuts(-1) {
		m.sendTo(o, status, 0, 0)
	}
}

func (m *MidiEngine) sendClock() { m.trackerRealtime(0xF8) }
func (m *MidiEngine) sendStart() { m.trackerRealtime(0xFA) }
func (m *MidiEngine) sendStop()  { m.trackerRealtime(0xFC) }

// allNotesOff sends CC 123 on all channels to every open output (panic).
func (m *MidiEngine) allNotesOff() {
	m.mu.Lock()
	n := len(m.outs)
	m.mu.Unlock()
	for o := 0; o < n; o++ {
		for ch := 0; ch < 16; ch++ {
			m.sendTo(o, 0xB0|ch, 123, 0)
		}
	}
}

// sendTo writes one short message to output o, serialized against all senders.
func (m *MidiEngine) sendTo(o, status, d1, d2 int) {
	m.sendMu.Lock()
	defer m.sendMu.Unlock()
	m.mu.Lock()
	var str *portmidi.Stream
	if o >= 0 && o < len(m.outStr) {
		str = m.outStr[o]
	}
	m.mu.Unlock()
	if str == nil {
		return
	}
	str.WriteShort(int64(status&0xff), int64(d1&0x7f), int64(d2&0x7f))
}

// --- input listening + thru forwarding ---

func (m *MidiEngine) listen(hi int, str *portmidi.Stream, stop chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		events, err := str.Read(64)
		if err == nil {
			for _, e := range events {
				status := int(e.Status)
				d1, d2 := int(e.Data1), int(e.Data2)
				ch := status & 0x0f

				// Feed the recorder (gated by record-arm in the UI).
				m.mu.Lock()
				cb := m.onIn
				m.mu.Unlock()
				if cb != nil {
					switch classifyMidi(status, d2) {
					case kindNoteOn:
						cb(true, d1, d2, ch)
					case kindNoteOff:
						cb(false, d1, d2, ch)
					}
				}

				// Thru: forward to routed outputs (channel-filtered).
				m.forwardFrom(hi+1, status, d1, d2)
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (m *MidiEngine) forwardFrom(in, status, d1, d2 int) {
	hi := status & 0xf0
	isNote := hi == 0x80 || hi == 0x90
	isChan := hi >= 0x80 && hi <= 0xE0
	ch := status & 0x0f
	m.mu.Lock()
	if isNote && m.noThru {
		m.mu.Unlock()
		return // note thru disabled by the latch mode
	}
	var outs []int
	if in >= 0 && in < len(m.routes) {
		for o := 0; o < len(m.outs); o++ {
			if m.routes[in][o] && (!isChan || m.filter[o][ch]) {
				outs = append(outs, o)
			}
		}
	}
	m.mu.Unlock()
	for _, o := range outs {
		m.sendTo(o, status, d1, d2)
	}
}

func (m *MidiEngine) close() {
	m.allNotesOff()
	m.mu.Lock()
	for _, st := range m.inStop {
		if st != nil {
			close(st)
		}
	}
	for _, s := range m.inStr {
		if s != nil {
			s.Close()
		}
	}
	for _, s := range m.outStr {
		if s != nil {
			s.Close()
		}
	}
	avail := m.available
	m.mu.Unlock()
	if avail {
		portmidi.Terminate()
	}
}

// --- persistence helpers ---

// exportPatch returns the routes (as "input>>output" name pairs) and the
// per-output channel filters that differ from "all on".
func (m *MidiEngine) exportPatch() ([]string, map[string][]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var routes []string
	for i := 0; i < len(m.routes); i++ {
		inName := trackerInputName
		if i >= 1 {
			inName = m.ins[i-1].name
		}
		for o := 0; o < len(m.routes[i]); o++ {
			if m.routes[i][o] {
				routes = append(routes, inName+">>"+m.outs[o].name)
			}
		}
	}
	filters := map[string][]int{}
	for o := 0; o < len(m.filter); o++ {
		var on []int
		for c, v := range m.filter[o] {
			if v {
				on = append(on, c)
			}
		}
		if len(on) != 16 { // only persist non-default filters
			filters[m.outs[o].name] = on
		}
	}
	return routes, filters
}

// applyPatch rebuilds the matrix from saved name pairs and channel filters.
func (m *MidiEngine) applyPatch(routes []string, filters map[string][]int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	inIdx := func(name string) int {
		if name == trackerInputName {
			return 0
		}
		for i, d := range m.ins {
			if d.name == name {
				return i + 1
			}
		}
		return -1
	}
	outIdx := func(name string) int {
		for o, d := range m.outs {
			if d.name == name {
				return o
			}
		}
		return -1
	}
	// Clear existing routes.
	for i := range m.routes {
		for o := range m.routes[i] {
			m.routes[i][o] = false
		}
	}
	for _, r := range routes {
		parts := strings.SplitN(r, ">>", 2)
		if len(parts) != 2 {
			continue
		}
		i, o := inIdx(parts[0]), outIdx(parts[1])
		if i >= 0 && o >= 0 {
			m.routes[i][o] = true
		}
	}
	for name, chans := range filters {
		o := outIdx(name)
		if o < 0 {
			continue
		}
		for c := range m.filter[o] {
			m.filter[o][c] = false
		}
		for _, c := range chans {
			if c >= 0 && c < 16 {
				m.filter[o][c] = true
			}
		}
	}
}
