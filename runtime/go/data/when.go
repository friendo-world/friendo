package data

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/teambition/rrule-go"
)

// Parsing a post's `when`.
//
// The reserved keys — in a content file's front matter and in a <friendo-form>'s
// field names alike — are flat, so the two are spelled the same:
//
//	when: 2026-10-04 19:00 to 21:00      # or 2026-10-04 (all day), 2026-10-04 19:00,
//	                                     #    2026-10-04 to 2026-10-06, mondays 19:00 to 20:30
//	ends: 2026-10-04 21:00               # or a bare time: 21:00 / 9pm
//	timezone: America/Los_Angeles        # default: the site's
//	repeats: weekly                      # daily | weekly | monthly | yearly | every 2 weeks
//	repeats: {every: month, on: first tuesday, until: 2027-06-30}   # or count: 12
//	except: [2026-11-25]                 # skipped dates
//	rrule: "FREQ=WEEKLY;BYDAY=TU,TH"     # escape hatch, stored verbatim
//
// ParseWhen is the one parser both the content importer and the record API use,
// so the two ways of writing an event can't drift apart.

// WhenKeys are the reserved names ParseWhen consumes. Callers delete them from
// the record's free-form data after lifting, as the importer does for `location`.
var WhenKeys = []string{"when", "ends", "timezone", "repeats", "except", "rrule", "all_day"}

// HasWhen reports whether a field set declares a time.
func HasWhen(meta map[string]any) bool {
	_, ok := meta["when"]
	return ok
}

// ParseWhen reads the reserved keys into a series. present is false when there
// is no `when` at all. A present but unusable `when` yields ev == nil and a
// warning; lesser problems (a bad end, a bad zone) yield warnings and a best
// effort. siteLoc is the zone a time without one is read in.
func ParseWhen(meta map[string]any, siteLoc *time.Location) (ev *Event, warnings []string, present bool) {
	raw, present := meta["when"]
	if !present {
		return nil, nil, false
	}
	if siteLoc == nil {
		siteLoc = time.UTC
	}
	warn := func(format string, args ...any) { warnings = append(warnings, fmt.Sprintf(format, args...)) }

	loc := siteLoc
	tzName := ""
	if tz := strings.TrimSpace(stringOf(meta["timezone"])); tz != "" {
		if l, err := time.LoadLocation(tz); err == nil {
			loc, tzName = l, tz
		} else {
			warn("timezone %q is not a known zone (use an IANA name like Europe/Paris); using %s", tz, siteLoc)
		}
	}

	spec, err := parseWhenValue(raw, loc, time.Now().In(loc))
	if err != nil {
		warn("when: %v", err)
		return nil, warnings, true
	}
	ev = &Event{Starts: spec.starts, Ends: spec.ends, AllDay: spec.allDay, Timezone: tzName, loc: loc}

	if v, ok := meta["ends"]; ok && v != nil && stringOf(v) != "" {
		end, err := parseEndValue(v, ev.Starts, loc)
		if err != nil {
			warn("ends: %v", err)
		} else {
			ev.Ends = end
			if end.Hour() != 0 || end.Minute() != 0 || !spec.allDay {
				// An explicit clock time on the end makes a timed event.
				ev.AllDay = false
			}
		}
	}
	if v, ok := meta["all_day"]; ok {
		switch b := v.(type) {
		case bool:
			ev.AllDay = b
		case string:
			ev.AllDay = strings.EqualFold(b, "true") || b == "1" || strings.EqualFold(b, "yes")
		}
	}
	if ev.AllDay {
		ev.Starts = dateOnly(ev.Starts)
		if !ev.Ends.IsZero() {
			ev.Ends = dateOnly(ev.Ends)
			if ev.Ends.Equal(ev.Starts) {
				ev.Ends = time.Time{}
			}
		}
	}
	if !ev.Ends.IsZero() && ev.Ends.Before(ev.Starts) {
		warn("ends (%s) is before when (%s); ignoring the end", ev.Ends.Format("2006-01-02 15:04"), ev.Starts.Format("2006-01-02 15:04"))
		ev.Ends = time.Time{}
	}

	// Recurrence: an explicit rrule wins; else `repeats`; else the weekday sugar.
	if r := strings.TrimSpace(stringOf(meta["rrule"])); r != "" {
		if _, err := rrule.StrToROptionInLocation(strings.TrimPrefix(r, "RRULE:"), loc); err != nil {
			warn("rrule %q: %v", r, err)
		} else {
			ev.RRule = strings.TrimPrefix(r, "RRULE:")
		}
	} else if v, ok := meta["repeats"]; ok && v != nil {
		rule, err := ParseRepeats(v, ev.Starts, loc)
		if err != nil {
			warn("repeats: %v", err)
		} else {
			ev.RRule = rule
		}
	} else if spec.weekly {
		ev.RRule = "FREQ=WEEKLY"
	}

	if v, ok := meta["except"]; ok && v != nil {
		for _, item := range listOf(v) {
			d, err := parseDateValue(item, loc)
			if err != nil {
				warn("except: %v", err)
				continue
			}
			ev.ExDates = append(ev.ExDates, d.Format("2006-01-02"))
		}
	}
	return ev, warnings, true
}

