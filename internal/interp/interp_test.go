package interp

import (
	"testing"

	"github.com/masieono/smart-serve/internal/model"
)

func boolPtr(b bool) *bool { return &b }
func intPtr(i int) *int    { return &i }

func attr(id int, raw uint64) model.Attribute {
	return model.Attribute{ID: id, RawValue: raw}
}

func TestEvaluateATA(t *testing.T) {
	tests := []struct {
		name    string
		data    *model.SmartData
		verdict model.Verdict
		score   int
	}{
		{
			name:    "healthy ssd",
			data:    &model.SmartData{Bus: model.BusATA, Passed: boolPtr(true), Attributes: []model.Attribute{attr(5, 0), attr(197, 0), attr(198, 0)}},
			verdict: model.Good,
			score:   100,
		},
		{
			name:    "one pending sector is caution",
			data:    &model.SmartData{Bus: model.BusATA, Passed: boolPtr(true), Attributes: []model.Attribute{attr(197, 1)}},
			verdict: model.Caution,
			score:   74,
		},
		{
			name:    "ten pending sectors is bad",
			data:    &model.SmartData{Bus: model.BusATA, Passed: boolPtr(true), Attributes: []model.Attribute{attr(197, 10)}},
			verdict: model.Bad,
			score:   39,
		},
		{
			name:    "smart self-assessment failure caps at ten",
			data:    &model.SmartData{Bus: model.BusATA, Passed: boolPtr(false)},
			verdict: model.Bad,
			score:   10,
		},
		{
			name:    "crc errors alone are caution",
			data:    &model.SmartData{Bus: model.BusATA, Passed: boolPtr(true), Attributes: []model.Attribute{attr(199, 4)}},
			verdict: model.Caution,
			score:   74,
		},
		{
			name:    "high temperature is caution",
			data:    &model.SmartData{Bus: model.BusATA, Passed: boolPtr(true), TemperatureC: intPtr(60)},
			verdict: model.Caution,
			score:   74,
		},
		{
			name:    "packed raw value is unpacked",
			data:    &model.SmartData{Bus: model.BusATA, Passed: boolPtr(true), Attributes: []model.Attribute{attr(197, 327680)}},
			verdict: model.Caution,
			score:   74,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Evaluate(tc.data, DefaultThresholds())
			if got.Verdict != tc.verdict {
				t.Errorf("verdict = %q, want %q (%s)", got.Verdict, tc.verdict, got.Rationale)
			}
			if got.Score != tc.score {
				t.Errorf("score = %d, want %d (%s)", got.Score, tc.score, got.Rationale)
			}
		})
	}
}

func TestEvaluateNVMe(t *testing.T) {
	tests := []struct {
		name    string
		log     *model.NVMeHealth
		verdict model.Verdict
		score   int
	}{
		{
			name:    "healthy",
			log:     &model.NVMeHealth{AvailableSpare: 100, AvailableSpareThresh: 10, PercentageUsed: 4},
			verdict: model.Good,
			score:   100,
		},
		{
			name:    "worn but within limits",
			log:     &model.NVMeHealth{AvailableSpare: 100, AvailableSpareThresh: 10, PercentageUsed: 60},
			verdict: model.Good,
			score:   78,
		},
		{
			name:    "nearly worn out is caution",
			log:     &model.NVMeHealth{AvailableSpare: 100, AvailableSpareThresh: 10, PercentageUsed: 92},
			verdict: model.Caution,
			score:   65,
		},
		{
			name:    "critical warning is bad",
			log:     &model.NVMeHealth{CriticalWarning: 1, AvailableSpare: 100, AvailableSpareThresh: 10},
			verdict: model.Bad,
			score:   39,
		},
		{
			name:    "spare below threshold is bad",
			log:     &model.NVMeHealth{AvailableSpare: 5, AvailableSpareThresh: 10, PercentageUsed: 20},
			verdict: model.Bad,
			score:   39,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sd := &model.SmartData{Bus: model.BusNVMe, NVMe: tc.log}
			got := Evaluate(sd, DefaultThresholds())
			if got.Verdict != tc.verdict {
				t.Errorf("verdict = %q, want %q (%s)", got.Verdict, tc.verdict, got.Rationale)
			}
			if got.Score != tc.score {
				t.Errorf("score = %d, want %d (%s)", got.Score, tc.score, got.Rationale)
			}
		})
	}
}

