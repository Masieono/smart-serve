package store

import (
	"context"
	"testing"
	"time"
)

func TestSettings(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.SetSetting(ctx, "temperature_threshold_c", "55"); err != nil {
		t.Fatalf("SetSetting: %v", err)
	}
	v, ok, err := s.GetSetting(ctx, "temperature_threshold_c")
	if err != nil || !ok || v != "55" {
		t.Fatalf("GetSetting = %q, %v, %v", v, ok, err)
	}

	// Upsert replaces.
	if err := s.SetSetting(ctx, "temperature_threshold_c", "60"); err != nil {
		t.Fatalf("SetSetting update: %v", err)
	}
	v, _, _ = s.GetSetting(ctx, "temperature_threshold_c")
	if v != "60" {
		t.Errorf("value = %q, want 60", v)
	}

	// Unknown key is not an error.
	if _, ok, err := s.GetSetting(ctx, "nope"); err != nil || ok {
		t.Errorf("unknown setting: ok=%v err=%v", ok, err)
	}

	if err := s.SetSettings(ctx, map[string]string{"retention_days": "180", "theme": "dark"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	all, err := s.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings: %v", err)
	}
	if len(all) != 3 || all["retention_days"] != "180" || all["theme"] != "dark" {
		t.Errorf("settings = %v", all)
	}
}

// A26. The projection window follows the user's retention setting, so what they keep is what the
// app reads — an owner who raised retention to five years to watch a slow trend was having that
// history discarded by the projection anyway.
func TestRetentionWindowFollowsTheSetting(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	// Nothing set: the documented default.
	if got := s.RetentionDays(ctx); got != DefaultRetentionDays {
		t.Errorf("RetentionDays = %d, want the default %d", got, DefaultRetentionDays)
	}
	if got := s.RetentionWindow(ctx); got != time.Duration(DefaultRetentionDays)*24*time.Hour {
		t.Errorf("RetentionWindow = %v", got)
	}

	// A user who keeps five years gets five years.
	if err := s.SetSettings(ctx, map[string]string{"retention_days": "1825"}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if got := s.RetentionDays(ctx); got != 1825 {
		t.Errorf("RetentionDays = %d, want 1825", got)
	}
	if got := s.RetentionWindow(ctx); got != 1825*24*time.Hour {
		t.Errorf("RetentionWindow = %v, want 1825 days", got)
	}
}

// A zero or unparseable setting must not mean "keep nothing": pruning the entire history would be a
// catastrophic reading of a typo, and the settings UI cannot produce a zero.
func TestAnUnusableRetentionSettingFallsBack(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	for _, bad := range []string{"0", "-5", "", "  ", "soon"} {
		if err := s.SetSettings(ctx, map[string]string{"retention_days": bad}); err != nil {
			t.Fatalf("SetSettings(%q): %v", bad, err)
		}
		if got := s.RetentionDays(ctx); got != DefaultRetentionDays {
			t.Errorf("retention_days = %q gave %d, want the default %d",
				bad, got, DefaultRetentionDays)
		}
	}
}

// Whitespace around a number is a paste artefact, not a different value.
func TestRetentionDaysToleratesWhitespace(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.SetSettings(ctx, map[string]string{"retention_days": " 365 "}); err != nil {
		t.Fatalf("SetSettings: %v", err)
	}
	if got := s.RetentionDays(ctx); got != 365 {
		t.Errorf("RetentionDays = %d, want 365", got)
	}
}

func TestSchedules(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	every := int64(21600)
	next := time.Date(2026, 3, 1, 3, 0, 0, 0, time.UTC)
	id, err := s.CreateSchedule(ctx, Schedule{
		Enabled: true, Kind: "interval", EverySeconds: &every, Label: "every 6h", NextRun: &next,
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	list, err := s.ListSchedules(ctx)
	if err != nil {
		t.Fatalf("ListSchedules: %v", err)
	}
	if len(list) != 1 || list[0].ID != id || list[0].Kind != "interval" {
		t.Fatalf("schedules = %+v", list)
	}
	if list[0].EverySeconds == nil || *list[0].EverySeconds != 21600 {
		t.Errorf("every_seconds = %v", list[0].EverySeconds)
	}
	if list[0].NextRun == nil || !list[0].NextRun.Equal(next) {
		t.Errorf("next_run = %v, want %v", list[0].NextRun, next)
	}

	// Disable and switch to cron.
	sched := list[0]
	sched.Enabled = false
	sched.Kind = "cron"
	sched.Cron = "0 3 * * *"
	sched.EverySeconds = nil
	if err := s.UpdateSchedule(ctx, sched); err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	got, err := s.GetSchedule(ctx, id)
	if err != nil {
		t.Fatalf("GetSchedule: %v", err)
	}
	if got.Enabled || got.Kind != "cron" || got.Cron != "0 3 * * *" || got.EverySeconds != nil {
		t.Errorf("updated schedule = %+v", got)
	}

	if err := s.DeleteSchedule(ctx, id); err != nil {
		t.Fatalf("DeleteSchedule: %v", err)
	}
	list, _ = s.ListSchedules(ctx)
	if len(list) != 0 {
		t.Errorf("schedules after delete = %+v", list)
	}
}

func TestChannelsAndNotificationLog(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	id, err := s.CreateChannel(ctx, Channel{
		Type: "ntfy", Label: "alerts", Config: `{"topic":"x"}`, Enabled: true,
		Events: `["health_change"]`,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	ch, err := s.GetChannel(ctx, id)
	if err != nil || ch == nil {
		t.Fatalf("GetChannel: %v, %+v", err, ch)
	}
	if ch.Type != "ntfy" || !ch.Enabled || ch.Config != `{"topic":"x"}` {
		t.Errorf("channel = %+v", ch)
	}

	sentAt := time.Date(2026, 4, 1, 3, 0, 0, 0, time.UTC)
	if err := s.LogNotification(ctx, id, "health_change", "health_change:A:caution", sentAt, "sent", ""); err != nil {
		t.Fatalf("LogNotification: %v", err)
	}
	last, err := s.LastNotificationAt(ctx, id, "health_change:A:caution")
	if err != nil {
		t.Fatalf("LastNotificationAt: %v", err)
	}
	if last == nil || !last.Equal(sentAt) {
		t.Errorf("last = %v, want %v", last, sentAt)
	}
	if none, _ := s.LastNotificationAt(ctx, id, "other"); none != nil {
		t.Errorf("unexpected last for unknown key: %v", none)
	}

	recs, err := s.RecentNotifications(ctx, 10)
	if err != nil || len(recs) != 1 {
		t.Fatalf("RecentNotifications = %+v, %v", recs, err)
	}
	if recs[0].Status != "sent" || recs[0].EventType != "health_change" {
		t.Errorf("notification = %+v", recs[0])
	}

	// Deleting the channel cascades to its log rows.
	if err := s.DeleteChannel(ctx, id); err != nil {
		t.Fatalf("DeleteChannel: %v", err)
	}
	recs, err = s.RecentNotifications(ctx, 10)
	if err != nil {
		t.Fatalf("RecentNotifications after delete: %v", err)
	}
	if len(recs) != 0 {
		t.Errorf("log rows = %d, want 0 after cascade delete", len(recs))
	}
}
