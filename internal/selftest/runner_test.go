package selftest

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/masieono/smart-serve/internal/scan"
	"github.com/masieono/smart-serve/internal/smart"
	"github.com/masieono/smart-serve/internal/store"
)

// Real smartctl payloads, trimmed to the fields this package reads. The shapes are the fixtures'
// (`internal/smart/testdata`): a running test is a log row carrying `remaining_percent`, and a
// finished one is a row that carries `passed` without it.
const (
	runningLog = `{"ata_smart_self_test_log":{"extended":{"table":[{"type":{"string":"Short offline"},` +
		`"status":{"string":"Self-test routine in progress","passed":true,"remaining_percent":40}}]}}}`
	finishedLog = `{"ata_smart_self_test_log":{"extended":{"table":[{"type":{"string":"Short offline"},` +
		`"status":{"string":"Completed without error","passed":true}}]}}}`
	noLog           = `{"json_format_version":[1,0]}`
	unsupportedLog  = `{"ata_smart_data":{"capabilities":{"self_tests_supported":false}}}`
	estimateTwoMins = `{"ata_smart_data":{"self_test":{"polling_minutes":{"short":2}}}}`
)

// --- fakes --------------------------------------------------------------------

type fakeStore struct {
	devices []store.DeviceRecord
	err     error
}

func (f *fakeStore) ListDevices(context.Context) ([]store.DeviceRecord, error) {
	return f.devices, f.err
}

type fakeRescanner struct {
	mu      sync.Mutex
	serials []string
}

func (f *fakeRescanner) ScanDevice(_ context.Context, serial, _ string, _ scan.Trigger) (scan.Result, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.serials = append(f.serials, serial)
	return scan.Result{}, false, nil
}

func (f *fakeRescanner) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.serials...)
}

// fakeDriver serves scripted self-test logs per device path, and records what it was asked to start.
//
// `onRead` is how a test simulates time passing: the runner's wait loop is driven by a clock, and the
// clock is advanced by the read that follows each poll. `failFrom` makes reads fail from a given
// index onward, which is how "the first read worked and a later poll did not" is expressed — the
// distinction the runner depends on, since a failed read is not a finished test.
type fakeDriver struct {
	mu       sync.Mutex
	starts   []string
	reads    map[string]int
	logs     map[string][]string
	startErr map[string]error
	failFrom map[string]int
	onRead   func()
}

func newFakeDriver(logs map[string][]string) *fakeDriver {
	return &fakeDriver{reads: map[string]int{}, logs: logs}
}

func (f *fakeDriver) start(_ context.Context, _ time.Duration, path, _ string, kind smart.SelfTestKind) (smart.Result, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.startErr[path]; err != nil {
		return smart.Result{}, err
	}
	f.starts = append(f.starts, path+" "+string(kind))
	return smart.Result{Stdout: []byte(noLog)}, nil
}

func (f *fakeDriver) read(_ context.Context, _ time.Duration, path, _ string) (smart.Result, error) {
	f.mu.Lock()
	n := f.reads[path]
	f.reads[path]++
	from, fails := f.failFrom[path]
	list := f.logs[path]
	onRead := f.onRead
	f.mu.Unlock()

	if onRead != nil {
		onRead()
	}
	if fails && n >= from {
		return smart.Result{}, errors.New("Read Self-test Log failed: Invalid Field in Command")
	}
	if len(list) == 0 {
		return smart.Result{Stdout: []byte(noLog)}, nil
	}
	if n >= len(list) {
		n = len(list) - 1
	}
	return smart.Result{Stdout: []byte(list[n])}, nil
}

func (f *fakeDriver) startedPaths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.starts...)
}

func (f *fakeDriver) readCount(path string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads[path]
}

// --- helpers ------------------------------------------------------------------

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// record builds a drive the runner would consider testable.
func record(serial, path string) store.DeviceRecord {
	return store.DeviceRecord{Serial: serial, DevicePath: path, SmartAvailable: true}
}

// runner builds a Runner with a 1ms poll, so a test never waits on a real clock.
func runner(t *testing.T, st Store, drv *fakeDriver, re Rescanner) *Runner {
	t.Helper()
	r := New(Options{
		Store:          st,
		Rescanner:      re,
		Driver:         Driver{Start: drv.start, Read: drv.read},
		PollInterval:   time.Millisecond,
		CommandTimeout: func(context.Context) time.Duration { return time.Second },
		Logger:         testLogger(),
	})
	return r
}