// Metrics feed the device detail page: throughput, power cycles and so on.
func TestDeriveMetricsNVMe(t *testing.T) {
	sd := &model.SmartData{NVMe: &model.NVMeHealth{
		PowerOnHours:     31603,
		PowerCycles:      289,
		DataUnitsRead:    2000000,
		DataUnitsWritten: 1000000,
	}}
	m := DeriveMetrics(sd)

	// One NVMe data unit is 1,000 sectors of 512 bytes.
	if m.DataReadBytes == nil || *m.DataReadBytes != 2000000*512000 {
		t.Errorf("data read = %v, want %d", m.DataReadBytes, 2000000*512000)
	}
	if m.DataWrittenBytes == nil || *m.DataWrittenBytes != 1000000*512000 {
		t.Errorf("data written = %v, want %d", m.DataWrittenBytes, 1000000*512000)
	}
	if m.PowerOnHours == nil || *m.PowerOnHours != 31603 {
		t.Errorf("power-on hours = %v, want 31603", m.PowerOnHours)
	}
	if m.PowerCycles == nil || *m.PowerCycles != 289 {
		t.Errorf("power cycles = %v, want 289", m.PowerCycles)
	}
}

func TestDeriveMetricsATA(t *testing.T) {
	sd := &model.SmartData{Attributes: []model.Attribute{
		{ID: 9, RawValue: 12345},
		{ID: 12, RawValue: 42},
		{ID: 241, RawValue: 1000000},
		{ID: 242, RawValue: 2000000},
		{ID: 231, Value: 94},
	}}
	m := DeriveMetrics(sd)

	if m.PowerOnHours == nil || *m.PowerOnHours != 12345 {
		t.Errorf("power-on hours = %v, want 12345 (attribute 9)", m.PowerOnHours)
	}
	if m.PowerCycles == nil || *m.PowerCycles != 42 {
		t.Errorf("power cycles = %v, want 42 (attribute 12)", m.PowerCycles)
	}
	// ATA LBAs are 512-byte sectors.
	if m.DataWrittenBytes == nil || *m.DataWrittenBytes != 1000000*512 {
		t.Errorf("data written = %v, want %d", m.DataWrittenBytes, 1000000*512)
	}
	if m.DataReadBytes == nil || *m.DataReadBytes != 2000000*512 {
		t.Errorf("data read = %v, want %d", m.DataReadBytes, 2000000*512)
	}
	if m.LifeLeftPct == nil || *m.LifeLeftPct != 94 {
		t.Errorf("life left = %v, want 94 (attribute 231)", m.LifeLeftPct)
	}
}

func TestDeriveMetricsHandlesMissingData(t *testing.T) {
	if m := DeriveMetrics(nil); m.PowerOnHours != nil || m.DataReadBytes != nil {
		t.Errorf("nil payload should yield empty metrics, got %+v", m)
	}
	if m := DeriveMetrics(&model.SmartData{}); m.PowerOnHours != nil || m.PowerCycles != nil {
		t.Errorf("empty payload should yield empty metrics, got %+v", m)
	}
}

func TestEffectiveCount(t *testing.T) {
	tests := []struct {
		raw  uint64
		want uint64
	}{
		{0, 0},
		{5, 5},
		{65535, 65535},
		{327680, 5},             // 0x0000000000050000 — packed, low word zero
		{100000, 100000},        // low word non-zero: genuine count, not packed
		{0x0000000500000000, 5}, // counter in the third word
	}
	for _, tc := range tests {
		if got := effectiveCount(tc.raw); got != tc.want {
			t.Errorf("effectiveCount(%d) = %d, want %d", tc.raw, got, tc.want)
		}
	}
}

func TestEvaluateNilIsUnknown(t *testing.T) {
	got := Evaluate(nil, DefaultThresholds())
	if got.Verdict != model.Unknown {
		t.Errorf("verdict = %q, want %q", got.Verdict, model.Unknown)
	}
	if got.Score != -1 {
		t.Errorf("score = %d, want -1", got.Score)
	}
}

// A device we could not read must never be reported as healthy.
func TestEvaluateNoDataIsUnknown(t *testing.T) {
	got := Evaluate(&model.SmartData{Messages: []string{"error: device open failed"}}, DefaultThresholds())
	if got.Verdict != model.Unknown {
		t.Errorf("verdict = %q, want unknown", got.Verdict)
	}
	if got.Score != -1 {
		t.Errorf("score = %d, want -1", got.Score)
	}
	if got.Rationale != "error: device open failed" {
		t.Errorf("rationale = %q", got.Rationale)
	}
}
