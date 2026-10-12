// Package interp turns normalized S.M.A.R.T. data into a human-facing verdict and
// a 0-100 health score. It is the normative implementation of
// docs/SMART_INTERPRETATION.md sections 4 and 8.
package interp

import (
	"fmt"
	"math"
	"math/bits"
	"strconv"
	"strings"

	"github.com/masieono/smart-serve/internal/model"
)

// Thresholds are the user-tunable interpretation parameters (docs/DATA_MODEL.md).
type Thresholds struct {
	TemperatureC           int
	ReallocatedBad         int
	PendingBad             int
	UncorrectableBad       int
	WarnOnAnyNonZero       bool
	NVMePercentageUsedWarn int
}

// DefaultThresholds returns the shipped defaults (docs/DATA_MODEL.md).
func DefaultThresholds() Thresholds {
	return Thresholds{
		TemperatureC:           55,
		ReallocatedBad:         10,
		PendingBad:             10,
		UncorrectableBad:       10,
		WarnOnAnyNonZero:       true,
		NVMePercentageUsedWarn: 90,
	}
}

// FromSettings builds the thresholds from the settings map, falling back to the
// shipped default for anything absent or unparseable.
//
// This is what makes the Settings page real: without it the scanner would evaluate
// every drive against the defaults and every threshold the user tuned would be
// silently ignored (ADR-009 says thresholds live in the database and are edited in
// the UI).
func FromSettings(settings map[string]string) Thresholds {
	t := DefaultThresholds()
	if v, ok := settingInt(settings, "temperature_threshold_c"); ok {
		t.TemperatureC = v
	}
	if v, ok := settingInt(settings, "reallocated_bad_threshold"); ok {
		t.ReallocatedBad = v
	}
	if v, ok := settingInt(settings, "pending_bad_threshold"); ok {
		t.PendingBad = v
	}
	if v, ok := settingInt(settings, "uncorrectable_bad_threshold"); ok {
		t.UncorrectableBad = v
	}
	if v, ok := settingInt(settings, "nvme_percentage_used_warn"); ok {
		t.NVMePercentageUsedWarn = v
	}
	if v, ok := settingBool(settings, "warn_on_any_nonzero"); ok {
		t.WarnOnAnyNonZero = v
	}
	return t
}

