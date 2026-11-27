# Third-Party Notices

The Smart-Serve container image bundles the following third-party software.

## smartmontools (`smartctl`, `smartd`)

- **License:** GNU General Public License v2 (GPL-2.0)
- **Copyright:** Bruce Allen, Christian Franke, and the smartmontools contributors
- **Upstream:** https://www.smartmontools.org/
- **Source:** https://sourceforge.net/projects/smartmontools/files/

Smart-Serve invokes `smartctl` as a **separate process**; the Smart-Serve source
code is not a derivative work of smartmontools and is licensed separately under the
MIT License (see `LICENSE`). The GPL-2.0 text is available at
https://www.gnu.org/licenses/old-licenses/gpl-2.0.html and is included with the
distribution package inside the image.

## util-linux (`lsblk`)

- **License:** GPL-2.0 / LGPL-2.1 (component-dependent)
- **Upstream:** https://www.kernel.org/pub/linux/utils/util-linux/

See `docs/DECISIONS.md` ADR-012 for the licensing rationale.

## Go dependencies linked into the binary

These eight modules are compiled into the `smart-serve` binary. The list is what
`go list -deps ./cmd/smartserve` reports, not what `go.mod` declares: test-only modules and the
compiler tooling in the module graph are not shipped, and this file describes what ships.

| Module | Version | Licence |
|---|---|---|
| `modernc.org/sqlite` | v1.61.0 | BSD-3-Clause |
| `modernc.org/libc` | v1.79.0 | BSD-3-Clause |
| `modernc.org/memory` | v1.12.1 | BSD-3-Clause |
| `modernc.org/mathutil` | v1.7.1 | BSD-3-Clause |
| `github.com/google/uuid` | v1.6.0 | BSD-3-Clause |
| `github.com/remyoudompheng/bigfft` | module pseudo-version, see `go.mod` | BSD-3-Clause |
| `golang.org/x/sys` | v0.49.0 | BSD-3-Clause |
| `github.com/dustin/go-humanize` | v1.1.0 | MIT |

None is GPL or LGPL, so nothing here reaches the separate-process reasoning that
applies to `smartctl` and `lsblk` above, and the MIT licence of Smart-Serve is unaffected.
The full licence text and copyright line for each module is distributed with the module
itself in the Go module cache and in the image build, where `go mod download` retains it.

### BSD-3-Clause

Applies to the six modules marked BSD-3-Clause above.

```
Redistribution and use in source and binary forms, with or without modification, are
permitted provided that the following conditions are met:

1. Redistributions of source code must retain the above copyright notice, this list of
   conditions and the following disclaimer.

2. Redistributions in binary form must reproduce the above copyright notice, this list
   of conditions and the following disclaimer in the documentation and/or other
   materials provided with the distribution.

3. Neither the name of the copyright holder nor the names of its contributors may be
   used to endorse or promote products derived from this software without specific
   prior written permission.

THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS" AND ANY
EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE IMPLIED WARRANTIES OF
MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE ARE DISCLAIMED. IN NO EVENT SHALL
THE COPYRIGHT HOLDER OR CONTRIBUTORS BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL,
SPECIAL, EXEMPLARY, OR CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO,
PROCUREMENT OF SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY OUT OF THE USE
OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF SUCH DAMAGE.
```

### MIT, for `github.com/dustin/go-humanize`

Copyright (c) 2012 Dustin Sallings

```
Permission is hereby granted, free of charge, to any person obtaining a copy of this
software and associated documentation files (the "Software"), to deal in the Software
without restriction, including without limitation the rights to use, copy, modify,
merge, publish, distribute, sublicense, and/or sell copies of the Software, and to
permit persons to whom the Software is furnished to do so, subject to the following
conditions:

The above copyright notice and this permission notice shall be included in all copies
or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED,
INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR
PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE
LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT
OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR
OTHER DEALINGS IN THE SOFTWARE.
```