// waitFor polls until cond holds, so a test does not race the pass goroutine.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// fakeClock lets a test decide how long a wait took, so the estimate floor is exercised without
// waiting for it.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func serials(devices []store.DeviceRecord) []string {
	out := make([]string, 0, len(devices))
	for _, d := range devices {
		out = append(out, d.Serial)
	}
	return out
}

// --- eligibility --------------------------------------------------------------

// A drive that cannot be asked is skipped rather than asked and refused. Each of these is a real
// answer from a real drive, and each produces a different refusal if it is not filtered out first.
func TestEligibleSkipsDrivesThatCannotBeTested(t *testing.T) {
	st := &fakeStore{devices: []store.DeviceRecord{
		{Serial: "ok", DevicePath: "/dev/sda", SmartAvailable: true},
		{Serial: "no-path", SmartAvailable: true},
		{Serial: "unreadable", DevicePath: "/dev/sdb"},
		{Serial: "no-selftest", DevicePath: "/dev/sdc", SmartAvailable: true, RawOutput: unsupportedLog},
	}}

	got, err := Eligible(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Serial != "ok" {
		t.Fatalf("eligible = %v, want just the one drive", serials(got))
	}

	// And each skip has a reason, so a reader can be told why rather than left to wonder.
	for _, tc := range []struct {
		name string
		rec  store.DeviceRecord
	}{
		{"no device path", store.DeviceRecord{Serial: "x", SmartAvailable: true}},
		{"S.M.A.R.T. unreadable", store.DeviceRecord{Serial: "x", DevicePath: "/dev/sda"}},
		{"does not implement self-tests", store.DeviceRecord{Serial: "x", DevicePath: "/dev/sda",
			SmartAvailable: true, RawOutput: unsupportedLog}},
	} {
		if WhyNot(tc.rec) == "" {
			t.Errorf("%s: no reason given for skipping", tc.name)
		}
	}
	if WhyNot(record("x", "/dev/sda")) != "" {
		t.Error("a healthy drive should have no reason to be skipped")
	}
}

// Sorted by serial: stable, unique, and not something the user can edit. Renaming a drive or moving
// it to another bay must not reorder a queue that is already draining.
func TestEligibleIsOrderedBySerial(t *testing.T) {
	st := &fakeStore{devices: []store.DeviceRecord{
		{Serial: "zeta", DevicePath: "/dev/sdc", SmartAvailable: true, Name: "aaa", Bay: "bay 1"},
		{Serial: "alpha", DevicePath: "/dev/sda", SmartAvailable: true, Name: "zzz", Bay: "bay 9"},
		{Serial: "mid", DevicePath: "/dev/sdb", SmartAvailable: true},
	}}

	got, err := Eligible(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"alpha", "mid", "zeta"}
	for i, w := range want {
		if got[i].Serial != w {
			t.Fatalf("order = %v, want %v", serials(got), want)
		}
	}
}

// The queue exists for this and nothing else: twenty-four long tests starting together is an I/O
// meltdown. Asserted through the rescanner, which is called only after a drive's test has ended — so
// a second start before the first rescan would mean two tests were running at once.
func TestAPassTestsOneDriveAtATime(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	drv := newFakeDriver(map[string][]string{
		"/dev/sda": {noLog, finishedLog},
		"/dev/sdb": {noLog, finishedLog},
	})
	drv.onRead = func() { clock.advance(time.Minute) }
	st := &fakeStore{devices: []store.DeviceRecord{record("b", "/dev/sdb"), record("a", "/dev/sda")}}
	re := &fakeRescanner{}
	r := runner(t, st, drv, re)
	r.now = clock.now

	n, err := r.StartQueue(context.Background(), "short")
	if err != nil || n != 2 {
		t.Fatalf("StartQueue = %d, %v; want 2, nil", n, err)
	}
	waitFor(t, "the pass to finish", func() bool { return len(re.seen()) == 2 })

	if got := drv.startedPaths(); len(got) != 2 ||
		!strings.HasPrefix(got[0], "/dev/sda ") || !strings.HasPrefix(got[1], "/dev/sdb ") {
		t.Fatalf("starts = %v, want /dev/sda then /dev/sdb (serial order)", got)
	}
	if got := re.seen(); got[0] != "a" || got[1] != "b" {
		t.Fatalf("rescanned = %v, want a then b", got)
	}
	// Every start named the kind it was asked for.
	for _, s := range drv.startedPaths() {
		if !strings.HasSuffix(s, " short") {
			t.Errorf("started %q, want the short test the schedule asked for", s)
		}
	}
	if p := r.Progress(); p.Running {
		t.Errorf("progress still says running: %+v", p)
	}
}

// The button's own guard, for the same reason: the drive refuses a second test, and on NVMe that
// refusal arrives as a bare exit status.
func TestADriveAlreadyTestingIsNotAskedAgain(t *testing.T) {
	drv := newFakeDriver(map[string][]string{"/dev/sda": {runningLog}})
	st := &fakeStore{devices: []store.DeviceRecord{record("a", "/dev/sda")}}
	re := &fakeRescanner{}
	r := runner(t, st, drv, re)

	if _, err := r.StartQueue(context.Background(), "short"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pass to finish", func() bool { return !r.Progress().Running })

	if got := drv.startedPaths(); len(got) != 0 {
		t.Fatalf("starts = %v, want none — the drive was already testing", got)
	}
	if got := re.seen(); len(got) != 0 {
		t.Fatalf("rescanned = %v, want none", got)
	}
}

// Some drives write nothing to the log until a test *finishes*. Without the floor, the first poll
// would conclude the drive was already done and start the next one on top of a test still running.
func TestADriveThatNeverReportsProgressIsNotAbandonedEarly(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	drv := newFakeDriver(map[string][]string{"/dev/sda": {noLog, noLog, finishedLog}})
	drv.onRead = func() { clock.advance(time.Minute) }
	st := &fakeStore{devices: []store.DeviceRecord{record("a", "/dev/sda")}}
	re := &fakeRescanner{}
	r := runner(t, st, drv, re)
	r.now = clock.now

	if _, err := r.StartQueue(context.Background(), "short"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pass to finish", func() bool { return len(re.seen()) == 1 })

	// Three reads: the one before starting, then two polls. The first poll was a minute in and the
	// floor is two, so it could not conclude anything — which is the whole point.
	if got := drv.readCount("/dev/sda"); got != 3 {
		t.Fatalf("reads = %d, want 3 (the check, then two polls)", got)
	}
}

// A drive that *does* report its test running is believed over the clock, however long it takes.
func TestAReportedRunningTestExtendsTheWait(t *testing.T) {
	clock := &fakeClock{t: time.Unix(0, 0)}
	drv := newFakeDriver(map[string][]string{"/dev/sda": {noLog, runningLog, runningLog, finishedLog}})
	drv.onRead = func() { clock.advance(10 * time.Minute) }
	st := &fakeStore{devices: []store.DeviceRecord{record("a", "/dev/sda")}}
	re := &fakeRescanner{}
	r := runner(t, st, drv, re)
	r.now = clock.now

	if _, err := r.StartQueue(context.Background(), "short"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the pass to finish", func() bool { return len(re.seen()) == 1 })

	// Four reads: the check, then three polls. Two of those were well past the two-minute floor and
	// the drive said it was still testing — so the pass kept waiting.
	if got := drv.readCount("/dev/sda"); got != 4 {
		t.Fatalf("reads = %d, want 4 (the check, then three polls)", got)
	}
}

// A failed read is not a finished test. Treating it as one would start the next drive on top of a
// test that is still running — and this is the failure the project has actually seen on hardware: the
// same SN730 reported "Read Self-test Log failed" under one smartctl and read fine under the next.
func TestAReadFailureIsNotAFinishedTest(t *testing.T) {
	drv := newFakeDriver(map[string][]string{"/dev/sda": {noLog, noLog, finishedLog}})
	drv.failFrom = map[string]int{"/dev/sda": 1} // the check works; every poll after it fails
	st := &fakeStore{devices: []store.DeviceRecord{record("a", "/dev/sda")}}
	re := &fakeRescanner{}
	r := runner(t, st, drv, re)

	ctx, cancel := context.WithCancel(context.Background())
	if _, err := r.StartQueue(ctx, "short"); err != nil {
		t.Fatal(err)
	}
	// It waits rather than moving on, so nothing is rescanned while it does.
	waitFor(t, "a few failed polls", func() bool { return drv.readCount("/dev/sda") >= 4 })
	if got := re.seen(); len(got) != 0 {
		t.Fatalf("rescanned = %v, want none while the log cannot be read", got)
	}
	cancel()
	waitFor(t, "the pass to stop", func() bool { return !r.Progress().Running })
}

// Two overlapping passes would test the same drives twice, and the second would find each one already
// testing and skip it — so the honest answer is to refuse up front.
func TestAPassRefusesToOverlap(t *testing.T) {
	drv := newFakeDriver(map[string][]string{"/dev/sda": {noLog, runningLog}})
	st := &fakeStore{devices: []store.DeviceRecord{record("a", "/dev/sda")}}
	r := runner(t, st, drv, &fakeRescanner{})

	if _, err := r.StartQueue(context.Background(), "short"); err != nil {
		t.Fatal(err)
	}
	if _, err := r.StartQueue(context.Background(), "short"); !errors.Is(err, ErrAlreadyRunning) {
		t.Fatalf("second StartQueue = %v, want ErrAlreadyRunning", err)
	}
	r.Stop()
	waitFor(t, "the pass to stop", func() bool { return !r.Progress().Running })
}

// A fleet with nothing testable is not an error: it is a pass with nothing to do.
func TestAPassWithNothingToDoIsNotAnError(t *testing.T) {
	st := &fakeStore{devices: []store.DeviceRecord{{Serial: "a", SmartAvailable: true}}}
	r := runner(t, st, newFakeDriver(nil), &fakeRescanner{})

	n, err := r.StartQueue(context.Background(), "short")
	if err != nil || n != 0 {
		t.Fatalf("StartQueue = %d, %v; want 0, nil", n, err)
	}
	if p := r.Progress(); p.Running {
		t.Errorf("progress says running for a pass that never started: %+v", p)
	}
}

// Anything unrecognised is short. The store, the API and the UI all apply this rule, and it guesses
// cheap on purpose: a mistyped value that silently meant *long* would be hours of I/O per drive.
func TestAnUnknownKindIsShort(t *testing.T) {
	for _, in := range []string{"", "SHORT", "extended", "  long  ", "Long"} {
		want := store.SelfTestShort
		if strings.EqualFold(strings.TrimSpace(in), "long") {
			want = store.SelfTestLong
		}
		if got := normaliseKind(in); got != want {
			t.Errorf("normaliseKind(%q) = %q, want %q", in, got, want)
		}
	}
}

// The drive's own estimate sets the floor, clamped at both ends: a drive reporting no estimate still
// gets one, and a drive reporting an implausible one cannot stall the pass for a day.
func TestTheFloorComesFromTheDrivesOwnEstimate(t *testing.T) {
	mins := func(n int) *int { return &n }
	for _, tc := range []struct {
		name  string
		short *int
		want  time.Duration
	}{
		{"no estimate", nil, minWaitFloor},
		{"below the floor", mins(1), minWaitFloor},
		{"the drive's own", mins(90), 90 * time.Minute},
		{"absurd", mins(100000), minWaitCeiling},
	} {
		payload := `{"ata_smart_data":{"self_test":{"polling_minutes":{"short":` + itoa(tc.short) + `}}}}`
		if got := estimateFloor(smart.ParseLogs([]byte(payload)), store.SelfTestShort); got != tc.want {
			t.Errorf("%s: floor = %v, want %v", tc.name, got, tc.want)
		}
	}

	// The long estimate is a separate field, and a long schedule has to use it: a short drive's
	// two-minute estimate would end a twenty-hour test after two minutes.
	long := `{"ata_smart_data":{"self_test":{"polling_minutes":{"short":2,"extended":200}}}}`
	if got := estimateFloor(smart.ParseLogs([]byte(long)), store.SelfTestLong); got != 200*time.Minute {
		t.Errorf("long floor = %v, want 200m", got)
	}
	if got := estimateFloor(smart.ParseLogs([]byte(long)), store.SelfTestShort); got != minWaitFloor {
		t.Errorf("short floor = %v, want the floor", got)
	}
}

func itoa(v *int) string {
	if v == nil {
		return "0"
	}
	return strconv.Itoa(*v)
}
