package httpapi

import (
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/masieono/smart-serve/internal/interp"
	"github.com/masieono/smart-serve/internal/store"
)

// handleMetrics serves the Prometheus exposition format (`FEATURES.md` C6).
//
// **Off unless the operator turns it on.** The idle-cost argument is not that this handler is
// expensive — it does no work of its own and runs no timer — but that enabling it invites *someone
// else's* scraper to poll this app on a schedule, and that is the operator's decision rather than a
// default (`ADR-023`). A disabled endpoint answers **404, not 403**: the honest answer to "is this
// feature on?" is "it is not here", and a 403 would advertise an endpoint the operator did not
// enable.
//
// The format is written by hand rather than with `prometheus/client_golang`. The exposition format
// is four rules and a handful of escaping, this is six metrics, and the library would multiply a
// dependency tree that currently holds exactly one module — which is a feature of this project, not
// an accident (`ADR-020`).
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	settings, err := s.store.GetSettings(r.Context())
	if err != nil {
		s.fail(w, "metrics settings", err)
		return
	}
	if !strings.EqualFold(strings.TrimSpace(settings["prometheus_enabled"]), "true") {
		writeError(w, http.StatusNotFound, "metrics are not enabled")
		return
	}

	devices, err := s.store.ListDevices(r.Context())
	if err != nil {
		s.fail(w, "metrics devices", err)
		return
	}
	// The same three derived queries the dashboard makes, and for the same reason: a scrape should
	// see what the screen sees. Each is best-effort — a missing figure drops a series rather than
	// failing the whole scrape, which is exactly what Prometheus wants for "no measurement".
	facts, ferr := s.store.LatestSnapshotFacts(r.Context())
	if ferr != nil {
		facts = nil
	}
	trend, terr := s.store.SectorTrend(r.Context(),
		time.Now().Add(-s.store.RetentionWindow(r.Context())), interp.WorsenWindow)
	if terr != nil {
		trend = nil
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	var b strings.Builder
	writeMetrics(&b, s.version, devices, facts, trend, s.lastScanTime(r.Context()))
	_, _ = w.Write([]byte(b.String()))
}

// metricFamily is one metric's metadata plus its series, so the HELP and TYPE lines are emitted in
// one place and cannot drift from the samples beneath them.
type metricFamily struct {
	name   string
	help   string
	typ    string // "gauge" or "counter"
	series []metricSample
}

type metricSample struct {
	labels string // rendered, e.g. `{serial="S1"}`; empty when the metric has no labels
	value  string
}

// add appends a sample. Empty label values are dropped rather than rendered, because the exposition
// format treats an empty value as equivalent to an absent label — and `bay=""` on every drive would
// be noise in a scrape diff.
func (f *metricFamily) add(labels map[string]string, value string) {
	kept := map[string]string{}
	for k, v := range labels {
		if v != "" {
			kept[k] = v
		}
	}
	f.series = append(f.series, metricSample{labels: renderLabels(kept), value: value})
}

func (f *metricFamily) write(b *strings.Builder) {
	if len(f.series) == 0 {
		return
	}
	// Sorted, so a scrape is diffable: an exporter whose sample order wanders makes every scrape look
	// like a change, and makes a test that asserts on output flaky.
	sort.Slice(f.series, func(i, j int) bool { return f.series[i].labels < f.series[j].labels })
	fmt.Fprintf(b, "# HELP %s %s\n# TYPE %s %s\n", f.name, f.help, f.name, f.typ)
	for _, s := range f.series {
		fmt.Fprintf(b, "%s%s %s\n", f.name, s.labels, s.value)
	}
}

// renderLabels renders a label set, escaping the three characters the format reserves.
func renderLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return ""
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `%s="%s"`, k, escapeLabelValue(labels[k]))
	}
	b.WriteString("}")
	return b.String()
}