// LiftWhen parses the reserved keys out of a record's fields and removes them,
// returning the series (nil if the record declares no usable time). Warnings are
// for the caller to surface.
func LiftWhen(meta map[string]any, siteLoc *time.Location) (*Event, []string) {
	ev, warnings, _ := ParseWhen(meta, siteLoc)
	for _, k := range WhenKeys {
		delete(meta, k)
	}
	return ev, warnings
}

type whenSpec struct {
	starts, ends time.Time
	allDay       bool
	weekly       bool // weekday sugar ("mondays 19:00")
}

var (
	weekdayNames = map[string]time.Weekday{
		"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday, "thu": time.Thursday,
		"fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
	}
	weekdaySugarRe = regexp.MustCompile(`^(?:every\s+)?(mon|tue|wed|thu|fri|sat|sun)(?:day|sday|nesday|rsday|urday)?s?\b\s*(.*)$`)
	rangeSplitRe   = regexp.MustCompile(`\s+(?:to|-|–|—|until)\s+`)
)

// parseWhenValue reads the `when` value: a string in one of the documented
// shapes, or a time.Time (YAML decodes a bare 2026-10-04 to one).
func parseWhenValue(v any, loc *time.Location, now time.Time) (whenSpec, error) {
	switch t := v.(type) {
	case time.Time:
		w := inLocation(t, loc)
		return whenSpec{starts: w, allDay: isMidnight(w)}, nil
	case map[string]any:
		// Tolerate {starts, ends} from an API client.
		if s, ok := t["starts"]; ok {
			spec, err := parseWhenValue(s, loc, now)
			if err != nil {
				return spec, err
			}
			if e, ok := t["ends"]; ok && e != nil {
				if end, err := parseEndValue(e, spec.starts, loc); err == nil {
					spec.ends = end
				}
			}
			return spec, nil
		}
		return whenSpec{}, fmt.Errorf("expected a date like 2026-10-04 19:00, got a map")
	}
	s := strings.TrimSpace(stringOf(v))
	if s == "" {
		return whenSpec{}, fmt.Errorf("is empty")
	}

	// Weekday sugar: "mondays 19:00 to 20:30", "every tuesday", "sundays".
	if m := weekdaySugarRe.FindStringSubmatch(strings.ToLower(s)); m != nil {
		wd := weekdayNames[m[1]]
		day := dateOnly(now)
		for day.Weekday() != wd {
			day = day.AddDate(0, 0, 1)
		}
		spec := whenSpec{starts: day, allDay: true, weekly: true}
		if rest := strings.TrimSpace(m[2]); rest != "" {
			parts := rangeSplitRe.Split(rest, 2)
			start, err := parseClock(parts[0])
			if err != nil {
				return spec, fmt.Errorf("%q: %v", s, err)
			}
			spec.starts = day.Add(start)
			spec.allDay = false
			if len(parts) == 2 {
				end, err := parseClock(parts[1])
				if err != nil {
					return spec, fmt.Errorf("%q: %v", s, err)
				}
				spec.ends = day.Add(end)
				if spec.ends.Before(spec.starts) {
					spec.ends = spec.ends.AddDate(0, 0, 1)
				}
			}
		}
		return spec, nil
	}

	parts := rangeSplitRe.Split(s, 2)
	start, startAllDay, err := parseDateTime(parts[0], loc)
	if err != nil {
		return whenSpec{}, err
	}
	spec := whenSpec{starts: start, allDay: startAllDay}
	if len(parts) == 2 {
		end, err := parseEndString(parts[1], start, loc)
		if err != nil {
			return spec, err
		}
		spec.ends = end
		if !isMidnight(end) || !startAllDay {
			spec.allDay = false
		}
	}
	return spec, nil
}

