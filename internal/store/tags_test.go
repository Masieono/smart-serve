package store

import (
	"reflect"
	"testing"
)

func TestTagListSplitsAndTrims(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"tank", []string{"tank"}},
		{"tank, media", []string{"tank", "media"}},
		{" tank ,, media , ", []string{"tank", "media"}},
		{",,,", nil},
		{"tank,tank", []string{"tank", "tank"}},
	} {
		if got := TagList(tc.in); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("TagList(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// Case-insensitive, because the tags are typed by hand and "Bench" is the same intention as "bench".
// The grouping in the UI compares the same way, so a tag that grouped would also be scoped.
func TestHasTagIgnoresCase(t *testing.T) {
	for _, tc := range []struct {
		tags, want string
		ok         bool
	}{
		{"bench", "bench", true},
		{"Bench", "bench", true},
		{"BENCH", "bench", true},
		{" tank , bench ", "bench", true},
		{"benchmark", "bench", false},
		{"tank", "bench", false},
		{"", "bench", false},
		{"benchtop, spare", "bench", false},
	} {
		if got := HasTag(tc.tags, tc.want); got != tc.ok {
			t.Errorf("HasTag(%q, %q) = %v, want %v", tc.tags, tc.want, got, tc.ok)
		}
	}
}

// A substring is not a tag. `bench` and `benchtop` are different labels, and matching by prefix would
// silently pull a production pool into a testing session.
func TestHasTagIsNotASubstringMatch(t *testing.T) {
	if HasTag("benchtop", BenchTag) {
		t.Error("benchtop matched bench")
	}
}