func escapeLabelValue(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

// writeMetrics renders one scrape.
//
// Two decisions are visible in the shape of this. **Descriptive labels live in one `_info` metric**,
// with the numeric series keyed by serial alone: a model, a bay and a tag set are constants per
// drive, and repeating them across five metrics would multiply the series count for nothing. And **a
// reading the drive did not report drops the series rather than exporting a zero** — which is the
// same rule as the em dash on screen and the empty cell in an export, and here it is also what
// Prometheus expects: an absent series means "no measurement", while a `0` is a claim that the drive
// reported zero.
func writeMetrics(
	b *strings.Builder,
	version string,
	devices []store.DeviceRecord,
	facts map[string]store.SnapshotFacts,
	trend map[string]int64,
	lastScan any,
) {
	build := &metricFamily{name: "smartserve_build_info", typ: "gauge",
		help: "Build information. Always 1."}
	info := &metricFamily{name: "smartserve_device_info", typ: "gauge",
		help: "Drive identity and its last verdict, as labels. Always 1."}
	score := &metricFamily{name: "smartserve_device_score", typ: "gauge",
		help: "Health score, 0-100. Absent for a drive that could not be scored."}
	temp := &metricFamily{name: "smartserve_device_temperature_celsius", typ: "gauge",
		help: "Last reported drive temperature in Celsius."}
	hours := &metricFamily{name: "smartserve_device_power_on_hours", typ: "gauge",
		help: "Power-on hours at the last scan."}
	wear := &metricFamily{name: "smartserve_device_wear_percent", typ: "gauge",
		help: "Percentage of rated write endurance used. Absent when the drive reports none."}
	worsening := &metricFamily{name: "smartserve_device_new_sectors", typ: "gauge",
		help: "Bad sectors that appeared over the recent window. Absent when the count is not moving."}
	total := &metricFamily{name: "smartserve_devices_total", typ: "gauge",
		help: "Drives this host has seen, connected or not."}
	byHealth := &metricFamily{name: "smartserve_devices", typ: "gauge",
		help: "Drives by verdict."}
	lastScanAt := &metricFamily{name: "smartserve_scan_last_success_timestamp_seconds", typ: "gauge",
		help: "Unix timestamp of the last completed scan."}

	build.add(map[string]string{"version": version}, "1")

	counts := map[string]int{}
	for _, d := range devices {
		// Unplugged drives are still described and still counted: they are part of the inventory, and
		// their readings are the last ones anybody saw.
		info.add(map[string]string{
			"serial": d.Serial, "name": d.Name, "model": d.Model,
			"transport": d.Transport, "bay": d.Bay, "health": d.Health, "tags": d.Tags,
		}, "1")
		counts[d.Health]++

		labels := map[string]string{"serial": d.Serial}
		if d.Score >= 0 {
			score.add(labels, strconv.Itoa(d.Score))
		}
		if d.TemperatureC != nil {
			temp.add(labels, strconv.Itoa(*d.TemperatureC))
		}
		if f, ok := facts[d.Serial]; ok {
			if f.PowerOnHours != nil {
				hours.add(labels, strconv.FormatInt(*f.PowerOnHours, 10))
			}
			if f.WearPct != nil {
				wear.add(labels, strconv.FormatFloat(*f.WearPct, 'f', -1, 64))
			}
		}
		if delta, ok := trend[d.Serial]; ok && delta > 0 {
			worsening.add(labels, strconv.FormatInt(delta, 10))
		}
	}

	total.add(nil, strconv.Itoa(len(devices)))
	for health, n := range counts {
		byHealth.add(map[string]string{"health": health}, strconv.Itoa(n))
	}

	// `lastScanTime` is shaped for JSON, where nil means "no scan yet"; a scrape has no way to say
	// that, so the series is simply absent.
	if ts, ok := lastScan.(string); ok && ts != "" {
		if t, err := time.Parse(time.RFC3339, ts); err == nil {
			lastScanAt.add(nil, strconv.FormatInt(t.Unix(), 10))
		}
	}

	for _, f := range []*metricFamily{
		build, info, score, temp, hours, wear, worsening, total, byHealth, lastScanAt,
	} {
		f.write(b)
	}
}