// parseEndValue reads `ends`: a full date-time, a bare date, or a clock time on
// the start's day.
func parseEndValue(v any, start time.Time, loc *time.Location) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return inLocation(t, loc), nil
	}
	return parseEndString(stringOf(v), start, loc)
}

func parseEndString(s string, start time.Time, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if d, err := parseClock(s); err == nil {
		end := dateOnly(start).Add(d)
		if end.Before(start) {
			end = end.AddDate(0, 0, 1) // "22:00 to 02:00" crosses midnight
		}
		return end, nil
	}
	end, _, err := parseDateTime(s, loc)
	return end, err
}

var dateTimeLayouts = []string{
	"2006-01-02 15:04:05", "2006-01-02T15:04:05", "2006-01-02 15:04", "2006-01-02T15:04",
	"2006-01-02 3:04pm", "2006-01-02 3:04 pm", "2006-01-02 3pm", "2006-01-02 3 pm",
	"2006-01-02 3:04PM", "2006-01-02 3:04 PM", "2006-01-02 3PM", "2006-01-02 3 PM",
}

// parseDateTime reads "2026-10-04", "2026-10-04 19:00", "2026-10-04T19:00",
// "2026-10-04 7pm", or an RFC 3339 stamp (an explicit offset is converted into
// loc). allDay is true for a bare date.
func parseDateTime(s string, loc *time.Location) (t time.Time, allDay bool, err error) {
	s = strings.TrimSpace(s)
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(loc), false, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", s, loc); err == nil {
		return t, true, nil
	}
	for _, layout := range dateTimeLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, false, nil
		}
	}
	return time.Time{}, false, fmt.Errorf("%q is not a date I understand (try 2026-10-04 or 2026-10-04 19:00)", s)
}

// parseDateValue reads a bare date (for `except` and `until`).
func parseDateValue(v any, loc *time.Location) (time.Time, error) {
	if t, ok := v.(time.Time); ok {
		return dateOnly(inLocation(t, loc)), nil
	}
	t, _, err := parseDateTime(stringOf(v), loc)
	if err != nil {
		return t, err
	}
	return dateOnly(t), nil
}

var clockLayouts = []string{"15:04", "15:04:05", "3:04pm", "3:04 pm", "3pm", "3 pm", "3:04PM", "3:04 PM", "3PM", "3 PM"}

// parseClock reads a time of day ("19:00", "7pm", "7:30 pm") as an offset from midnight.
func parseClock(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	for _, layout := range clockLayouts {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Duration(t.Hour())*time.Hour + time.Duration(t.Minute())*time.Minute + time.Duration(t.Second())*time.Second, nil
		}
	}
	return 0, fmt.Errorf("%q is not a time of day (try 19:00 or 7pm)", s)
}

func dateOnly(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, t.Location())
}

func isMidnight(t time.Time) bool {
	return t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0
}

func stringOf(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case time.Time:
		return t.Format("2006-01-02 15:04:05")
	case int:
		return strconv.Itoa(t)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}

