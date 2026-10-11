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
