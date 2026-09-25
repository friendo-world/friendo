package data

import (
	"os"
	"strings"
	"testing"
	"time"
)

func la(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

// Every documented `when` spelling parses to the expected start/end/all-day.
func TestParseWhenShapes(t *testing.T) {
	loc := la(t)
	d := func(y int, m time.Month, day, h, mi int) time.Time { return time.Date(y, m, day, h, mi, 0, 0, loc) }
	cases := []struct {
		name         string
		meta         map[string]any
		starts, ends time.Time
		allDay       bool
		rule         string
		warn         bool
	}{
		{"all day", map[string]any{"when": "2026-10-04"}, d(2026, 10, 4, 0, 0), time.Time{}, true, "", false},
		{"yaml date", map[string]any{"when": time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}, d(2026, 10, 4, 0, 0), time.Time{}, true, "", false},
		{"start only", map[string]any{"when": "2026-10-04 19:00"}, d(2026, 10, 4, 19, 0), time.Time{}, false, "", false},
		{"T separator", map[string]any{"when": "2026-10-04T19:00"}, d(2026, 10, 4, 19, 0), time.Time{}, false, "", false},
		{"12-hour", map[string]any{"when": "2026-10-04 7pm to 9:30 pm"}, d(2026, 10, 4, 19, 0), d(2026, 10, 4, 21, 30), false, "", false},
		{"range", map[string]any{"when": "2026-10-04 10:00 to 16:00"}, d(2026, 10, 4, 10, 0), d(2026, 10, 4, 16, 0), false, "", false},
		{"dash range", map[string]any{"when": "2026-10-04 10:00 - 16:00"}, d(2026, 10, 4, 10, 0), d(2026, 10, 4, 16, 0), false, "", false},
		{"crosses midnight", map[string]any{"when": "2026-10-04 22:00 to 02:00"}, d(2026, 10, 4, 22, 0), d(2026, 10, 5, 2, 0), false, "", false},
		{"full end", map[string]any{"when": "2026-10-04 19:00 to 2026-10-05 02:00"}, d(2026, 10, 4, 19, 0), d(2026, 10, 5, 2, 0), false, "", false},
		{"multi-day", map[string]any{"when": "2026-10-04 to 2026-10-06"}, d(2026, 10, 4, 0, 0), d(2026, 10, 6, 0, 0), true, "", false},
		{"ends key", map[string]any{"when": "2026-10-07 19:00", "ends": "20:30"}, d(2026, 10, 7, 19, 0), d(2026, 10, 7, 20, 30), false, "", false},
		{"ends key full", map[string]any{"when": "2026-10-07 19:00", "ends": "2026-10-07 20:30"}, d(2026, 10, 7, 19, 0), d(2026, 10, 7, 20, 30), false, "", false},
		{"ends before start", map[string]any{"when": "2026-10-07 19:00", "ends": "2026-10-07 18:00"}, d(2026, 10, 7, 19, 0), time.Time{}, false, "", true},
		{"rfc3339 offset converts", map[string]any{"when": "2026-10-04T19:00:00-04:00"}, d(2026, 10, 4, 16, 0), time.Time{}, false, "", false},
		{"repeats word", map[string]any{"when": "2026-10-07 19:00", "repeats": "weekly"}, d(2026, 10, 7, 19, 0), time.Time{}, false, "FREQ=WEEKLY", false},
		{"repeats every 2 weeks", map[string]any{"when": "2026-10-07 19:00", "repeats": "every 2 weeks"}, d(2026, 10, 7, 19, 0), time.Time{}, false, "FREQ=WEEKLY;INTERVAL=2", false},
		{"repeats map", map[string]any{"when": "2026-10-06 19:00", "repeats": map[string]any{"every": "month", "on": "first tuesday", "until": "2027-06-30"}},
			d(2026, 10, 6, 19, 0), time.Time{}, false, "FREQ=MONTHLY;UNTIL=20270701T065959Z;BYDAY=+1TU", false},
		{"repeats weekly on days", map[string]any{"when": "2026-10-06 19:00", "repeats": map[string]any{"every": "week", "on": []any{"tue", "thu"}, "count": 8}},
			d(2026, 10, 6, 19, 0), time.Time{}, false, "FREQ=WEEKLY;COUNT=8;BYDAY=TU,TH", false},
		{"repeats monthly on 15th", map[string]any{"when": "2026-10-15", "repeats": map[string]any{"every": "month", "on": 15}},
			d(2026, 10, 15, 0, 0), time.Time{}, true, "FREQ=MONTHLY;BYMONTHDAY=15", false},
		{"rrule escape hatch", map[string]any{"when": "2026-10-06 19:00", "rrule": "FREQ=WEEKLY;BYDAY=TU,TH"}, d(2026, 10, 6, 19, 0), time.Time{}, false, "FREQ=WEEKLY;BYDAY=TU,TH", false},
		{"bad repeats warns", map[string]any{"when": "2026-10-06 19:00", "repeats": "sometimes"}, d(2026, 10, 6, 19, 0), time.Time{}, false, "", true},
		{"bad zone warns", map[string]any{"when": "2026-10-06 19:00", "timezone": "Mars/Olympus"}, d(2026, 10, 6, 19, 0), time.Time{}, false, "", true},
	}
	for _, c := range cases {
		ev, warnings, present := ParseWhen(c.meta, loc)
		if !present {
			t.Errorf("%s: not present", c.name)
			continue
		}
		if ev == nil {
			t.Errorf("%s: no event (warnings %v)", c.name, warnings)
			continue
		}
		if !ev.Starts.Equal(c.starts) || ev.Starts.Location().String() != loc.String() {
			t.Errorf("%s: starts = %v, want %v", c.name, ev.Starts, c.starts)
		}
		if !ev.Ends.Equal(c.ends) {
			t.Errorf("%s: ends = %v, want %v", c.name, ev.Ends, c.ends)
		}
		if ev.AllDay != c.allDay {
			t.Errorf("%s: all_day = %v, want %v", c.name, ev.AllDay, c.allDay)
		}
		if ev.RRule != c.rule {
			t.Errorf("%s: rrule = %q, want %q", c.name, ev.RRule, c.rule)
		}
		if (len(warnings) > 0) != c.warn {
			t.Errorf("%s: warnings = %v, want warn=%v", c.name, warnings, c.warn)
		}
	}
}

func TestParseWhenBadAndAbsent(t *testing.T) {
	loc := la(t)
	if _, _, present := ParseWhen(map[string]any{"title": "x"}, loc); present {
		t.Error("no when should not be present")
	}
	ev, warnings, present := ParseWhen(map[string]any{"when": "next tuesday"}, loc)
	if !present || ev != nil || len(warnings) == 0 {
		t.Errorf("garbage when: ev=%v warnings=%v present=%v", ev, warnings, present)
	}
	ev, warnings, _ = ParseWhen(map[string]any{"when": "2026-10-04 25:00"}, loc)
	if ev != nil || len(warnings) == 0 {
		t.Errorf("25:00 should fail: ev=%v warnings=%v", ev, warnings)
	}
}

func TestParseWhenTimezoneOverride(t *testing.T) {
	loc := la(t)
	ev, _, _ := ParseWhen(map[string]any{"when": "2026-10-04 19:00", "timezone": "Europe/Paris"}, loc)
	if ev.Timezone != "Europe/Paris" || ev.Starts.Location().String() != "Europe/Paris" || ev.Starts.Hour() != 19 {
		t.Errorf("zone override: %+v", ev)
	}
}

func TestWeekdaySugar(t *testing.T) {
	loc := la(t)
	ev, warnings, _ := ParseWhen(map[string]any{"when": "mondays 19:00 to 20:30"}, loc)
	if ev == nil {
		t.Fatalf("sugar failed: %v", warnings)
	}
	if ev.Starts.Weekday() != time.Monday || ev.Starts.Hour() != 19 || ev.Ends.Hour() != 20 || ev.Ends.Minute() != 30 {
		t.Errorf("starts %v ends %v", ev.Starts, ev.Ends)
	}
	if ev.RRule != "FREQ=WEEKLY" || ev.AllDay {
		t.Errorf("rule %q allday %v", ev.RRule, ev.AllDay)
	}
	if ev.Starts.Before(time.Now().In(loc).Add(-24 * time.Hour)) {
		t.Errorf("first occurrence should be upcoming, got %v", ev.Starts)
	}
	ev, _, _ = ParseWhen(map[string]any{"when": "every sunday"}, loc)
	if ev == nil || !ev.AllDay || ev.Starts.Weekday() != time.Sunday || ev.RRule != "FREQ=WEEKLY" {
		t.Errorf("every sunday: %+v", ev)
	}
	// "monthly" must never be read as a Monday.
	if _, err := ParseRepeats("monthly", time.Now(), loc); err != nil {
		t.Error(err)
	}
}

func TestLiftWhenRemovesKeys(t *testing.T) {
	meta := map[string]any{"title": "x", "when": "2026-10-04", "repeats": "weekly", "except": []any{"2026-10-11"}, "mood": "calm"}
	ev, warnings := LiftWhen(meta, la(t))
	if ev == nil || len(warnings) != 0 {
		t.Fatalf("lift: %v %v", ev, warnings)
	}
	if _, has := meta["when"]; has {
		t.Error("when still in meta")
	}
	if _, has := meta["repeats"]; has {
		t.Error("repeats still in meta")
	}
	if meta["mood"] != "calm" || meta["title"] != "x" {
		t.Error("other keys disturbed")
	}
	if len(ev.ExDates) != 1 || ev.ExDates[0] != "2026-10-11" {
		t.Errorf("exdates %v", ev.ExDates)
	}
}

// A weekly 7pm event stays at 7pm local across the fall-back change (Nov 1, 2026
// in Los Angeles), and skipped dates are honored.
func TestOccurrencesAcrossDST(t *testing.T) {
	loc := la(t)
	ev, _, _ := ParseWhen(map[string]any{"when": "2026-10-20 19:00 to 20:30", "repeats": "weekly", "except": []any{"2026-11-10"}}, loc)
	from := time.Date(2026, 10, 19, 0, 0, 0, 0, loc)
	to := time.Date(2026, 11, 20, 0, 0, 0, 0, loc)
	occs := ev.Occurrences(from, to, 0)
	var got []string
	for _, o := range occs {
		got = append(got, o.Starts.Format("Jan 2 15:04 -0700")+" – "+o.Ends.Format("15:04"))
	}
	want := []string{
		"Oct 20 19:00 -0700 – 20:30",
		"Oct 27 19:00 -0700 – 20:30",
		"Nov 3 19:00 -0800 – 20:30",
		"Nov 17 19:00 -0800 – 20:30",
	}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("occurrences:\n got %v\nwant %v", got, want)
	}
}

