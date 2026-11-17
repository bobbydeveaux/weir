package spec

import (
	"testing"
	"time"
)

func TestASchedulePicksTheRightNextTime(t *testing.T) {
	at := func(s string) time.Time {
		v, err := time.Parse("2006-01-02 15:04", s)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	// 17 November 2026 is a Tuesday.
	for _, tc := range []struct{ expr, after, want string }{
		{"* * * * *", "2026-11-17 10:00", "2026-11-17 10:01"},
		{"*/15 * * * *", "2026-11-17 10:07", "2026-11-17 10:15"},
		{"*/15 * * * *", "2026-11-17 10:45", "2026-11-17 11:00"},
		{"0 7 * * *", "2026-11-17 06:59", "2026-11-17 07:00"},
		{"0 7 * * *", "2026-11-17 07:00", "2026-11-18 07:00"},   // strictly after
		{"0 7 * * 1-5", "2026-11-20 08:00", "2026-11-23 07:00"}, // Friday morning to Monday
		{"0 7 * * 0", "2026-11-17 08:00", "2026-11-22 07:00"},   // Sunday
		{"0 7 * * 7", "2026-11-17 08:00", "2026-11-22 07:00"},   // 7 is Sunday too
		{"30 2 1 * *", "2026-11-17 10:00", "2026-12-01 02:30"},
		{"0 0 1 1 *", "2026-11-17 10:00", "2027-01-01 00:00"},
		{"0 9-17/4 * * *", "2026-11-17 10:00", "2026-11-17 13:00"},
		{"5,35 * * * *", "2026-11-17 10:05", "2026-11-17 10:35"},
		{"0 0 29 2 *", "2026-11-17 10:00", "2028-02-29 00:00"}, // the next leap day
		{"0 0 13 * 5", "2026-11-17 10:00", "2026-11-20 00:00"}, // the 13th or a Friday, whichever is first
		{"59 23 31 12 *", "2026-12-31 23:59", "2027-12-31 23:59"},
	} {
		s, err := ParseSchedule(tc.expr)
		if err != nil {
			t.Errorf("%q: %v", tc.expr, err)
			continue
		}
		if got := s.Next(at(tc.after)); !got.Equal(at(tc.want)) {
			t.Errorf("%q after %s = %s, want %s", tc.expr, tc.after, got.Format("2006-01-02 15:04 Mon"), tc.want)
		}
	}
	// A time in another zone is read as the same instant in UTC.
	s, _ := ParseSchedule("0 7 * * *")
	paris := time.Date(2026, 11, 17, 7, 30, 0, 0, time.FixedZone("CET", 3600)) // 06:30 UTC
	if got := s.Next(paris); !got.Equal(at("2026-11-17 07:00")) {
		t.Errorf("from 07:30 in Paris the next 07:00 UTC is %s", got)
	}
	if s, _ := ParseSchedule("0 0 30 2 *"); !s.Next(at("2026-11-17 10:00")).IsZero() {
		t.Error("30 February was given a next time")
	}
	for _, bad := range []string{"", "* * * *", "* * * * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *", "* * * * 8",
		"a * * * *", "*/0 * * * *", "5-1 * * * *", "1- * * * *", "every morning"} {
		if _, err := ParseSchedule(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
}
