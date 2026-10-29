// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package sql

import (
	"testing"
	"time"
)

func TestParseAndDisplaySQLTimestamp(t *testing.T) {
	beijing := time.FixedZone("", int((8 * time.Hour).Seconds()))
	fakeLosAngeles := time.FixedZone("", -int((8 * time.Hour).Seconds()))
	cases := []struct {
		s string
		t time.Time
	}{
		{"2010T", time.Date(2010, 1, 1, 0, 0, 0, 0, time.UTC)},
		{"2010-02T", time.Date(2010, 2, 1, 0, 0, 0, 0, time.UTC)},
		{"2010-02-03T", time.Date(2010, 2, 3, 0, 0, 0, 0, time.UTC)},
		{"2010-02-03T04:11Z", time.Date(2010, 2, 3, 4, 11, 0, 0, time.UTC)},
		{"2010-02-03T04:11:30Z", time.Date(2010, 2, 3, 4, 11, 30, 0, time.UTC)},
		{"2010-02-03T04:11:30.23Z", time.Date(2010, 2, 3, 4, 11, 30, 230000000, time.UTC)},
		{"2010-02-03T04:11+08:00", time.Date(2010, 2, 3, 4, 11, 0, 0, beijing)},
		{"2010-02-03T04:11:30+08:00", time.Date(2010, 2, 3, 4, 11, 30, 0, beijing)},
		{"2010-02-03T04:11:30.23+08:00", time.Date(2010, 2, 3, 4, 11, 30, 230000000, beijing)},
		{"2010-02-03T04:11:30-08:00", time.Date(2010, 2, 3, 4, 11, 30, 0, fakeLosAngeles)},
		{"2010-02-03T04:11:30.23-08:00", time.Date(2010, 2, 3, 4, 11, 30, 230000000, fakeLosAngeles)},
	}
	for i, tc := range cases {
		tval, err := parseSQLTimestamp(tc.s)
		if err != nil {
			t.Errorf("Case %d: Unexpected error: %v", i+1, err)
			continue
		}
		if !tval.Equal(tc.t) {
			t.Errorf("Case %d: Expected %v got %v", i+1, tc.t, tval)
			continue
		}

		tstr := FormatSQLTimestamp(tc.t)
		if tstr != tc.s {
			t.Errorf("Case %d: Expected %s got %s", i+1, tc.s, tstr)
			continue
		}
	}
}