func TestOccurrencesUntilCountAndOneOff(t *testing.T) {
	loc := la(t)
	far := time.Date(2030, 1, 1, 0, 0, 0, 0, loc)
	ev, _, _ := ParseWhen(map[string]any{"when": "2026-10-06 19:00", "repeats": map[string]any{"every": "week", "count": 3}}, loc)
	if n := len(ev.Occurrences(time.Date(2026, 1, 1, 0, 0, 0, 0, loc), far, 0)); n != 3 {
		t.Errorf("count: %d occurrences, want 3", n)
	}
	ev, _, _ = ParseWhen(map[string]any{"when": "2026-10-06 19:00", "repeats": map[string]any{"every": "week", "until": "2026-10-27"}}, loc)
	if n := len(ev.Occurrences(time.Date(2026, 1, 1, 0, 0, 0, 0, loc), far, 0)); n != 4 {
		t.Errorf("until: %d occurrences, want 4 (Oct 6, 13, 20, 27)", n)
	}
	// Monthly on the last Friday.
	ev, _, _ = ParseWhen(map[string]any{"when": "2026-10-30 18:00", "repeats": map[string]any{"every": "month", "on": "last friday"}}, loc)
	occs := ev.Occurrences(time.Date(2026, 10, 1, 0, 0, 0, 0, loc), time.Date(2027, 1, 1, 0, 0, 0, 0, loc), 0)
	var days []string
	for _, o := range occs {
		days = append(days, o.Starts.Format("Jan 2"))
	}
	if strings.Join(days, ",") != "Oct 30,Nov 27,Dec 25" {
		t.Errorf("last friday: %v", days)
	}
	// One-off inside / outside the window; in-progress counts.
	ev, _, _ = ParseWhen(map[string]any{"when": "2026-10-04 10:00 to 16:00"}, loc)
	if len(ev.Occurrences(time.Date(2026, 10, 4, 12, 0, 0, 0, loc), far, 0)) != 1 {
		t.Error("in-progress one-off should be included")
	}
	if len(ev.Occurrences(time.Date(2026, 10, 4, 16, 0, 0, 0, loc), far, 0)) != 0 {
		t.Error("finished one-off should be excluded")
	}
	// Next.
	if n := ev.Next(time.Date(2026, 1, 1, 0, 0, 0, 0, loc)); n == nil || n.Starts.Day() != 4 {
		t.Errorf("next = %v", n)
	}
	if n := ev.Next(far); n != nil {
		t.Errorf("next after the end = %v, want nil", n)
	}
	// Window cap.
	ev, _, _ = ParseWhen(map[string]any{"when": "2026-01-01 09:00", "repeats": "daily"}, loc)
	if n := len(ev.Occurrences(time.Date(2026, 1, 1, 0, 0, 0, 0, loc), time.Date(2036, 1, 1, 0, 0, 0, 0, loc), 0)); n != MaxOccurrences {
		t.Errorf("cap: %d, want %d", n, MaxOccurrences)
	}
}