func listOf(v any) []any {
	switch t := v.(type) {
	case []any:
		return t
	case []string:
		out := make([]any, len(t))
		for i, s := range t {
			out[i] = s
		}
		return out
	case nil:
		return nil
	default:
		// A comma-separated string: "2026-11-25, 2026-12-23".
		var out []any
		for _, p := range strings.Split(stringOf(v), ",") {
			if p = strings.TrimSpace(p); p != "" {
				out = append(out, p)
			}
		}
		return out
	}
}

// --- repeats ---

var everyRe = regexp.MustCompile(`^(?:every\s+)?(?:(\d+)\s+)?(day|week|month|year)s?$`)

// ParseRepeats turns the `repeats` value into an RRULE body. A string:
// "daily" | "weekly" | "monthly" | "yearly" | "every 2 weeks" | "weekdays". A map:
// {every, on, until, count}.
func ParseRepeats(v any, start time.Time, loc *time.Location) (string, error) {
	opt := rrule.ROption{}
	switch t := v.(type) {
	case string:
		s := strings.ToLower(strings.TrimSpace(t))
		if s == "" || s == "never" || s == "once" || s == "no" || s == "none" {
			return "", nil
		}
		switch s {
		case "daily", "every day":
			opt.Freq = rrule.DAILY
		case "weekly", "every week":
			opt.Freq = rrule.WEEKLY
		case "monthly", "every month":
			opt.Freq = rrule.MONTHLY
		case "yearly", "annually", "every year":
			opt.Freq = rrule.YEARLY
		case "weekdays", "every weekday":
			opt.Freq = rrule.WEEKLY
			opt.Byweekday = []rrule.Weekday{rrule.MO, rrule.TU, rrule.WE, rrule.TH, rrule.FR}
		case "fortnightly", "biweekly", "every other week":
			opt.Freq = rrule.WEEKLY
			opt.Interval = 2
		default:
			if m := everyRe.FindStringSubmatch(s); m != nil {
				opt.Freq = freqOf(m[2])
				if m[1] != "" {
					opt.Interval, _ = strconv.Atoi(m[1])
				}
			} else if wd, ok := weekdayNames[firstThree(s)]; ok && weekdaySugarRe.MatchString(s) {
				opt.Freq = rrule.WEEKLY
				opt.Byweekday = []rrule.Weekday{toRRuleWeekday(wd)}
			} else {
				return "", fmt.Errorf("%q — use daily, weekly, monthly, yearly, or every 2 weeks", t)
			}
		}
	case map[string]any:
		every := strings.ToLower(strings.TrimSpace(stringOf(t["every"])))
		if every == "" {
			return "", fmt.Errorf("needs `every: day | week | month | year`")
		}
		m := everyRe.FindStringSubmatch(every)
		if m == nil {
			return "", fmt.Errorf("every: %q — use day, week, month, year, or 2 weeks", every)
		}
		opt.Freq = freqOf(m[2])
		if m[1] != "" {
			opt.Interval, _ = strconv.Atoi(m[1])
		}
		if iv, ok := t["interval"]; ok {
			if n, err := strconv.Atoi(stringOf(iv)); err == nil && n > 0 {
				opt.Interval = n
			}
		}
		if on, ok := t["on"]; ok && on != nil {
			if err := applyOn(&opt, on); err != nil {
				return "", err
			}
		}
		if u, ok := t["until"]; ok && u != nil && stringOf(u) != "" {
			d, err := parseDateValue(u, loc)
			if err != nil {
				return "", fmt.Errorf("until: %v", err)
			}
			// Through the end of that day, expressed in UTC as RFC 5545 asks.
			opt.Until = d.AddDate(0, 0, 1).Add(-time.Second).UTC()
		}
		if c, ok := t["count"]; ok && c != nil {
			n, err := strconv.Atoi(stringOf(c))
			if err != nil || n <= 0 {
				return "", fmt.Errorf("count: %q is not a whole number", stringOf(c))
			}
			opt.Count = n
		}
	default:
		return "", fmt.Errorf("%v — use a word like weekly, or every/on/until", v)
	}
	if opt.Interval == 1 {
		opt.Interval = 0
	}
	rule := opt.RRuleString()
	// Round-trip through the parser so a bad combination is caught now.
	if _, err := rrule.StrToROptionInLocation(rule, loc); err != nil {
		return "", err
	}
	return rule, nil
}

