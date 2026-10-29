// Copyright (c) 2015-2021 MinIO, Inc.
//
// This file is part of MinIO Object Storage stack
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License v3 as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
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
	"io"
	"testing"
	"time"
)

// memRecord is a minimal in-memory Record used to evaluate SQL
// expressions directly against typed and untyped values.
type memRecord struct {
	cols map[string]*Value
}

func newMemRecord() *memRecord {
	return &memRecord{cols: map[string]*Value{}}
}

func (r *memRecord) Get(name string) (*Value, error) {
	if v, ok := r.cols[name]; ok {
		return v, nil
	}
	// Unknown columns behave like a missing JSON field.
	return FromMissing(), nil
}

func (r *memRecord) Set(name string, value *Value) (Record, error) {
	r.cols[name] = value
	return r, nil
}

func (r *memRecord) WriteCSV(io.Writer, WriteCSVOpts) error { return nil }
func (r *memRecord) WriteJSON(io.Writer) error              { return nil }

func (r *memRecord) Clone(dst Record) Record {
	if c, ok := dst.(*memRecord); ok {
		return c
	}
	return newMemRecord()
}

func (r *memRecord) Reset()                         { r.cols = map[string]*Value{} }
func (r *memRecord) Raw() (SelectObjectFormat, any) { return SelectFmtUnknown, nil }
func (r *memRecord) Replace(any) error              { return nil }

// evalOne parses and analyzes query, evaluates it against input and
// returns the value of the first output column. A second return value
// reports whether the row passed the WHERE clause.
func evalOne(t *testing.T, query string, input Record) (*Value, bool) {
	t.Helper()
	stmt, err := ParseSelectStatement(query)
	if err != nil {
		t.Fatalf("failed to parse %q: %v", query, err)
	}
	out := newMemRecord()
	rec, err := stmt.Eval(input, out)
	if err != nil {
		t.Fatalf("failed to evaluate %q: %v", query, err)
	}
	if rec == nil {
		return nil, false
	}
	outRec := rec.(*memRecord)
	for _, v := range outRec.cols {
		return v, true
	}
	return nil, true
}

// evalErr parses and evaluates query, returning any error from parsing,
// analysis or row evaluation.
func evalErr(t *testing.T, query string, input Record) error {
	t.Helper()
	stmt, err := ParseSelectStatement(query)
	if err != nil {
		return err
	}
	_, err = stmt.Eval(input, newMemRecord())
	return err
}

func mustTimestamp(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := parseSQLTimestamp(s)
	if err != nil {
		t.Fatalf("invalid test timestamp %q: %v", s, err)
	}
	return ts
}

