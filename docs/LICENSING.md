# Licensing analysis

This document records the open-source licensing review carried out before
release. The short answer: **sizzletracker can be released under the GNU
General Public License, version 3 (GPL-3.0).** Every dependency is permissively
licensed and one-way compatible with the GPLv3. There are no incompatibilities
to resolve.

> This is an engineering analysis, not legal advice. If the project is being
> released by or on behalf of an organisation, have counsel confirm it.

---

## 1. What "compatible with GPLv3" means

The project's own source (every `*.go` file outside `internal/portmidi/`) is
original work; the copyright holder may license it however they wish, including
GPL-3.0.

The constraint comes from the third-party code that is **combined** with it —
compiled into the binary or linked at runtime. A license is "compatible with
the GPLv3" if its code may be incorporated into a larger work that is
distributed under the GPLv3 as a whole. Permissive licenses (MIT, BSD,
Apache-2.0) all allow this; the combined work is then governed by the GPLv3,
while the permissive components keep their own notices.

The Free Software Foundation publishes the compatibility matrix used here:
<https://www.gnu.org/licenses/license-list.html>.

---

## 2. Dependency inventory

All versions are taken from [`go.mod`](../go.mod) / [`go.sum`](../go.sum) at the
time of review.

### Compiled into the binary

| Component | Version | License | How it is linked | GPLv3-compatible |
|---|---|---|---|---|
| `github.com/gdamore/tcell/v2` | 2.13.10 | Apache-2.0 | static (Go) | Yes |
| `github.com/gdamore/encoding` | 1.0.1 | Apache-2.0 | static (via tcell) | Yes |
| `github.com/mattn/go-runewidth` | 0.0.24 | MIT | static (Go) | Yes |
| `github.com/clipperhouse/uax29/v2` | 2.2.0 | MIT | static (via tcell) | Yes |
| `github.com/lucasb-eyer/go-colorful` | 1.3.0 | MIT | static (via tcell) | Yes |
| `github.com/rivo/uniseg` | 0.4.7 | MIT | static (via runewidth) | Yes |
| `golang.org/x/sys` | 0.38.0 | BSD-3-Clause | static (Go) | Yes |
| `golang.org/x/term` | 0.37.0 | BSD-3-Clause | static (Go) | Yes |
| `golang.org/x/text` | 0.31.0 | BSD-3-Clause | static (via tcell/encoding) | Yes |
| `internal/portmidi` (vendored from `github.com/rakyll/portmidi`) | — | Apache-2.0 | static (in-tree copy) | Yes |
| Go standard library & runtime | go 1.24 | BSD-3-Clause | static | Yes |

### Linked at runtime (cgo)

| Component | License | How it is linked | GPLv3-compatible |
|---|---|---|---|
| PortMidi C library (`libportmidi`) | permissive, MIT-style | dynamic, via cgo | Yes |

PortMidi ships its own short permissive license (MIT-style; some distributions
tag recent releases as Apache-2.0). Either way it is GPL-compatible. Because it
is a separately-installed system library that is *dynamically* linked, it is not
redistributed in this repository — but anyone shipping a compiled binary should
include PortMidi's license alongside it (see
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)).

### Present in `go.sum` but not in the binary

`go.sum` also lists `github.com/yuin/goldmark`, `golang.org/x/crypto`,
`golang.org/x/mod`, `golang.org/x/net`, `golang.org/x/sync`,
`golang.org/x/tools` and `golang.org/x/xerrors`. These are transitive entries in
the module graph (test and tooling dependencies of the libraries above); they
are **not compiled into sizzletracker**. All are BSD-3-Clause or MIT, so they
would be compatible even if they were.

---

## 3. Compatibility verdict

| License in use | Direction | Result |
|---|---|---|
| MIT | MIT → GPLv3 | Compatible (permissive) |
| BSD-3-Clause | BSD → GPLv3 | Compatible (permissive) |
| Apache-2.0 | Apache-2.0 → GPLv3 | **Compatible** (FSF-confirmed, one-way) |

The only nuance is **Apache-2.0** (used by tcell, gdamore/encoding and the
vendored PortMidi bindings). Apache-2.0 is explicitly compatible with **GPLv3**,
but it is *not* compatible with **GPLv2** because of Apache's patent-termination
clause. This rules out GPLv2-only, but GPL-3.0 (and "GPL-3.0-or-later") are
fine.

**Conclusion: release under GPL-3.0 is viable with no changes to dependencies.**

---

## 4. Obligations that come with the GPLv3 release

Choosing GPLv3 is allowed; these are the conditions to honour when distributing:

1. **Ship the license.** A verbatim copy of the GPLv3 lives at
   [`../LICENSE`](../LICENSE). Keep it in the repository and in any source
   distribution.
2. **State the license per file (recommended).** Add a short header to each
   source file (see the snippet in §6). This is best practice, not a hard
   requirement — see "Recommended next steps".
3. **Preserve upstream notices.** The permissive licenses (MIT, BSD, Apache-2.0)
   require that their copyright and permission notices travel with the code.
   They are collected in
   [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Apache-2.0 additionally
   asks that any upstream `NOTICE` file be reproduced; none of the Apache-2.0
   dependencies ship a `NOTICE`, so there is nothing extra to carry.
4. **Offer source for binaries.** If you distribute compiled binaries, you must
   make the corresponding source (this repository at the built revision)
   available under the GPLv3.

The combined program is GPLv3; the bundled permissive components remain under
their own licenses internally. That is the normal and intended arrangement.

---

## 5. Alternatives (if copyleft is not desired)

Because **every dependency is permissive**, nothing forces a copyleft license.
If wider adoption / embedding in closed products were a goal instead, the
project could equally be released under a permissive license:

- **MIT** — simplest, maximum adoption, matches the spirit of most deps.
- **Apache-2.0** — like MIT but with an explicit patent grant; aligns with the
  largest dependency (tcell) and the vendored PortMidi bindings.

These are offered only as options. The reviewed and recommended choice for this
release is **GPL-3.0**, which is fully supported by the dependency set.

---

## 6. Recommended next steps (optional polish)

- Add an SPDX header to each first-party source file:

  ```go
  // SPDX-License-Identifier: GPL-3.0-or-later
  // Copyright (C) 2026 <author>
  ```

- Fill in the real copyright holder / year in `LICENSE`'s "how to apply"
  section and in any file headers.
- When publishing release binaries, bundle `LICENSE`,
  `docs/THIRD_PARTY_NOTICES.md`, and the PortMidi license file from the system
  package used to build them.
