package main

import (
	"sync"
	"time"

	"github.com/rakyll/portmidi"
)

// portDevice pairs a PortMidi device id with its display name.
type portDevice struct {
	id   portmidi.DeviceID
	name string
}

// MidiEngine wraps PortMidi output/input. Output is monophonic-agnostic (the
// player decides note lifecycles); input feeds the punch-in recorder.
type MidiEngine struct {
	mu sync.Mutex

	available bool // PortMidi initialised successfully

	outs     []portDevice
	outIndex int
	outStr   *portmidi.Stream

	ins     []portDevice
	inIndex int
	inStr   *portmidi.Stream
	inStop  chan struct{}

	// onIn reports note events from the input device: on=true for note-on
	// (velocity > 0), on=false for note-off (or note-on with velocity 0).
	onIn func(on bool, note, vel int)
}

func newMidiEngine() *MidiEngine {
	m := &MidiEngine{outIndex: -1, inIndex: -1}
	if err := portmidi.Initialize(); err != nil {
		return m // available stays false; app runs silently
	}
	m.available = true
	m.refreshPorts()
	if len(m.outs) > 0 {
		// Prefer the system default output if present.
		def := portmidi.DefaultOutputDeviceID()
		sel := 0
		for i, d := range m.outs {
			if d.id == def {
				sel = i
				break
			}
		}
		m.selectOut(sel)
	}
	return m
}

func (m *MidiEngine) refreshPorts() {
	m.outs = m.outs[:0]
	m.ins = m.ins[:0]
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

func (m *MidiEngine) OutName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.available {
		return "<no portmidi>"
	}
	if m.outIndex < 0 || m.outIndex >= len(m.outs) {
		return "<none>"
	}
	return m.outs[m.outIndex].name
}

func (m *MidiEngine) InName() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.inIndex < 0 || m.inIndex >= len(m.ins) {
		return "<off>"
	}
	return m.ins[m.inIndex].name
}

func (m *MidiEngine) numOut() int { m.mu.Lock(); defer m.mu.Unlock(); return len(m.outs) }
func (m *MidiEngine) numIn() int  { m.mu.Lock(); defer m.mu.Unlock(); return len(m.ins) }

func (m *MidiEngine) selectOut(idx int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if idx < 0 || idx >= len(m.outs) {
		return
	}
	if m.outStr != nil {
		m.outStr.Close()
		m.outStr = nil
	}
	str, err := portmidi.NewOutputStream(m.outs[idx].id, 1024, 0)
	if err != nil {
		m.outIndex = -1
		return
	}
	m.outStr = str
	m.outIndex = idx
}

func (m *MidiEngine) cycleOut() {
	n := m.numOut()
	if n == 0 {
		return
	}
	m.mu.Lock()
	next := (m.outIndex + 1) % n
	m.mu.Unlock()
	m.selectOut(next)
}

// selectIn opens input port idx (-1 == off) and spawns a poll loop that routes
// note-on events to the callback.
func (m *MidiEngine) selectIn(idx int) {
	m.mu.Lock()
	if m.inStop != nil {
		close(m.inStop)
		m.inStop = nil
	}
	if m.inStr != nil {
		m.inStr.Close()
		m.inStr = nil
	}
	m.inIndex = -1
	cb := m.onIn
	m.mu.Unlock()

	if idx < 0 || idx >= m.numIn() {
		return
	}

	m.mu.Lock()
	dev := m.ins[idx].id
	m.mu.Unlock()

	str, err := portmidi.NewInputStream(dev, 1024)
	if err != nil {
		return
	}
	stop := make(chan struct{})

	m.mu.Lock()
	m.inStr = str
	m.inStop = stop
	m.inIndex = idx
	m.mu.Unlock()

	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			events, err := str.Read(64)
			if err == nil && cb != nil {
				for _, e := range events {
					status := e.Status & 0xf0
					switch {
					case status == 0x90 && e.Data2 > 0:
						cb(true, int(e.Data1), int(e.Data2))
					case status == 0x80 || (status == 0x90 && e.Data2 == 0):
						cb(false, int(e.Data1), int(e.Data2))
					}
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
}

// cycleIn rotates input: off -> 0 -> 1 -> ... -> off.
func (m *MidiEngine) cycleIn() {
	n := m.numIn()
	if n == 0 {
		return
	}
	m.mu.Lock()
	cur := m.inIndex
	m.mu.Unlock()
	next := cur + 1
	if next >= n {
		next = -1
	}
	m.selectIn(next)
}

func (m *MidiEngine) setInputCallback(cb func(on bool, note, vel int)) {
	m.mu.Lock()
	m.onIn = cb
	m.mu.Unlock()
}

func (m *MidiEngine) noteOn(ch, note, vel int) {
	m.mu.Lock()
	str := m.outStr
	m.mu.Unlock()
	if str == nil {
		return
	}
	str.WriteShort(int64(0x90|(ch&0x0f)), int64(note&0x7f), int64(vel&0x7f))
}

func (m *MidiEngine) noteOff(ch, note int) {
	m.mu.Lock()
	str := m.outStr
	m.mu.Unlock()
	if str == nil {
		return
	}
	str.WriteShort(int64(0x80|(ch&0x0f)), int64(note&0x7f), 0)
}

// allNotesOff sends CC 123 on all channels.
func (m *MidiEngine) allNotesOff() {
	m.mu.Lock()
	str := m.outStr
	m.mu.Unlock()
	if str == nil {
		return
	}
	for ch := 0; ch < 16; ch++ {
		str.WriteShort(int64(0xB0|ch), 123, 0)
	}
}

func (m *MidiEngine) close() {
	m.allNotesOff()
	m.mu.Lock()
	if m.inStop != nil {
		close(m.inStop)
		m.inStop = nil
	}
	if m.inStr != nil {
		m.inStr.Close()
	}
	if m.outStr != nil {
		m.outStr.Close()
	}
	avail := m.available
	m.mu.Unlock()
	if avail {
		portmidi.Terminate()
	}
}
