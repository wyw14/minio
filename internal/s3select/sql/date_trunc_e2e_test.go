// Copyright (c) 2015-2026 MinIO, Inc.
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

package sql_test

import (
	"testing"

	"github.com/minio/minio/internal/s3select/json"
	"github.com/minio/minio/internal/s3select/jstream"
	"github.com/minio/minio/internal/s3select/sql"
)

const tsCol = "2026-03-15T13:24:58+05:30"

// runSelect parses and evaluates a non-aggregating SELECT statement
// against a single JSON record. The second return value reports whether
// the record passed the WHERE clause and was emitted.
func runSelect(t *testing.T, query string, in jstream.KVS) (jstream.KVS, bool, error) {
	t.Helper()
	stmt, err := sql.ParseSelectStatement(query)
	if err != nil {
		return nil, false, err
	}
	input := json.NewRecord(sql.SelectFmtJSON)
	if err = input.Replace(in); err != nil {
		t.Fatal(err)
	}
	output := json.NewRecord(sql.SelectFmtJSON)
	res, err := stmt.Eval(input, output)
	if err != nil {
		return nil, false, err
	}
	if res == nil {
		return nil, false, nil
	}
	return res.(*json.Record).KVS, true, nil
}

func kv(t *testing.T, kvs jstream.KVS, key string) any {
	t.Helper()
	for _, kv := range kvs {
		if kv.Key == key {
			return kv.Value
		}
	}
	t.Fatalf("key %q not found in output record: %#v", key, kvs)
	return nil
}

func TestDateTruncProjection(t *testing.T) {
	// Bare time-unit keyword, lower case, cast literal argument.
	out, ok, err := runSelect(t,
		"SELECT DATE_TRUNC(day, CAST('"+tsCol+"' AS TIMESTAMP)) AS t FROM S3Object",
		jstream.KVS{})
	if err != nil || !ok {
		t.Fatalf("unexpected: ok=%v err=%v", ok, err)
	}
	if got := kv(t, out, "t"); got != "2026-03-15T00:00+05:30" {
		t.Errorf("bare keyword: got %v want %q", got, "2026-03-15T00:00+05:30")
	}

	// String-literal unit with mixed case and a string timestamp column.
	out, ok, err = runSelect(t,
		"SELECT DATE_TRUNC('DaY', ts) AS t FROM S3Object",
		jstream.KVS{{Key: "ts", Value: tsCol}})
	if err != nil || !ok {
		t.Fatalf("unexpected: ok=%v err=%v", ok, err)
	}
	if got := kv(t, out, "t"); got != "2026-03-15T00:00+05:30" {
		t.Errorf("string unit: got %v want %q", got, "2026-03-15T00:00+05:30")
	}
}

// The unit is read from a column, so different rows truncate to
// different calendar units with the same query.
func TestDateTruncPerRowUnits(t *testing.T) {
	query := "SELECT DATE_TRUNC(unit, ts) AS t FROM S3Object"
	cases := []struct {
		unit string
		want string
	}{
		{"year", "2026-01-01T00:00+05:30"},
		{"MONTH", "2026-03-01T00:00+05:30"},
		{"day", "2026-03-15T00:00+05:30"},
		{"Hour", "2026-03-15T13:00+05:30"},
		{"minute", "2026-03-15T13:24+05:30"},
		{"SECOND", "2026-03-15T13:24:58+05:30"},
	}
	for i, tc := range cases {
		out, ok, err := runSelect(t, query,
			jstream.KVS{
				{Key: "unit", Value: tc.unit},
				{Key: "ts", Value: tsCol},
			})
		if err != nil || !ok {
			t.Fatalf("case %d: unexpected: ok=%v err=%v", i+1, ok, err)
		}
		if got := kv(t, out, "t"); got != tc.want {
			t.Errorf("case %d unit %q: got %v want %q", i+1, tc.unit, got, tc.want)
		}
	}
}

func TestDateTruncWhereAndComparison(t *testing.T) {
	// Row within the day passes a WHERE against local midnight in the
	// same offset; a row from the next day does not.
	passQuery := "SELECT ts FROM S3Object WHERE DATE_TRUNC('day', ts) = CAST('2026-03-15T00:00:00+05:30' AS TIMESTAMP)"
	in := jstream.KVS{{Key: "ts", Value: tsCol}}
	if _, ok, err := runSelect(t, passQuery, in); err != nil || !ok {
		t.Errorf("same-day row: expected pass, got ok=%v err=%v", ok, err)
	}
	in = jstream.KVS{{Key: "ts", Value: "2026-03-16T13:24:58+05:30"}}
	if _, ok, err := runSelect(t, passQuery, in); err != nil || ok {
		t.Errorf("next-day row: expected filter, got ok=%v err=%v", ok, err)
	}

	// HOUR truncation compared against the local hour boundary.
	hourQuery := "SELECT ts FROM S3Object WHERE DATE_TRUNC('hour', CAST('" + tsCol + "' AS TIMESTAMP)) = CAST('2026-03-15T13:00:00+05:30' AS TIMESTAMP)"
	if _, ok, err := runSelect(t, hourQuery, jstream.KVS{}); err != nil || !ok {
		t.Errorf("hour comparison: expected pass, got ok=%v err=%v", ok, err)
	}

	// Truncated timestamps are usable in a projection comparison.
	out, ok, err := runSelect(t,
		"SELECT DATE_TRUNC('day', a) = DATE_TRUNC('day', b) AS eq FROM S3Object",
		jstream.KVS{
			{Key: "a", Value: "2026-03-15T01:00:00+05:30"},
			{Key: "b", Value: "2026-03-15T23:59:59+05:30"},
		})
	if err != nil || !ok {
		t.Fatalf("unexpected: ok=%v err=%v", ok, err)
	}
	if got := kv(t, out, "eq"); got != true {
		t.Errorf("same-day equality: got %v want true", got)
	}
}