func TestAllDayOccurrenceEnds(t *testing.T) {
	loc := la(t)
	ev, _, _ := ParseWhen(map[string]any{"when": "2026-10-04 to 2026-10-06", "repeats": "monthly"}, loc)
	occs := ev.Occurrences(time.Date(2026, 11, 1, 0, 0, 0, 0, loc), time.Date(2026, 12, 1, 0, 0, 0, 0, loc), 0)
	if len(occs) != 1 || occs[0].Starts.Day() != 4 || occs[0].Ends.Day() != 6 || !occs[0].AllDay {
		t.Errorf("all-day monthly: %+v", occs)
	}
}

func TestDescribeRule(t *testing.T) {
	loc := la(t)
	cases := map[string]string{
		"":                                   "",
		"FREQ=WEEKLY":                        "weekly",
		"FREQ=DAILY":                         "daily",
		"FREQ=WEEKLY;INTERVAL=2":             "every 2 weeks",
		"FREQ=WEEKLY;BYDAY=TU,TH":            "weekly on Tue, Thu",
		"FREQ=MONTHLY;BYDAY=+1TU":            "monthly on the first Tuesday",
		"FREQ=MONTHLY;BYDAY=-1FR":            "monthly on the last Friday",
		"FREQ=MONTHLY;BYMONTHDAY=15":         "monthly on the 15th",
		"FREQ=YEARLY;UNTIL=20270701T065959Z": "yearly until Jun 30, 2027",
		"FREQ=WEEKLY;COUNT=12":               "weekly (12 times)",
	}
	for rule, want := range cases {
		if got := DescribeRule(rule, loc); got != want {
			t.Errorf("%q → %q, want %q", rule, got, want)
		}
	}
}

