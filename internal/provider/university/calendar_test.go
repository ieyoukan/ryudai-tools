package university

import (
	"os"
	"testing"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, e := os.ReadFile("testdata/" + name)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestCalendarDatesAndOverlaps(t *testing.T) {
	es, err := calendarEvents(fixture(t, "calendar2026.html"), 2026)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		date, semester, kind string
		quarter, activeCount int
	}{
		{"2026-10-05", "後学期", "registration_adjustment", 3, 1},
		{"2027-01-02", "後学期", "vacation", 4, 1},
		{"2026-08-04", "前学期", "exams", 2, 1},
		{"2027-03-31", "後学期", "vacation", 0, 2},
	} {
		r := calendarAt(es, tc.date, 30, 2026)
		if r.Semester != tc.semester || r.Quarter != tc.quarter || len(r.Active) != tc.activeCount {
			t.Errorf("%s: %+v", tc.date, r)
		}
		found := false
		for _, e := range r.Active {
			if e.Kind == tc.kind {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: missing %s", tc.date, tc.kind)
		}
		if tc.kind == "vacation" && (r.ClassesInSession == nil || *r.ClassesInSession) {
			t.Errorf("vacation treated as classes: %+v", r)
		}
	}
	if r := calendarAt(es, "2026-10-15", 1, 2026); r.EffectiveWeekday != "月" {
		t.Fatalf("makeup: %+v", r)
	}
	for _, e := range calendarAt(es, "2026-08-11", 1, 2026).Active {
		if e.Title == "予備日(※7)" {
			t.Fatal("separate reserve dates became a range")
		}
	}
	for _, e := range calendarAt(es, "2026-04-16", 1, 2026).Active {
		if e.Title == "定期健康診断(4月16日を除く)" {
			t.Fatal("excluded date is active")
		}
	}
	for _, e := range calendarAt(es, "2026-10-05", 1, 2026).Active {
		if e.OpenEnded {
			t.Fatal("unknown end treated as active indefinitely")
		}
	}
	if _, e := calendarEvents(fixture(t, "calendar2026.html"), 2028); e == nil {
		t.Fatal("unpublished academic year must fail")
	}
}