func TestDateTrunc(t *testing.T) {
	kolkata := time.FixedZone("", int((5*time.Hour + 30*time.Minute).Seconds()))
	newfoundland := time.FixedZone("", -int((3*time.Hour+30*time.Minute).Seconds()))
	eastNeg := time.FixedZone("", -int((5 * time.Hour).Seconds()))

	cases := []struct {
		part    string
		input   time.Time
		want    time.Time
		wantStr string
	}{
		// Requirement example: +05:30 offset is retained on the result.
		{timePartDay,
			time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata),
			time.Date(2026, 3, 15, 0, 0, 0, 0, kolkata),
			"2026-03-15T00:00+05:30"},

		// All six units on the same non-integral-offset timestamp.
		{timePartYear,
			time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata),
			time.Date(2026, 1, 1, 0, 0, 0, 0, kolkata),
			"2026-01-01T00:00+05:30"},
		{timePartMonth,
			time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata),
			time.Date(2026, 3, 1, 0, 0, 0, 0, kolkata),
			"2026-03-01T00:00+05:30"},
		{timePartHour,
			time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata),
			time.Date(2026, 3, 15, 13, 0, 0, 0, kolkata),
			"2026-03-15T13:00+05:30"},
		{timePartMinute,
			time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata),
			time.Date(2026, 3, 15, 13, 24, 0, 0, kolkata),
			"2026-03-15T13:24+05:30"},
		{timePartSecond,
			time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata),
			time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata),
			"2026-03-15T13:24:58+05:30"},

		// Fractional seconds are dropped by SECOND truncation.
		{timePartSecond,
			time.Date(2026, 3, 15, 13, 24, 58, 654321000, kolkata),
			time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata),
			"2026-03-15T13:24:58+05:30"},

		// Year-end, UTC zone: compact layouts apply.
		{timePartYear,
			time.Date(2026, 12, 31, 23, 59, 59, 999999999, time.UTC),
			time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
			"2026T"},
		{timePartMonth,
			time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
			"2026-12T"},

		// Leap day.
		{timePartMonth,
			time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC),
			time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
			"2024-02T"},
		{timePartDay,
			time.Date(2024, 2, 29, 12, 34, 56, 0, time.UTC),
			time.Date(2024, 2, 29, 0, 0, 0, 0, time.UTC),
			"2024-02-29T"},

		// Pre-1970 date with a non-UTC offset.
		{timePartMonth,
			time.Date(1969, 7, 20, 20, 17, 40, 0, eastNeg),
			time.Date(1969, 7, 1, 0, 0, 0, 0, eastNeg),
			"1969-07-01T00:00-05:00"},
		{timePartYear,
			time.Date(1957, 10, 4, 19, 28, 34, 0, kolkata),
			time.Date(1957, 1, 1, 0, 0, 0, 0, kolkata),
			"1957-01-01T00:00+05:30"},

		// Negative, non-integral offset (-03:30).
		{timePartDay,
			time.Date(2026, 3, 15, 13, 24, 58, 0, newfoundland),
			time.Date(2026, 3, 15, 0, 0, 0, 0, newfoundland),
			"2026-03-15T00:00-03:30"},
		{timePartHour,
			time.Date(2026, 3, 15, 13, 24, 58, 0, newfoundland),
			time.Date(2026, 3, 15, 13, 0, 0, 0, newfoundland),
			"2026-03-15T13:00-03:30"},
	}

	for i, tc := range cases {
		v, err := dateTrunc(tc.part, tc.input)
		if err != nil {
			t.Errorf("Case %d: unexpected error: %v", i+1, err)
			continue
		}
		got, ok := v.ToTimestamp()
		if !ok {
			t.Errorf("Case %d: result is not a timestamp", i+1)
			continue
		}
		if !got.Equal(tc.want) {
			t.Errorf("Case %d: instant mismatch\n got: %v\nwant: %v", i+1, got, tc.want)
		}
		gy, gm, gd := got.Date()
		gh, gmi, gs := got.Clock()
		wy, wm, wd := tc.want.Date()
		wh, wmi, ws := tc.want.Clock()
		if (gy != wy) || (gm != wm) || (gd != wd) ||
			(gh != wh) || (gmi != wmi) || (gs != ws) || got.Nanosecond() != 0 {
			t.Errorf("Case %d: calendar fields mismatch\n got: %04d-%02d-%02d %02d:%02d:%02d.%09d\nwant: %04d-%02d-%02d %02d:%02d:%02d.000000000",
				i+1, gy, gm, gd, gh, gmi, gs, got.Nanosecond(),
				wy, wm, wd, wh, wmi, ws)
		}
		gotOff, wantOff := offsetSeconds(got), offsetSeconds(tc.want)
		if gotOff != wantOff {
			t.Errorf("Case %d: zone offset mismatch: got %d want %d", i+1, gotOff, wantOff)
		}
		if gotStr := FormatSQLTimestamp(got); gotStr != tc.wantStr {
			t.Errorf("Case %d: formatted mismatch: got %q want %q", i+1, gotStr, tc.wantStr)
		}
	}

	// Unknown units must error, never silently return the input.
	for _, part := range []string{"WEEK", "", "MILLISECOND"} {
		in := time.Date(2026, 3, 15, 13, 24, 58, 0, kolkata)
		if v, err := dateTrunc(part, in); err == nil {
			t.Errorf("unit %q: expected error, got %v", part, v)
		}
	}
}

func offsetSeconds(t time.Time) int {
	_, off := t.Zone()
	return off
}
