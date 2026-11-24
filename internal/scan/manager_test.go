package scan

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/events"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// covers is the rule the whole fix rests on, so it is tested on its own.
func TestOnlyCovers(t *testing.T) {
	full := only{}
	a := only{serial: "A"}
	b := only{serial: "B"}

	if !full.covers(a) || !full.covers(b) || !full.covers(full) {
		t.Error("a full scan reads every drive, so it answers any request")
	}
	if !a.covers(a) {
		t.Error("a scan of A answers a request for A")
	}
	if a.covers(b) {
		t.Error("a scan of A must not answer a request for B: that result is for another drive")
	}
	if a.covers(full) {
		t.Error("a scan of one drive must not answer a request for the whole fleet")
	}
}

// blockingRunner holds a scan open until the test releases it, so the single-flight behaviour can be
// observed rather than inferred.
type blockingRunner struct {
	started chan string
	release chan struct{}
}

func newBlocking() *blockingRunner {
	return &blockingRunner{started: make(chan string, 4), release: make(chan struct{})}
}

func (r *blockingRunner) Run(ctx context.Context) ([]Device, error) {
	r.started <- "fleet"
	select {
	case <-r.release:
	case <-ctx.Done():
	}
	return nil, nil
}

func (r *blockingRunner) RunDevice(ctx context.Context, serial, path string) (Device, error) {
	r.started <- serial
	select {
	case <-r.release:
	case <-ctx.Done():
	}
	return Device{Discovered: smart.Discovered{Serial: serial}}, nil
}

func newManager(t *testing.T, r Runner) *Manager {
	t.Helper()
	st, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "scan.db"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { st.Close() })
	return NewManager(r, st, events.NewHub())
}

// The bug, stated as the caller saw it: ask for drive A while drive B is being read, and get B's
// result back with no error, which the UI reported as "Drive rescanned".
func TestScanDeviceDoesNotJoinAnotherDrivesScan(t *testing.T) {
	runner := newBlocking()
	m := newManager(t, runner)

	first := make(chan error, 1)
	go func() {
		_, _, err := m.ScanDevice(context.Background(), "B", "/dev/sdb", Manual)
		first <- err
	}()
	if got := <-runner.started; got != "B" {
		t.Fatalf("first scan read %q, want B", got)
	}

	_, joined, err := m.ScanDevice(context.Background(), "A", "/dev/sda", Manual)
	if !errors.Is(err, ErrBusy) {
		t.Fatalf("err = %v, want ErrBusy: a scan of B cannot answer a request for A", err)
	}
	if joined {
		t.Error("joined = true, but the running scan is reading a different drive")
	}

	close(runner.release)
	if err := <-first; err != nil {
		t.Fatalf("the first scan failed: %v", err)
	}
}

// A full scan asked for while one drive is being read must not pretend to have started.
func TestFullScanCannotJoinADeviceScan(t *testing.T) {
	runner := newBlocking()
	m := newManager(t, runner)

	first := make(chan error, 1)
	go func() {
		_, _, err := m.ScanDevice(context.Background(), "B", "/dev/sdb", Manual)
		first <- err
	}()
	<-runner.started

	if running, coversFull := m.Busy(); !running || coversFull {
		t.Errorf("Busy() = (%v, %v), want busy and not covering the whole fleet", running, coversFull)
	}
	if _, _, err := m.Scan(context.Background(), Manual); !errors.Is(err, ErrBusy) {
		t.Errorf("full scan err = %v, want ErrBusy", err)
	}

	close(runner.release)
	<-first
}

// The joins that are honest still happen: the same drive, and anything with a full scan running.
func TestHonestJoinsStillWork(t *testing.T) {
	runner := newBlocking()
	m := newManager(t, runner)

	first := make(chan error, 1)
	go func() {
		_, _, err := m.ScanDevice(context.Background(), "A", "/dev/sda", Manual)
		first <- err
	}()
	<-runner.started

	// Same drive: joining is correct, and the caller waits on the real result.
	second := make(chan error, 1)
	go func() {
		_, joined, err := m.ScanDevice(context.Background(), "A", "/dev/sda", Manual)
		if !joined {
			t.Error("a request for the drive being read should join it, not be refused")
		}
		second <- err
	}()

	// Let both finish.
	close(runner.release)
	if err := <-first; err != nil {
		t.Fatalf("first: %v", err)
	}
	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("joined scan: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the joining caller never returned")
	}
}

// Two identical enclosures, neither reporting a serial: the fallback identity has to differ, or the
// second drive upserts over the first and their histories interleave. A pair of docks becoming one
// drive is a silent loss, and `DECISIONS.md` promised the `-<n>` disambiguation the code never did.
func TestModelLessDrivesGetDistinctIdentities(t *testing.T) {
	mk := func() Device {
		return Device{Discovered: smart.Discovered{Model: "USB 3.0 SCSI Disk Device", Path: "/dev/sdb"}}
	}
	devices := uniqueModelLessSerials([]Device{mk(), mk(), mk()})

	got := map[string]bool{}
	for _, d := range devices {
		got[serialFor(d)] = true
	}
	if len(got) != 3 {
		t.Fatalf("identities = %v, want three distinct ones", got)
	}
	// The first keeps the plain form, so a single such drive keeps its history across upgrades.
	if !got["__nomodel:USB 3.0 SCSI Disk Device"] {
		t.Errorf("the first drive lost its plain identity: %v", got)
	}
}

// A drive with a real serial is never touched, however many there are.
func TestRealSerialsAreLeftAlone(t *testing.T) {
	devices := uniqueModelLessSerials([]Device{
		{Discovered: smart.Discovered{Serial: "AAA"}},
		{Discovered: smart.Discovered{Serial: "BBB"}},
	})
	if serialFor(devices[0]) != "AAA" || serialFor(devices[1]) != "BBB" {
		t.Errorf("serials were rewritten: %q %q", serialFor(devices[0]), serialFor(devices[1]))
	}
}
