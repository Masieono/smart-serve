package notify

import (
	"strings"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/store"
)

func dev(serial, name, health string) store.DeviceRecord {
	return store.DeviceRecord{Serial: serial, Name: name, Health: health, Score: 80}
}

// B13: one message instead of a stream. The event catalog is exception-shaped — every other event
// fires because something changed — and a quiet week sends nothing at all. That is right for alerts
// and wrong for the question "is my fleet okay?".
func TestADigestSaysWhatTheFleetIs(t *testing.T) {
	ev := DigestEvent(Digest{
		From:    time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC),
		To:      time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC),
		Devices: []store.DeviceRecord{dev("a", "parity", "good"), dev("b", "media", "good")},
		ScansOK: 7,
	}, CollectOptions{Version: "test"})

	if ev.Type != EventDigest || ev.Title() != "Smart-Serve digest" {
		t.Errorf("event = %+v", ev)
	}
	body := ev.Body()
	for _, want := range []string{"2 drives: 2 healthy", "Scans: 7 completed, 0 failed"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest is missing %q:\n%s", want, body)
		}
	}
	// Nothing to report is *omitted*, not printed as "none": a heading with nothing under it is noise,
	// and a reader learns to skip the whole message.
	for _, unwanted := range []string{"Changed:", "Worse:", "Replace first:"} {
		if strings.Contains(body, unwanted) {
			t.Errorf("a quiet week printed %q:\n%s", unwanted, body)
		}
	}
	// A healthy fleet is a note, not an alert.
	if ev.Priority != PriorityLow {
		t.Errorf("priority = %q, want low for a healthy fleet", ev.Priority)
	}
}

// The priority reflects the content rather than the event type, because a weekly message that always
// arrives at the same importance stops carrying information.
func TestADigestAboutAFailingDriveIsNotALowPriorityNote(t *testing.T) {
	ev := DigestEvent(Digest{
		Devices: []store.DeviceRecord{dev("a", "parity", "bad"), dev("b", "media", "good")},
		ScansOK: 7,
		Ranking: []interp.ReplacementRank{
			{Serial: "a", Name: "parity", Band: interp.ReplaceNow, Reasons: []string{"failing now"}},
			{Serial: "b", Name: "media", Band: interp.ReplaceFine},
		},
	}, CollectOptions{})

	if ev.Priority != PriorityMedium {
		t.Errorf("priority = %q, want medium", ev.Priority)
	}
	body := ev.Body()
	if !strings.Contains(body, "1 failing") {
		t.Errorf("digest does not name the failing count:\n%s", body)
	}
	// The ranking's own words, so the digest and the dashboard cannot disagree about which drive to
	// buy — and the drive the ranking called fine is left out.
	if !strings.Contains(body, "Replace first: parity (failing now)") {
		t.Errorf("digest does not name the drive to replace:\n%s", body)
	}
	if strings.Contains(body, "media") {
		t.Errorf("a drive the ranking called fine is in the shortlist:\n%s", body)
	}
}

// A drive that was not being watched at the start of the period has not *changed*, it has arrived, and
// reporting it as a change would make every first week look like a fleet-wide event.
func TestADriveWithNoBaselineIsNotAChange(t *testing.T) {
	summary := digestSummary(Digest{
		Devices: []store.DeviceRecord{dev("a", "new-drive", "caution")},
		Before:  map[string]string{}, // never seen before this period
	})
	if strings.Contains(summary, "Changed:") {
		t.Errorf("a brand-new drive was reported as a change:\n%s", summary)
	}

	// A drive that *was* seen, and moved, is.
	summary = digestSummary(Digest{
		Devices: []store.DeviceRecord{dev("a", "parity", "bad")},
		Before:  map[string]string{"a": "caution"},
	})
	if !strings.Contains(summary, "parity caution → bad") {
		t.Errorf("a real change was missed:\n%s", summary)
	}

	// And one that held steady is not a change either.
	summary = digestSummary(Digest{
		Devices: []store.DeviceRecord{dev("a", "parity", "caution")},
		Before:  map[string]string{"a": "caution"},
	})
	if strings.Contains(summary, "Changed:") {
		t.Errorf("an unchanged drive was reported as a change:\n%s", summary)
	}
}

// A section listing twenty-four drives is not a summary — it is the dashboard in a notification tray.
// What was left out is said, because a silently truncated list reads as a complete one.
func TestADigestCapsItsListsAndSaysSo(t *testing.T) {
	var devices []store.DeviceRecord
	worsening := map[string]int64{}
	for _, s := range []string{"a", "b", "c", "d", "e", "f", "g"} {
		devices = append(devices, dev(s, "drive-"+s, "good"))
		worsening[s] = 1
	}
	worsening["g"] = 40 // worst first, so this one must lead

	summary := digestSummary(Digest{Devices: devices, Worsening: worsening})
	if !strings.Contains(summary, "and 2 more worse") {
		t.Errorf("the cap does not say what it left out:\n%s", summary)
	}
	if !strings.Contains(summary, "drive-g +40 sectors") {
		t.Errorf("the worst drive is not first:\n%s", summary)
	}
}

// The summary is several lines and the body must keep them apart: a digest collapsed onto one line is
// unreadable in the clients that only render the body.
func TestADigestBodyKeepsItsLines(t *testing.T) {
	ev := DigestEvent(Digest{
		Devices: []store.DeviceRecord{dev("a", "parity", "bad"), dev("b", "media", "good")},
		Before:  map[string]string{"a": "caution"},
		ScansOK: 7,
	}, CollectOptions{})

	body := ev.Body()
	if !strings.Contains(body, "\n") {
		t.Fatalf("digest body is one line:\n%s", body)
	}
	for _, want := range []string{"Changed:", "Scans:"} {
		if !strings.Contains(body, want) {
			t.Errorf("digest body is missing %q:\n%s", want, body)
		}
	}
	// The title still leads, so a client that does not render the Title header still reads correctly.
	if !strings.HasPrefix(body, "📊 Smart-Serve digest") {
		t.Errorf("body does not open with the title:\n%s", body)
	}
}

// A digest is a *scheduled* message, so squelching never applies: suppressing a weekly summary because
// a previous one arrived an hour ago would mean the schedule and the notification disagreed about
// whether it ran.
func TestADigestIsNeverSquelched(t *testing.T) {
	ev := DigestEvent(Digest{To: time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)}, CollectOptions{})
	if got := squelchWindow(ev, time.Hour); got != 0 {
		t.Errorf("squelch = %v, want 0", got)
	}
	// One key per fire, so two digests an hour apart are two messages.
	ev2 := DigestEvent(Digest{To: time.Date(2026, 10, 20, 1, 0, 0, 0, time.UTC)}, CollectOptions{})
	if ev.Key() == ev2.Key() {
		t.Errorf("two digests share a key: %q", ev.Key())
	}
	// And the version rides along, so a digest says which build sent it.
	if got := DigestEvent(Digest{}, CollectOptions{Version: "1.2.3"}).Version; got != "1.2.3" {
		t.Errorf("version = %q, want it carried", got)
	}
}

// The digest is not opt-in the way the heartbeat and group heat are: those two would be sent to
// channels that never asked, whereas a digest only exists because somebody created a digest schedule.
// The schedule *is* the subscription.
func TestADigestIsNotOptIn(t *testing.T) {
	if !subscribed("", EventDigest) {
		t.Error("a channel subscribed to everything does not receive a digest")
	}
	if !subscribed(`["digest"]`, EventDigest) {
		t.Error("naming it does not subscribe")
	}
}