func TestDateTruncNullAndMissing(t *testing.T) {
	cases := []struct {
		name  string
		query string
		in    jstream.KVS
	}{
		{"null timestamp", "SELECT DATE_TRUNC('day', NULL) AS t FROM S3Object", nil},
		{"missing timestamp", "SELECT DATE_TRUNC('day', nope) AS t FROM S3Object", nil},
		{"null unit", "SELECT DATE_TRUNC(NULL, CAST('" + tsCol + "' AS TIMESTAMP)) AS t FROM S3Object", nil},
		{"missing unit", "SELECT DATE_TRUNC(unit, CAST('" + tsCol + "' AS TIMESTAMP)) AS t FROM S3Object", nil},
	}
	for _, tc := range cases {
		out, ok, err := runSelect(t, tc.query, tc.in)
		if err != nil || !ok {
			t.Errorf("%s: unexpected ok=%v err=%v", tc.name, ok, err)
			continue
		}
		if got := kv(t, out, "t"); got != nil {
			t.Errorf("%s: got %v want NULL", tc.name, got)
		}
	}
}

func TestDateTruncErrors(t *testing.T) {
	evalErrorQueries := []struct {
		name  string
		query string
		in    jstream.KVS
	}{
		{"unknown string unit",
			"SELECT DATE_TRUNC('week', CAST('" + tsCol + "' AS TIMESTAMP)) AS t FROM S3Object", nil},
		{"non-timestamp argument",
			"SELECT DATE_TRUNC('day', 42) AS t FROM S3Object", nil},
		{"unparseable timestamp string",
			"SELECT DATE_TRUNC('day', 'not a timestamp') AS t FROM S3Object", nil},
		{"non-string unit",
			"SELECT DATE_TRUNC(42, CAST('" + tsCol + "' AS TIMESTAMP)) AS t FROM S3Object", nil},
		{"unknown unit from row column",
			"SELECT DATE_TRUNC(unit, ts) AS t FROM S3Object",
			jstream.KVS{{Key: "unit", Value: "week"}, {Key: "ts", Value: tsCol}}},
	}
	for _, tc := range evalErrorQueries {
		if _, _, err := runSelect(t, tc.query, tc.in); err == nil {
			t.Errorf("%s: expected an error, got success", tc.name)
		}
	}

	// Wrong argument count must fail while parsing.
	parseErrorQueries := []string{
		"SELECT DATE_TRUNC('day') AS t FROM S3Object",
		"SELECT DATE_TRUNC('day', ts, 1) AS t FROM S3Object",
		"SELECT DATE_TRUNC() AS t FROM S3Object",
	}
	for i, q := range parseErrorQueries {
		if _, err := sql.ParseSelectStatement(q); err == nil {
			t.Errorf("parse case %d: expected parse error for %q", i+1, q)
		}
	}
}

func TestExistingDateFunctionsUnchanged(t *testing.T) {
	out, ok, err := runSelect(t,
		"SELECT DATE_ADD(day, 2, CAST('2026-03-15T' AS TIMESTAMP)) AS t FROM S3Object", nil)
	if err != nil || !ok {
		t.Fatalf("DATE_ADD: ok=%v err=%v", ok, err)
	}
	if got := kv(t, out, "t"); got != "2026-03-17T" {
		t.Errorf("DATE_ADD: got %v want %q", got, "2026-03-17T")
	}

	out, ok, err = runSelect(t,
		"SELECT DATE_DIFF(day, CAST('2026-03-15T' AS TIMESTAMP), CAST('2026-03-17T' AS TIMESTAMP)) AS d FROM S3Object", nil)
	if err != nil || !ok {
		t.Fatalf("DATE_DIFF: ok=%v err=%v", ok, err)
	}
	if got := kv(t, out, "d"); got != float64(2) {
		t.Errorf("DATE_DIFF: got %v want 2", got)
	}

	out, ok, err = runSelect(t,
		"SELECT EXTRACT(month FROM CAST('2026-03-15T' AS TIMESTAMP)) AS m FROM S3Object", nil)
	if err != nil || !ok {
		t.Fatalf("EXTRACT: ok=%v err=%v", ok, err)
	}
	if got := kv(t, out, "m"); got != float64(3) {
		t.Errorf("EXTRACT: got %v want 3", got)
	}
}