// TestTryCastConversions covers constants, typed column values and
// untyped (CSV-like byte slice) column values. Successful conversions
// must follow the same rules as CAST; values that cannot convert must
// yield SQL NULL without an error.
func TestTryCastConversions(t *testing.T) {
	t.Parallel()
	ts := mustTimestamp(t, "2020-01-02T15:04:05Z")

	tests := []struct {
		name  string
		query string
		col   string
		val   *Value
		want  *Value
	}{
		// Constants - successful conversions
		{name: "const-string-to-int", query: `SELECT TRY_CAST('123' AS INT) AS r FROM s3object s`, want: FromInt(123)},
		{name: "const-string-to-integer", query: `SELECT TRY_CAST('123' AS INTEGER) AS r FROM s3object s`, want: FromInt(123)},
		{name: "const-float-string-to-int-truncates", query: `SELECT TRY_CAST('3.9' AS INT) AS r FROM s3object s`, want: FromInt(3)},
		{name: "const-float-to-int-truncates", query: `SELECT TRY_CAST(3.9 AS INT) AS r FROM s3object s`, want: FromInt(3)},
		{name: "const-int-to-int", query: `SELECT TRY_CAST(42 AS INT) AS r FROM s3object s`, want: FromInt(42)},
		{name: "const-string-to-float", query: `SELECT TRY_CAST('1.5' AS FLOAT) AS r FROM s3object s`, want: FromFloat(1.5)},
		{name: "const-int-to-float", query: `SELECT TRY_CAST(2 AS FLOAT) AS r FROM s3object s`, want: FromFloat(2)},
		{name: "const-string-to-bool-true", query: `SELECT TRY_CAST('true' AS BOOL) AS r FROM s3object s`, want: FromBool(true)},
		{name: "const-string-to-bool-false", query: `SELECT TRY_CAST('FALSE' AS BOOL) AS r FROM s3object s`, want: FromBool(false)},
		{name: "const-bool-to-bool", query: `SELECT TRY_CAST(TRUE AS BOOL) AS r FROM s3object s`, want: FromBool(true)},
		{name: "const-int-to-string", query: `SELECT TRY_CAST(42 AS STRING) AS r FROM s3object s`, want: FromString("42")},
		{name: "const-float-to-string", query: `SELECT TRY_CAST(1.5 AS STRING) AS r FROM s3object s`, want: FromString("1.5")},
		{name: "const-bool-to-string", query: `SELECT TRY_CAST(TRUE AS STRING) AS r FROM s3object s`, want: FromString("true")},
		{name: "const-string-to-timestamp", query: `SELECT TRY_CAST('2020-01-02T15:04:05Z' AS TIMESTAMP) AS r FROM s3object s`, want: FromTimestamp(ts)},

		// Constants - values that cannot convert become NULL
		{name: "const-bad-string-to-int", query: `SELECT TRY_CAST('abc' AS INT) AS r FROM s3object s`, want: FromNull()},
		{name: "const-bad-string-to-float", query: `SELECT TRY_CAST('abc' AS FLOAT) AS r FROM s3object s`, want: FromNull()},
		{name: "const-int-string-to-bool", query: `SELECT TRY_CAST('1' AS BOOL) AS r FROM s3object s`, want: FromNull()},
		{name: "const-garbage-to-bool", query: `SELECT TRY_CAST('maybe' AS BOOL) AS r FROM s3object s`, want: FromNull()},
		{name: "const-bad-string-to-timestamp", query: `SELECT TRY_CAST('not-a-timestamp' AS TIMESTAMP) AS r FROM s3object s`, want: FromNull()},
		{name: "const-bool-to-int", query: `SELECT TRY_CAST(TRUE AS INT) AS r FROM s3object s`, want: FromNull()},
		{name: "const-int-to-bool", query: `SELECT TRY_CAST(1 AS BOOL) AS r FROM s3object s`, want: FromNull()},

		// NULL and MISSING input always becomes NULL
		{name: "null-to-int", query: `SELECT TRY_CAST(NULL AS INT) AS r FROM s3object s`, want: FromNull()},
		{name: "null-to-float", query: `SELECT TRY_CAST(NULL AS FLOAT) AS r FROM s3object s`, want: FromNull()},
		{name: "null-to-bool", query: `SELECT TRY_CAST(NULL AS BOOL) AS r FROM s3object s`, want: FromNull()},
		{name: "null-to-string", query: `SELECT TRY_CAST(NULL AS STRING) AS r FROM s3object s`, want: FromNull()},
		{name: "null-to-timestamp", query: `SELECT TRY_CAST(NULL AS TIMESTAMP) AS r FROM s3object s`, want: FromNull()},
		{name: "missing-to-int", query: `SELECT TRY_CAST(MISSING AS INT) AS r FROM s3object s`, want: FromNull()},

		// Typed column values (JSON-like scalars)
		{name: "col-int-string", query: `SELECT TRY_CAST(s.v AS INT) AS r FROM s3object s`, col: "v", val: FromString("42"), want: FromInt(42)},
		{name: "col-int-bad", query: `SELECT TRY_CAST(s.v AS INT) AS r FROM s3object s`, col: "v", val: FromString("oops"), want: FromNull()},
		{name: "col-float", query: `SELECT TRY_CAST(s.v AS FLOAT) AS r FROM s3object s`, col: "v", val: FromFloat(2.5), want: FromFloat(2.5)},
		{name: "col-bool", query: `SELECT TRY_CAST(s.v AS BOOL) AS r FROM s3object s`, col: "v", val: FromBool(true), want: FromBool(true)},
		{name: "col-timestamp", query: `SELECT TRY_CAST(s.v AS TIMESTAMP) AS r FROM s3object s`, col: "v", val: FromTimestamp(ts), want: FromTimestamp(ts)},
		{name: "col-null", query: `SELECT TRY_CAST(s.v AS INT) AS r FROM s3object s`, col: "v", val: FromNull(), want: FromNull()},
		{name: "col-missing", query: `SELECT TRY_CAST(s.absent AS INT) AS r FROM s3object s`, want: FromNull()},
		{name: "col-array-to-int", query: `SELECT TRY_CAST(s.v AS INT) AS r FROM s3object s`, col: "v", val: FromArray([]Value{*FromInt(1)}), want: FromNull()},

		// Untyped byte-slice column values, as read from CSV fields
		{name: "csv-bytes-to-int", query: `SELECT TRY_CAST(s.v AS INT) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("42")), want: FromInt(42)},
		{name: "csv-bytes-to-int-trimmed", query: `SELECT TRY_CAST(s.v AS INT) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("  42 ")), want: FromInt(42)},
		{name: "csv-bytes-to-int-bad", query: `SELECT TRY_CAST(s.v AS INT) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("n/a")), want: FromNull()},
		{name: "csv-empty-bytes-to-int", query: `SELECT TRY_CAST(s.v AS INT) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("")), want: FromNull()},
		{name: "csv-bytes-to-float", query: `SELECT TRY_CAST(s.v AS FLOAT) AS r FROM s3object s`, col: "v", val: FromBytes([]byte(" 0.5 ")), want: FromFloat(0.5)},
		{name: "csv-bytes-to-float-bad", query: `SELECT TRY_CAST(s.v AS FLOAT) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("--")), want: FromNull()},
		{name: "csv-bytes-to-bool", query: `SELECT TRY_CAST(s.v AS BOOL) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("False")), want: FromBool(false)},
		{name: "csv-bytes-to-bool-bad", query: `SELECT TRY_CAST(s.v AS BOOL) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("1")), want: FromNull()},
		{name: "csv-bytes-to-string", query: `SELECT TRY_CAST(s.v AS STRING) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("abc")), want: FromString("abc")},
		{name: "csv-bytes-to-timestamp", query: `SELECT TRY_CAST(s.v AS TIMESTAMP) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("2020-01-02T15:04:05Z")), want: FromTimestamp(ts)},
		{name: "csv-bytes-to-timestamp-bad", query: `SELECT TRY_CAST(s.v AS TIMESTAMP) AS r FROM s3object s`, col: "v", val: FromBytes([]byte("not-a-timestamp")), want: FromNull()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := newMemRecord()
			if tt.val != nil {
				input.cols[tt.col] = tt.val
			}
			got, ok := evalOne(t, tt.query, input)
			if !ok {
				t.Fatalf("row did not pass WHERE clause")
			}
			if !got.SameTypeAs(*tt.want) {
				t.Fatalf("type mismatch: got %s, want %s (value %v)", got.GetTypeString(), tt.want.GetTypeString(), got)
			}
			if !got.Equals(*tt.want) {
				t.Fatalf("value mismatch: got %v (%s), want %v (%s)", got, got.GetTypeString(), tt.want, tt.want.GetTypeString())
			}
		})
	}
}

// TestTryCastCaseInsensitive ensures keyword casing follows the rest of
// the SQL dialect.
func TestTryCastCaseInsensitive(t *testing.T) {
	t.Parallel()
	input := newMemRecord()
	input.cols["v"] = FromBytes([]byte("7"))
	got, ok := evalOne(t, `select try_cast(s.v as integer) as r from s3object s`, input)
	if !ok || !got.Equals(*FromInt(7)) {
		t.Fatalf("got %v, ok %v", got, ok)
	}
}

// TestTryCastExpressionErrors verifies that only value conversion
// failures are tolerated: errors raised while evaluating the input
// expression itself must still be reported.
func TestTryCastExpressionErrors(t *testing.T) {
	t.Parallel()

	constErr := evalErr(t, `SELECT TRY_CAST(1/0 AS INT) AS r FROM s3object s`, newMemRecord())
	if constErr == nil {
		t.Fatalf("expected division by zero in input expression to error, got nil")
	}

	input := newMemRecord()
	input.cols["v"] = FromInt(3)
	colErr := evalErr(t, `SELECT TRY_CAST(s.v/0 AS INT) AS r FROM s3object s`, input)
	if colErr == nil {
		t.Fatalf("expected division by zero on column to error, got nil")
	}
}

// TestTryCastUnknownTargetType verifies unsupported target types and
// malformed syntax are rejected during parsing/analysis, rather than
// being converted to NULL.
func TestTryCastUnknownTargetType(t *testing.T) {
	t.Parallel()

	// DECIMAL is accepted by CAST but not implemented, and it is not a
	// supported TRY_CAST target type, so this is a syntax error.
	if err := evalErr(t, `SELECT TRY_CAST('1' AS DECIMAL) AS r FROM s3object s`, newMemRecord()); err == nil {
		t.Fatalf("expected TRY_CAST to DECIMAL to be a parse error")
	}

	// Bogus type is a syntax error.
	if err := evalErr(t, `SELECT TRY_CAST('1' AS BLOB) AS r FROM s3object s`, newMemRecord()); err == nil {
		t.Fatalf("expected TRY_CAST to unknown type to be a parse error")
	}

	// Malformed syntax.
	if err := evalErr(t, `SELECT TRY_CAST('1' AS INT FROM s3object s`, newMemRecord()); err == nil {
		t.Fatalf("expected malformed TRY_CAST to be a parse error")
	}
}

// TestTryCastWhereAndAggregation checks that TRY_CAST can be used in
// WHERE clauses and as an argument of existing aggregation functions,
// following the regular aggregation analysis rules.
func TestTryCastWhereAndAggregation(t *testing.T) {
	t.Parallel()

	// Mixed good/bad rows, driven through WHERE.
	rows := []map[string]*Value{
		{"id": FromInt(1), "num": FromBytes([]byte("10")), "flag": FromBytes([]byte("true"))},
		{"id": FromInt(2), "num": FromBytes([]byte("bad")), "flag": FromBytes([]byte("maybe"))},
		{"id": FromInt(3), "num": FromBytes([]byte("-2")), "flag": FromBytes([]byte("false"))},
		{"id": FromInt(4), "num": FromNull(), "flag": FromNull()},
	}
	runWhere := func(query string) []int64 {
		t.Helper()
		var got []int64
		for _, row := range rows {
			in := newMemRecord()
			in.cols = row
			v, pass := evalOne(t, query, in)
			if !pass {
				continue
			}
			id, ok := v.ToInt()
			if !ok {
				t.Fatalf("expected int id, got %s", v.GetTypeString())
			}
			got = append(got, id)
		}
		return got
	}

	if got := runWhere(`SELECT id FROM s3object s WHERE TRY_CAST(num AS INT) IS NOT NULL`); len(got) != 2 || got[0] != 1 || got[1] != 3 {
		t.Fatalf("IS NOT NULL filter got %v, want [1 3]", got)
	}
	if got := runWhere(`SELECT id FROM s3object s WHERE TRY_CAST(num AS INT) IS NULL`); len(got) != 2 || got[0] != 2 || got[1] != 4 {
		t.Fatalf("IS NULL filter got %v, want [2 4]", got)
	}
	if got := runWhere(`SELECT id FROM s3object s WHERE TRY_CAST(flag AS BOOL) = TRUE`); len(got) != 1 || got[0] != 1 {
		t.Fatalf("BOOL equality filter got %v, want [1]", got)
	}
	if got := runWhere(`SELECT id FROM s3object s WHERE TRY_CAST(num AS INT) = -2`); len(got) != 1 || got[0] != 3 {
		t.Fatalf("INT equality filter got %v, want [3]", got)
	}

	// Aggregation: NULL conversion results must simply be skipped.
	aggStmt, err := ParseSelectStatement(`SELECT
		SUM(TRY_CAST(num AS INT)) AS s,
		COUNT(TRY_CAST(num AS INT)) AS c,
		AVG(TRY_CAST(num AS FLOAT)) AS a,
		MIN(TRY_CAST(num AS INT)) AS mn,
		MAX(TRY_CAST(num AS INT)) AS mx
		FROM s3object s`)
	if err != nil {
		t.Fatalf("aggregate query failed to parse: %v", err)
	}
	if !aggStmt.IsAggregated() {
		t.Fatalf("expected query to be analyzed as an aggregation")
	}
	for _, row := range rows {
		in := newMemRecord()
		in.cols = row
		if err := aggStmt.AggregateRow(in); err != nil {
			t.Fatalf("AggregateRow error: %v", err)
		}
	}
	out := newMemRecord()
	if err := aggStmt.AggregateResult(out); err != nil {
		t.Fatalf("AggregateResult error: %v", err)
	}
	if v := out.cols["s"]; !v.Equals(*FromFloat(8)) { // 10 + -2
		t.Fatalf("SUM got %v, want 8", v)
	}
	if v := out.cols["c"]; !v.Equals(*FromInt(2)) {
		t.Fatalf("COUNT got %v, want 2", v)
	}
	if v := out.cols["a"]; !v.Equals(*FromFloat(4)) { // (10 + -2) / 2
		t.Fatalf("AVG got %v, want 4", v)
	}
	if v := out.cols["mn"]; !v.Equals(*FromInt(-2)) {
		t.Fatalf("MIN got %v, want -2", v)
	}
	if v := out.cols["mx"]; !v.Equals(*FromInt(10)) {
		t.Fatalf("MAX got %v, want 10", v)
	}

	// Nesting an aggregation inside a row-level expression stays
	// invalid, just as it is for CAST.
	if err := evalErr(t, `SELECT TRY_CAST(num AS INT) + SUM(num) FROM s3object s`, newMemRecord()); err == nil {
		t.Fatalf("expected mixing row func with aggregation to be an analysis error")
	}
	// Nested aggregations remain invalid.
	if err := evalErr(t, `SELECT SUM(TRY_CAST(SUM(num) AS INT)) FROM s3object s`, newMemRecord()); err == nil {
		t.Fatalf("expected nested aggregation to be an analysis error")
	}
}

// TestCastFailureBehaviorUnchanged ensures regular CAST keeps failing
// on bad values, and its historic NULL handling is untouched.
func TestCastFailureBehaviorUnchanged(t *testing.T) {
	t.Parallel()

	if err := evalErr(t, `SELECT CAST('abc' AS INT) AS r FROM s3object s`, newMemRecord()); err == nil {
		t.Fatalf("expected CAST of bad value to keep returning an error")
	}
	if err := evalErr(t, `SELECT CAST(TRUE AS INT) AS r FROM s3object s`, newMemRecord()); err == nil {
		t.Fatalf("expected unsupported CAST to keep returning an error")
	}

	// CAST(NULL AS STRING) historically returns the string "NULL";
	// TRY_CAST(NULL AS STRING) returns SQL NULL. Document/guard both.
	castNull, ok := evalOne(t, `SELECT CAST(NULL AS STRING) AS r FROM s3object s`, newMemRecord())
	if !ok || !castNull.Equals(*FromString("NULL")) {
		t.Fatalf("CAST(NULL AS STRING) got %v, want \"NULL\"", castNull)
	}
	tryNull, ok := evalOne(t, `SELECT TRY_CAST(NULL AS STRING) AS r FROM s3object s`, newMemRecord())
	if !ok || !tryNull.IsNull() {
		t.Fatalf("TRY_CAST(NULL AS STRING) got %v, want NULL", tryNull)
	}
}
