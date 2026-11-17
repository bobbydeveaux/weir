package spec

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule is a parsed cron expression: five fields, for minute, hour, day
// of the month, month and day of the week. Times are in UTC.
//
//	0 7 * * 1-5     at 07:00 on weekdays
//	*/15 * * * *    every fifteen minutes
//	30 2 1 * *      at 02:30 on the first of each month
//
// Each field is *, a number, a range (1-5), a list (1,3,5) or a step (*/15,
// 9-17/2). Days of the week run from 0 (Sunday) to 6, and 7 also means Sunday.
// When both the day of the month and the day of the week are restricted, a
// day matches if either does, as in other cron implementations.
type Schedule struct {
	minute, hour, dom, month, dow field
	domAny, dowAny                bool
}

// field is the set of values a cron field allows, as a bit per value.
type field uint64

func (f field) has(n int) bool { return f&(1<<uint(n)) != 0 }

// ParseSchedule reads a five-field cron expression.
func ParseSchedule(expr string) (*Schedule, error) {
	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("a schedule has five fields (minute, hour, day of month, month, day of week), and %q has %d", expr, len(parts))
	}
	s := &Schedule{domAny: parts[2] == "*", dowAny: parts[4] == "*"}
	var err error
	for _, f := range []struct {
		into     *field
		text     string
		name     string
		min, max int
	}{
		{&s.minute, parts[0], "minute", 0, 59}, {&s.hour, parts[1], "hour", 0, 23}, {&s.dom, parts[2], "day of month", 1, 31},
		{&s.month, parts[3], "month", 1, 12}, {&s.dow, parts[4], "day of week", 0, 7},
	} {
		if *f.into, err = parseField(f.text, f.min, f.max); err != nil {
			return nil, fmt.Errorf("%s: %w", f.name, err)
		}
	}
	if s.dow.has(7) {
		s.dow |= 1 // 7 is Sunday too
	}
	return s, nil
}

func parseField(text string, min, max int) (field, error) {
	var out field
	for _, part := range strings.Split(text, ",") {
		rng, stepText, hasStep := strings.Cut(part, "/")
		step := 1
		if hasStep {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return 0, fmt.Errorf("%q: the step after / must be a whole number above zero", part)
			}
			step = n
		}
		lo, hi := min, max
		if rng != "*" {
			a, b, isRange := strings.Cut(rng, "-")
			n, err := strconv.Atoi(a)
			if err != nil {
				return 0, fmt.Errorf("%q is not a number, a range or *", part)
			}
			lo, hi = n, n
			if isRange {
				if hi, err = strconv.Atoi(b); err != nil {
					return 0, fmt.Errorf("%q is not a range", part)
				}
			} else if hasStep {
				hi = max // "5/10" means from 5 onwards, every 10
			}
		}
		if lo < min || hi > max || lo > hi {
			return 0, fmt.Errorf("%q is outside %d to %d", part, min, max)
		}
		for n := lo; n <= hi; n += step {
			out |= 1 << uint(n)
		}
	}
	return out, nil
}

func (s *Schedule) dayMatches(t time.Time) bool {
	dom, dow := s.dom.has(t.Day()), s.dow.has(int(t.Weekday()))
	switch {
	case s.domAny && s.dowAny:
		return true
	case s.domAny:
		return dow
	case s.dowAny:
		return dom
	}
	return dom || dow
}

// Next returns the first time strictly after after that the schedule matches,
// in UTC. It returns the zero time if there is none within five years, which
// can only happen for a date that does not exist, such as 30 February.
func (s *Schedule) Next(after time.Time) time.Time {
	t := after.UTC().Truncate(time.Minute).Add(time.Minute)
	limit := t.AddDate(5, 0, 0)
	for t.Before(limit) {
		switch {
		case !s.month.has(int(t.Month())):
			t = time.Date(t.Year(), t.Month()+1, 1, 0, 0, 0, 0, time.UTC)
		case !s.dayMatches(t):
			t = time.Date(t.Year(), t.Month(), t.Day()+1, 0, 0, 0, 0, time.UTC)
		case !s.hour.has(t.Hour()):
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.UTC)
		case !s.minute.has(t.Minute()):
			t = t.Add(time.Minute)
		default:
			return t
		}
	}
	return time.Time{}
}