// applyOn reads `on`: weekday names (weekly), "first tuesday" / "last friday" /
// a day-of-month number (monthly), or a list of those.
func applyOn(opt *rrule.ROption, on any) error {
	for _, item := range listOf(on) {
		s := strings.ToLower(strings.TrimSpace(stringOf(item)))
		if s == "" {
			continue
		}
		if n, err := strconv.Atoi(s); err == nil {
			if n < 1 || n > 31 {
				return fmt.Errorf("on: %d is not a day of the month", n)
			}
			opt.Bymonthday = append(opt.Bymonthday, n)
			continue
		}
		words := strings.Fields(s)
		nth := 0
		if len(words) == 2 {
			switch words[0] {
			case "first", "1st":
				nth = 1
			case "second", "2nd":
				nth = 2
			case "third", "3rd":
				nth = 3
			case "fourth", "4th":
				nth = 4
			case "last":
				nth = -1
			default:
				return fmt.Errorf("on: %q — try first tuesday, last friday, or 15", s)
			}
			words = words[1:]
		}
		wd, ok := weekdayNames[firstThree(words[0])]
		if !ok || len(words) != 1 {
			return fmt.Errorf("on: %q — try tue, first tuesday, last friday, or 15", s)
		}
		day := toRRuleWeekday(wd)
		if nth != 0 {
			day = day.Nth(nth)
		}
		opt.Byweekday = append(opt.Byweekday, day)
	}
	return nil
}

func firstThree(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if len(s) > 3 {
		return s[:3]
	}
	return s
}

func freqOf(unit string) rrule.Frequency {
	switch unit {
	case "day":
		return rrule.DAILY
	case "week":
		return rrule.WEEKLY
	case "month":
		return rrule.MONTHLY
	default:
		return rrule.YEARLY
	}
}

func toRRuleWeekday(wd time.Weekday) rrule.Weekday {
	switch wd {
	case time.Monday:
		return rrule.MO
	case time.Tuesday:
		return rrule.TU
	case time.Wednesday:
		return rrule.WE
	case time.Thursday:
		return rrule.TH
	case time.Friday:
		return rrule.FR
	case time.Saturday:
		return rrule.SA
	default:
		return rrule.SU
	}
}

var weekdayLong = map[rrule.Weekday]string{
	rrule.MO: "Monday", rrule.TU: "Tuesday", rrule.WE: "Wednesday", rrule.TH: "Thursday",
	rrule.FR: "Friday", rrule.SA: "Saturday", rrule.SU: "Sunday",
}

// DescribeRule renders an RRULE body as a person would say it: "weekly",
// "every 2 weeks on Tue, Thu", "monthly on the first Tuesday", "yearly until
// Jun 30, 2027", "daily (12 times)". "" for no rule.
func DescribeRule(rule string, loc *time.Location) string {
	if rule == "" {
		return ""
	}
	opt, err := rrule.StrToROptionInLocation(rule, loc)
	if err != nil {
		return "repeats"
	}
	unit := map[rrule.Frequency]string{rrule.DAILY: "day", rrule.WEEKLY: "week", rrule.MONTHLY: "month", rrule.YEARLY: "year"}[opt.Freq]
	word := map[rrule.Frequency]string{rrule.DAILY: "daily", rrule.WEEKLY: "weekly", rrule.MONTHLY: "monthly", rrule.YEARLY: "yearly"}[opt.Freq]
	if unit == "" {
		return "repeats"
	}
	var b strings.Builder
	if opt.Interval > 1 {
		fmt.Fprintf(&b, "every %d %ss", opt.Interval, unit)
	} else {
		b.WriteString(word)
	}
	if len(opt.Byweekday) > 0 {
		names := make([]string, 0, len(opt.Byweekday))
		nth := false
		for _, wd := range opt.Byweekday {
			n := wd.N()
			plain := weekdayLong[baseWeekday(wd)]
			switch {
			case n == -1:
				names = append(names, "last "+plain)
				nth = true
			case n > 0:
				names = append(names, []string{"", "first", "second", "third", "fourth", "fifth"}[n]+" "+plain)
				nth = true
			default:
				names = append(names, plain[:3])
			}
		}
		if nth {
			b.WriteString(" on the " + strings.Join(names, ", "))
		} else {
			b.WriteString(" on " + strings.Join(names, ", "))
		}
	}
	if len(opt.Bymonthday) > 0 {
		days := make([]string, 0, len(opt.Bymonthday))
		for _, d := range opt.Bymonthday {
			days = append(days, ordinal(d))
		}
		b.WriteString(" on the " + strings.Join(days, ", "))
	}
	if !opt.Until.IsZero() {
		b.WriteString(" until " + opt.Until.In(loc).Format("Jan 2, 2006"))
	}
	if opt.Count > 0 {
		fmt.Fprintf(&b, " (%d times)", opt.Count)
	}
	return b.String()
}

