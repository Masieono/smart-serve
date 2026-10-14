package smart

import "strings"

// FailureCause explains why a S.M.A.R.T. read produced nothing.
//
// It exists because three completely different problems come back from smartctl as the
// same "Operation not permitted", and they need three different fixes. Passing the raw
// text through leaves the user to work out which; naming the cause lets the UI say which
// line to add to a compose file.
type FailureCause string

const (
	// CauseNone means the read worked.
	CauseNone FailureCause = ""

	// CauseDeviceBlocked means the device node could not be *opened* at all. In a
	// container that is almost always the device cgroup: /dev is bind-mounted so the
	// node is visible, but Docker's default allow-list denies opening it. AppArmor
	// produces the same thing.
	CauseDeviceBlocked FailureCause = "device_blocked"

	// CauseQueryBlocked means the node opened but the S.M.A.R.T. query was refused — a
	// missing capability. Native NVMe needs CAP_SYS_ADMIN; a drive behind a USB bridge
	// needs CAP_SYS_RAWIO for SG_IO passthrough.
	CauseQueryBlocked FailureCause = "query_blocked"

	// CauseUnsupported means the drive genuinely does not implement S.M.A.R.T. Most USB
	// flash drives and card readers do not.
	CauseUnsupported FailureCause = "unsupported"

	// CauseMissing means the device node was not there — unplugged between the lsblk
	// listing and the read.
	CauseMissing FailureCause = "missing"

	// CauseUnknown is a failure that cannot be attributed.
	CauseUnknown FailureCause = "unknown"
)

// ClassifyFailure reads smartctl's own words and names the cause.
//
// Every string matched below was observed on a real system rather than invented, which
// is why this is substring matching on smartctl's phrasing: none of it is exposed as a
// structured field.
func ClassifyFailure(text string) FailureCause {
	t := strings.ToLower(strings.TrimSpace(text))
	switch {
	case t == "":
		return CauseNone

	// The node itself could not be opened.
	case strings.Contains(t, "open device") && strings.Contains(t, "failed"):
		if isPermission(t) {
			return CauseDeviceBlocked
		}
		return CauseMissing

	// It opened, but a query was refused — always a capability.
	case isPermission(t):
		return CauseQueryBlocked

	case strings.Contains(t, "no such file or directory"),
		strings.Contains(t, "cannot open"),
		strings.Contains(t, "device not connected"),
		strings.Contains(t, "not found"):
		return CauseMissing

	case strings.Contains(t, "smart support is: unavailable"),
		strings.Contains(t, "smart support is: disabled"),
		strings.Contains(t, "unknown usb bridge"),
		strings.Contains(t, "no s.m.a.r.t. data reported"),
		strings.Contains(t, "unavailable"):
		return CauseUnsupported
	}
	return CauseUnknown
}

// Blocked reports whether the cause is a permissions obstacle rather than a property of
// the drive. A blocked drive may be perfectly healthy; an unsupported one has nothing to
// report. Conflating the two is what makes "this drive does not report S.M.A.R.T." a
// misleading thing to show a container user.
func (c FailureCause) Blocked() bool {
	return c == CauseDeviceBlocked || c == CauseQueryBlocked
}

// Hint names the fix, for a log line or an operator who wants the short version.
func (c FailureCause) Hint() string {
	switch c {
	case CauseDeviceBlocked:
		return "the container cannot open device nodes; add " +
			"device_cgroup_rules: ['a *:* rwm'] to the service (docs/DEPLOYMENT.md)"
	case CauseQueryBlocked:
		return "the device opened but the query was refused; add " +
			"cap_add: [SYS_ADMIN, SYS_RAWIO] to the service (docs/DEPLOYMENT.md)"
	case CauseUnsupported:
		return "this drive does not implement S.M.A.R.T.; nothing to fix"
	case CauseMissing:
		return "the device node is gone; replug the drive or rescan"
	}
	return ""
}

func isPermission(t string) bool {
	return strings.Contains(t, "operation not permitted") ||
		strings.Contains(t, "permission denied") ||
		strings.Contains(t, "access denied")
}
