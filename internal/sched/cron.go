package sched

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronSpec is a parsed five-field cron expression: minute hour day-of-month
// month day-of-week. It supports *, lists, ranges and steps — the subset the
// schedule editor produces (see internal/webui/assets/js/cron.js).
type cronSpec struct {
	expr                          string
	minute, hour, dom, month, dow fieldSet
	domRestricted, dowRestricted  bool
}

// fieldSet is the accepted values for one cron field. `any` marks a literal `*`,
// which matters for the day-of-month / day-of-week interaction below.
type fieldSet struct {
	any    bool
	values map[int]bool
}

func (f fieldSet) contains(v int) bool { return f.any || f.values[v] }

type fieldDef struct {
	name     string
	min, max int
}

var cronFields = [...]fieldDef{
	{"minute", 0, 59},
	{"hour", 0, 23},
	{"day of month", 1, 31},
	{"month", 1, 12},
	{"day of week", 0, 7}, // 7 is accepted as Sunday and normalised to 0
}

// Next returns the first fire time strictly after `after`, evaluated in loc.
// Cron is a wall-clock concept, so the timezone is required rather than assumed
// (DECISIONS.md: "evaluate cron in local time").
func Next(expr string, after time.Time, loc *time.Location) (time.Time, error) {
	spec, err := parseCron(expr)
	if err != nil {
		return time.Time{}, err
	}
	if loc == nil {
		loc = time.Local
	}
	return spec.next(after.In(loc), loc)
}

func parseCron(expr string) (cronSpec, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return cronSpec{}, fmt.Errorf("cron expression needs 5 fields, got %d", len(parts))
	}
	var sets [5]fieldSet
	for i, part := range parts {
		set, err := parseCronField(part, cronFields[i])
		if err != nil {
			return cronSpec{}, err
		}
		sets[i] = set
	}
	spec := cronSpec{
		expr:   expr,
		minute: sets[0], hour: sets[1], dom: sets[2], month: sets[3], dow: sets[4],
		domRestricted: !sets[2].any,
		dowRestricted: !sets[4].any,
	}
	if spec.dow.values[7] {
		spec.dow.values[0] = true
		delete(spec.dow.values, 7)
	}
	return spec, nil
}

func parseCronField(part string, def fieldDef) (fieldSet, error) {
	set := fieldSet{values: map[int]bool{}}
	if part == "*" {
		set.any = true
		return set, nil
	}
	for _, item := range strings.Split(part, ",") {
		step := 1
		if slash := strings.IndexByte(item, '/'); slash >= 0 {
			n, err := strconv.Atoi(strings.TrimSpace(item[slash+1:]))
			if err != nil || n <= 0 {
				return set, fmt.Errorf("invalid step in %s field: %q", def.name, item)
			}
			step = n
			item = item[:slash]
		}

		lo, hi := def.min, def.max
		switch {
		case item == "*":
			// Range stays the full field; the step narrows it below.
		case strings.ContainsRune(item, '-'):
			bounds := strings.SplitN(item, "-", 2)
			a, err1 := strconv.Atoi(strings.TrimSpace(bounds[0]))
			b, err2 := strconv.Atoi(strings.TrimSpace(bounds[1]))
			if err1 != nil || err2 != nil {
				return set, fmt.Errorf("invalid range in %s field: %q", def.name, item)
			}
			lo, hi = a, b
		default:
			v, err := strconv.Atoi(strings.TrimSpace(item))
			if err != nil {
				return set, fmt.Errorf("invalid value in %s field: %q", def.name, item)
			}
			lo, hi = v, v
		}
		if lo < def.min || hi > def.max || lo > hi {
			return set, fmt.Errorf("%s field out of range (%d-%d): %q", def.name, def.min, def.max, part)
		}
		for v := lo; v <= hi; v += step {
			set.values[v] = true
		}
	}
	return set, nil
}

// next walks forward to the first matching minute. Rather than testing every
// minute it skips whole months, days and hours that cannot match, so even a
// sparse expression like "0 3 29 2 *" resolves in a few hundred steps. Times are
// rebuilt with time.Date so daylight-saving gaps and repeats are handled by the
// standard library instead of by arithmetic on instants.
func (c cronSpec) next(after time.Time, loc *time.Location) (time.Time, error) {
	t := time.Date(after.Year(), after.Month(), after.Day(), after.Hour(), after.Minute(), 0, 0, loc).
		Add(time.Minute)
	limit := t.AddDate(5, 0, 0)

	for t.Before(limit) {
		if !c.month.contains(int(t.Month())) {
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, loc)
			continue
		}
		if !c.dayMatches(t) {
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, loc)
			continue
		}
		if !c.hour.contains(t.Hour()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, loc)
			continue
		}
		if !c.minute.contains(t.Minute()) {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute()+1, 0, 0, loc)
			continue
		}
		return t, nil
	}
	return time.Time{}, fmt.Errorf("no occurrence of %q within 5 years", c.expr)
}

// dayMatches applies the standard cron day rule: when both day-of-month and
// day-of-week are restricted, a day matches if *either* does; otherwise the
// restricted field alone decides.
func (c cronSpec) dayMatches(t time.Time) bool {
	dom := c.dom.contains(t.Day())
	dow := c.dow.contains(int(t.Weekday()))
	switch {
	case c.domRestricted && c.dowRestricted:
		return dom || dow
	case c.domRestricted:
		return dom
	case c.dowRestricted:
		return dow
	default:
		return true
	}
}