// baseWeekday strips an nth from an rrule weekday (rrule.MO.Nth(2) → rrule.MO).
func baseWeekday(wd rrule.Weekday) rrule.Weekday {
	switch wd.Day() {
	case 0:
		return rrule.MO
	case 1:
		return rrule.TU
	case 2:
		return rrule.WE
	case 3:
		return rrule.TH
	case 4:
		return rrule.FR
	case 5:
		return rrule.SA
	default:
		return rrule.SU
	}
}

func ordinal(n int) string {
	suffix := "th"
	switch {
	case n%100 >= 11 && n%100 <= 13:
	case n%10 == 1:
		suffix = "st"
	case n%10 == 2:
		suffix = "nd"
	case n%10 == 3:
		suffix = "rd"
	}
	return strconv.Itoa(n) + suffix
}

// --- formatting ---

// FormatWhen prints a start/end the way a person would, e.g.
// "Sat Oct 4, 10:00 am – 4:00 pm", "Sat Oct 4, 7:00 pm", "Sat Oct 4" (all day),
// "Oct 4 – 6" (multi-day), "Sat Oct 4, 7:00 pm – Sun Oct 5, 2:00 am". The year is
// added when it isn't the current one. dateLayout overrides the date part
// ("" = "Mon Jan 2").
func FormatWhen(starts, ends time.Time, allDay bool, now time.Time, dateLayout string) string {
	if starts.IsZero() {
		return ""
	}
	if dateLayout == "" {
		dateLayout = "Mon Jan 2"
	}
	withYear := func(t time.Time) string {
		s := t.Format(dateLayout)
		if t.Year() != now.Year() && !strings.Contains(dateLayout, "2006") {
			s += ", " + t.Format("2006")
		}
		return s
	}
	clock := func(t time.Time) string {
		if t.Minute() == 0 {
			return strings.ToLower(t.Format("3 pm"))
		}
		return strings.ToLower(t.Format("3:04 pm"))
	}
	sameDay := func(a, b time.Time) bool {
		return a.Year() == b.Year() && a.YearDay() == b.YearDay()
	}
	if allDay {
		if ends.IsZero() || sameDay(starts, ends) {
			return withYear(starts)
		}
		if starts.Year() == ends.Year() && starts.Month() == ends.Month() && dateLayout == "Mon Jan 2" {
			s := fmt.Sprintf("%s %d – %d", starts.Format("Jan"), starts.Day(), ends.Day())
			if starts.Year() != now.Year() {
				s += ", " + starts.Format("2006")
			}
			return s
		}
		return withYear(starts) + " – " + withYear(ends)
	}
	if ends.IsZero() {
		return withYear(starts) + ", " + clock(starts)
	}
	if sameDay(starts, ends) {
		return withYear(starts) + ", " + clock(starts) + " – " + clock(ends)
	}
	return withYear(starts) + ", " + clock(starts) + " – " + withYear(ends) + ", " + clock(ends)
}
