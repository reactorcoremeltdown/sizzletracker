# Third-party notices

sizzletracker is distributed under the GPL-3.0 (see [`../LICENSE`](../LICENSE)).
It incorporates and links the components listed below, each under its own
permissive license. Those licenses require that their copyright and permission
notices be preserved; this file does so.

When shipping a **compiled binary**, include this file and the PortMidi license
from the system package you built against.

---

## Go libraries compiled into the binary

### github.com/gdamore/tcell/v2 — Apache License 2.0
Copyright The TCell Authors (Garrett D'Amore and contributors).
Terminal rendering and input. Full text: <https://www.apache.org/licenses/LICENSE-2.0>.

### github.com/gdamore/encoding — Apache License 2.0
Copyright Garrett D'Amore and contributors.
Character-set encodings used by tcell.

### github.com/mattn/go-runewidth — MIT License
Copyright (c) 2016 Yasuhiro Matsumoto.

### github.com/clipperhouse/uax29/v2 — MIT License
Copyright (c) 2020 Matt Sherman.

### github.com/lucasb-eyer/go-colorful — MIT License
Copyright (c) 2013 Lucas Beyer.

### github.com/rivo/uniseg — MIT License
Copyright (c) 2019 Oliver Kuederle.

### golang.org/x/sys, golang.org/x/term, golang.org/x/text — BSD 3-Clause License
Copyright (c) The Go Authors.

### Go standard library and runtime — BSD 3-Clause License
Copyright (c) The Go Authors.

> The standard MIT permission notice applies to each MIT component above:
> permission is granted, free of charge, to use, copy, modify, merge, publish,
> distribute, sublicense and/or sell copies, provided the copyright notice and
> this permission notice are included. The software is provided "as is", without
> warranty of any kind.

---

## Vendored source (in this repository)

### internal/portmidi — Apache License 2.0
A lightly modified copy of `github.com/rakyll/portmidi`.
Copyright 2013 Google Inc. All rights reserved.
The only change from upstream is the platform-aware cgo build directives in
[`../internal/portmidi/portmidi.go`](../internal/portmidi/portmidi.go); the full
license text is kept at
[`../internal/portmidi/LICENSE`](../internal/portmidi/LICENSE).

---

## Runtime native dependency (not redistributed here)

### PortMidi (`libportmidi`) — permissive, MIT-style
Copyright (c) Roger B. Dannenberg and contributors.
Cross-platform MIDI I/O, linked via cgo. Installed separately (e.g.
`brew install portmidi`, `apt-get install libportmidi-dev`). Its license ships
with that package; include it when distributing binaries that link PortMidi.

---

## Module-graph entries not compiled in

`github.com/yuin/goldmark`, `golang.org/x/crypto`, `golang.org/x/mod`,
`golang.org/x/net`, `golang.org/x/sync`, `golang.org/x/tools`,
`golang.org/x/xerrors` appear in `go.sum` as transitive test/tooling
dependencies of the libraries above. They are not part of the sizzletracker
binary. All are BSD-3-Clause or MIT.