// settingInt reads a positive integer setting.
func settingInt(settings map[string]string, key string) (int, bool) {
	raw, ok := settings[key]
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

// settingBool reads a boolean setting.
func settingBool(settings map[string]string, key string) (bool, bool) {
	raw, ok := settings[key]
	if !ok {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1", "yes", "on":
		return true, true
	case "false", "0", "no", "off":
		return false, true
	}
	return false, false
}

// Penalty is one line of the health-score breakdown.
type Penalty struct {
	Reason string  `json:"reason"`
	Points float64 `json:"points"`
}

// Result is the outcome of evaluating one device.
type Result struct {
	Verdict   model.Verdict `json:"verdict"`
	Rationale string        `json:"rationale"`
	Score     int           `json:"score"` // 0-100, or -1 when not applicable
	Penalties []Penalty     `json:"penalties"`
}

// Score caps (docs/SMART_INTERPRETATION.md 8.1): the verdict caps the score so the
// two never contradict. Penalties may push a Caution below its cap.
const (
	badCap     = 39.0
	cautionCap = 74.0
)

// Evaluate computes the verdict and score for a device.
//
// The available data decides the path: an NVMe health log selects the NVMe
// checks, while ATA attributes (or a self-assessment result) select the ATA
// checks. A device that reported no health data at all is Unknown — never Good,
// which would be a dangerous false positive for a health monitor.
func Evaluate(sd *model.SmartData, t Thresholds) Result {
	if sd == nil {
		return unknownResult("no S.M.A.R.T. data")
	}
	switch {
	case sd.NVMe != nil:
		return evaluateNVMe(sd, t)
	case len(sd.Attributes) > 0 || sd.Passed != nil:
		return evaluateATA(sd, t)
	default:
		msg := "no S.M.A.R.T. data reported"
		if len(sd.Messages) > 0 {
			msg = sd.Messages[0]
		}
		return unknownResult(msg)
	}
}

func unknownResult(reason string) Result {
	return Result{Verdict: model.Unknown, Rationale: reason, Score: -1}
}

func evaluateATA(sd *model.SmartData, t Thresholds) Result {
	// The drive's own self-assessment is authoritative.
	if sd.Passed != nil && !*sd.Passed {
		return Result{
			Verdict:   model.Bad,
			Rationale: "SMART self-assessment failed",
			Score:     10,
			Penalties: []Penalty{{Reason: "SMART self-assessment failed", Points: 90}},
		}
	}

	count := func(id int) uint64 {
		if a := sd.AttributeByID(id); a != nil {
			return effectiveCount(a.RawValue)
		}
		return 0
	}

	verdict := model.Good
	var why []string

	switch {
	case int(count(5)) >= t.ReallocatedBad:
		verdict = model.Bad
		why = append(why, fmt.Sprintf("%d reallocated sectors", count(5)))
	case int(count(197)) >= t.PendingBad:
		verdict = model.Bad
		why = append(why, fmt.Sprintf("%d pending sectors", count(197)))
	case int(count(198)) >= t.UncorrectableBad:
		verdict = model.Bad
		why = append(why, fmt.Sprintf("%d uncorrectable sectors", count(198)))
	}

	if verdict != model.Bad {
		if t.WarnOnAnyNonZero {
			if count(5) > 0 {
				verdict = model.Caution
				why = append(why, fmt.Sprintf("%d reallocated sectors", count(5)))
			}
			if count(197) > 0 {
				verdict = model.Caution
				why = append(why, fmt.Sprintf("%d pending sectors", count(197)))
			}
			if count(198) > 0 {
				verdict = model.Caution
				why = append(why, fmt.Sprintf("%d uncorrectable sectors", count(198)))
			}
		}
		if count(199) > 0 {
			verdict = model.Caution
			why = append(why, fmt.Sprintf("%d UDMA CRC errors — check cable", count(199)))
		}
		if count(10) > 0 {
			verdict = model.Caution
			why = append(why, fmt.Sprintf("%d spin retries", count(10)))
		}
		if count(187) > 0 {
			verdict = model.Caution
			why = append(why, fmt.Sprintf("%d reported uncorrectable errors", count(187)))
		}
		if count(196) > 0 {
			verdict = model.Caution
			why = append(why, fmt.Sprintf("%d reallocation events", count(196)))
		}
		if sd.TemperatureC != nil && *sd.TemperatureC >= t.TemperatureC {
			verdict = model.Caution
			why = append(why, fmt.Sprintf("temperature %d°C", *sd.TemperatureC))
		}
	}

	var pen []Penalty
	add := func(points, cap float64, reason string) {
		if points <= 0 {
			return
		}
		pen = append(pen, Penalty{Reason: reason, Points: math.Min(points, cap)})
	}
	add(float64(count(5)), 30, fmt.Sprintf("reallocated sectors (%d)", count(5)))
	add(float64(count(197))*2, 30, fmt.Sprintf("pending sectors (%d)", count(197)))
	add(float64(count(198))*2, 30, fmt.Sprintf("uncorrectable sectors (%d)", count(198)))
	add(float64(count(196))*0.5, 10, fmt.Sprintf("reallocation events (%d)", count(196)))
	add(float64(count(199))*0.5, 10, fmt.Sprintf("UDMA CRC errors (%d)", count(199)))
	add(float64(count(10))*3, 15, fmt.Sprintf("spin retries (%d)", count(10)))
	if sd.TemperatureC != nil && *sd.TemperatureC > t.TemperatureC {
		add(float64(*sd.TemperatureC-t.TemperatureC), 10, fmt.Sprintf("temperature (%d°C)", *sd.TemperatureC))
	}

	return finalize(verdict, why, pen)
}

func evaluateNVMe(sd *model.SmartData, t Thresholds) Result {
	n := sd.NVMe
	if n == nil {
		return Result{Verdict: model.Unknown, Rationale: "no NVMe health log", Score: -1}
	}

	verdict := model.Good
	var why []string
	var pen []Penalty

	if n.CriticalWarning != 0 {
		verdict = model.Bad
		why = append(why, fmt.Sprintf("NVMe critical warning 0x%x", n.CriticalWarning))
		pen = append(pen, Penalty{
			Reason: fmt.Sprintf("critical warning (0x%x)", n.CriticalWarning),
			Points: math.Min(40, float64(bits.OnesCount(uint(n.CriticalWarning)))*10),
		})
	}
	if n.AvailableSpare <= n.AvailableSpareThresh {
		verdict = model.Bad
		why = append(why, fmt.Sprintf("spare blocks below threshold (%d%%)", n.AvailableSpare))
	}

	if verdict != model.Bad {
		if n.PercentageUsed >= t.NVMePercentageUsedWarn {
			verdict = model.Caution
			why = append(why, fmt.Sprintf("%d%% lifetime used", n.PercentageUsed))
		}
		if sd.TemperatureC != nil && *sd.TemperatureC >= t.TemperatureC {
			verdict = model.Caution
			why = append(why, fmt.Sprintf("temperature %d°C", *sd.TemperatureC))
		}
	}

	if n.PercentageUsed > 5 {
		pen = append(pen, Penalty{
			Reason: fmt.Sprintf("lifetime used (%d%%)", n.PercentageUsed),
			Points: math.Min(45, float64(n.PercentageUsed-5)*0.4),
		})
	}
	if n.AvailableSpare < 100 {
		pen = append(pen, Penalty{
			Reason: fmt.Sprintf("spare blocks (%d%%)", n.AvailableSpare),
			Points: math.Min(30, float64(100-n.AvailableSpare)*0.3),
		})
	}
	if sd.TemperatureC != nil && *sd.TemperatureC > t.TemperatureC {
		pen = append(pen, Penalty{
			Reason: fmt.Sprintf("temperature (%d°C)", *sd.TemperatureC),
			Points: math.Min(10, float64(*sd.TemperatureC-t.TemperatureC)),
		})
	}

	return finalize(verdict, why, pen)
}

// finalize sums penalties, applies the verdict cap, and assembles the result.
func finalize(verdict model.Verdict, why []string, pen []Penalty) Result {
	score := 100.0
	for _, p := range pen {
		score -= p.Points
	}
	switch verdict {
	case model.Bad:
		score = math.Min(score, badCap)
	case model.Caution:
		score = math.Min(score, cautionCap)
	}
	if score < 0 {
		score = 0
	}
	rationale := "all checks passed"
	if len(why) > 0 {
		rationale = strings.Join(why, "; ")
	}
	return Result{
		Verdict:   verdict,
		Rationale: rationale,
		Score:     int(math.Round(score)),
		Penalties: pen,
	}
}

// Metrics are derived figures the UI wants but that are not stored as their own
// columns: media throughput, power cycles, and so on. They are computed from the
// raw S.M.A.R.T. payload so both ATA and NVMe devices are covered.
type Metrics struct {
	PowerOnHours     *int64  `json:"power_on_hours,omitempty"`
	PowerCycles      *int64  `json:"power_cycles,omitempty"`
	DataReadBytes    *uint64 `json:"data_read_bytes,omitempty"`
	DataWrittenBytes *uint64 `json:"data_written_bytes,omitempty"`
	LifeLeftPct      *int    `json:"life_left_pct,omitempty"`
	RotationRate     int     `json:"rotation_rate,omitempty"`
}

// DeriveMetrics extracts the extra figures from a normalized S.M.A.R.T. payload.
func DeriveMetrics(sd *model.SmartData) Metrics {
	if sd == nil {
		return Metrics{}
	}
	m := Metrics{
		PowerOnHours: sd.PowerOnHours,
		PowerCycles:  sd.PowerCycles,
		RotationRate: sd.RotationRate,
	}

	if n := sd.NVMe; n != nil {
		if m.PowerOnHours == nil && n.PowerOnHours > 0 {
			v := n.PowerOnHours
			m.PowerOnHours = &v
		}
		if m.PowerCycles == nil && n.PowerCycles > 0 {
			v := n.PowerCycles
			m.PowerCycles = &v
		}
		// NVMe reports data volume in 1,000-sector units.
		if n.DataUnitsRead > 0 {
			v := uint64(n.DataUnitsRead) * model.NVMeDataUnitBytes
			m.DataReadBytes = &v
		}
		if n.DataUnitsWritten > 0 {
			v := uint64(n.DataUnitsWritten) * model.NVMeDataUnitBytes
			m.DataWrittenBytes = &v
		}
		return m
	}

	// ATA: 9 power-on hours, 12 power cycles, 241/242 LBAs written/read (512-byte
	// sectors), 231 SSD life left (value is the percentage remaining).
	count := func(id int) (uint64, bool) {
		if a := sd.AttributeByID(id); a != nil {
			return effectiveCount(a.RawValue), true
		}
		return 0, false
	}
	if m.PowerOnHours == nil {
		if v, ok := count(9); ok {
			h := int64(v)
			m.PowerOnHours = &h
		}
	}
	if m.PowerCycles == nil {
		if v, ok := count(12); ok {
			c := int64(v)
			m.PowerCycles = &c
		}
	}
	if v, ok := count(242); ok && v > 0 {
		b := v * 512
		m.DataReadBytes = &b
	}
	if v, ok := count(241); ok && v > 0 {
		b := v * 512
		m.DataWrittenBytes = &b
	}
	if a := sd.AttributeByID(231); a != nil && a.Value > 0 {
		left := a.Value
		m.LifeLeftPct = &left
	}
	return m
}

// effectiveCount applies the packed-word rule (docs/SMART_INTERPRETATION.md 3):
// some vendors pack several 16-bit counters into one raw value. We only unpack
// when the value has the packed shape (lowest word zero, a higher word non-zero),
// which avoids the false negative where a genuine count such as 100000 would be
// misread as 1.
func effectiveCount(raw uint64) uint64 {
	if raw <= 0xFFFF {
		return raw
	}
	if raw&0xFFFF != 0 {
		return raw
	}
	for _, w := range []uint64{(raw >> 16) & 0xFFFF, (raw >> 32) & 0xFFFF, (raw >> 48) & 0xFFFF} {
		if w != 0 {
			return w
		}
	}
	return raw
}