func TestFormatWhen(t *testing.T) {
	loc := la(t)
	now := time.Date(2026, 9, 24, 12, 0, 0, 0, loc)
	d := func(m time.Month, day, h, mi int) time.Time { return time.Date(2026, m, day, h, mi, 0, 0, loc) }
	cases := []struct {
		starts, ends time.Time
		allDay       bool
		want         string
	}{
		{d(10, 4, 10, 0), d(10, 4, 16, 0), false, "Sun Oct 4, 10 am – 4 pm"},
		{d(10, 4, 19, 30), time.Time{}, false, "Sun Oct 4, 7:30 pm"},
		{d(10, 4, 0, 0), time.Time{}, true, "Sun Oct 4"},
		{d(10, 4, 0, 0), d(10, 6, 0, 0), true, "Oct 4 – 6"},
		{d(10, 30, 0, 0), d(11, 2, 0, 0), true, "Fri Oct 30 – Mon Nov 2"},
		{d(10, 4, 22, 0), d(10, 5, 2, 0), false, "Sun Oct 4, 10 pm – Mon Oct 5, 2 am"},
		{time.Date(2027, 1, 9, 9, 0, 0, 0, loc), time.Time{}, false, "Sat Jan 9, 2027, 9 am"},
	}
	for _, c := range cases {
		if got := FormatWhen(c.starts, c.ends, c.allDay, now, ""); got != c.want {
			t.Errorf("got %q, want %q", got, c.want)
		}
	}
	if got := FormatWhen(d(10, 4, 10, 0), time.Time{}, false, now, "2 Jan 2006"); got != "4 Oct 2026, 10 am" {
		t.Errorf("custom layout: %q", got)
	}
}

func TestEventStorageRoundTrip(t *testing.T) {
	dir := t.TempDir()
	if err := writeFile(dir+"/friendo.toml", "[site]\nname = \"t\"\ntimezone = \"America/Los_Angeles\"\n"); err != nil {
		t.Fatal(err)
	}
	db, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if db.Location.String() != "America/Los_Angeles" {
		t.Fatalf("site location = %v", db.Location)
	}
	id, err := db.CreateRecord("events", "fair", "Fair", "", "published", "")
	if err != nil {
		t.Fatal(err)
	}
	ev, _, _ := ParseWhen(map[string]any{"when": "2026-10-04 10:00 to 16:00", "repeats": "weekly", "except": []any{"2026-10-11"}}, db.Location)
	if err := db.ReconcileWhen(id, ev); err != nil {
		t.Fatal(err)
	}
	got := db.EventFor(id)
	if got == nil {
		t.Fatal("event not stored")
	}
	if !got.Starts.Equal(ev.Starts) || got.Starts.Location().String() != "America/Los_Angeles" || got.Starts.Hour() != 10 {
		t.Errorf("starts round-trip: %v", got.Starts)
	}
	if got.Ends.Hour() != 16 || got.RRule != "FREQ=WEEKLY" || len(got.ExDates) != 1 || got.TargetID != id {
		t.Errorf("fields: %+v", got)
	}
	// Reconcile again with a new time: same id, updated.
	ev2, _, _ := ParseWhen(map[string]any{"when": "2026-10-05"}, db.Location)
	db.ReconcileWhen(id, ev2)
	all, _ := db.ListEvents()
	if len(all) != 1 || !all[0].AllDay || all[0].RRule != "" {
		t.Errorf("after update: %+v", all)
	}
	// Row round trip (sync).
	back := EventFromRow(all[0].Row())
	if back == nil || back.ID != all[0].ID || !back.Starts.Equal(all[0].Starts) || back.AllDay != true {
		t.Errorf("row round trip: %+v", back)
	}
	// Removing the when deletes the row.
	db.ReconcileWhen(id, nil)
	if db.EventFor(id) != nil {
		t.Error("row should be gone")
	}
}

func writeFile(path, body string) error {
	return os.WriteFile(path, []byte(body), 0o644)
}
