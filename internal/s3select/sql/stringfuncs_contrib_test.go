/*
 * MinIO Object Storage (c) 2021 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package sql

import "testing"

func TestEvalSQLSubstring(t *testing.T) {
	evalCases := []struct {
		s           string
		startIdx    int
		length      int
		resExpected string
		errExpected error
	}{
		{"abcd", 1, 1, "a", nil},
		{"abcd", -1, 1, "a", nil},
		{"abcd", 999, 999, "", nil},
		{"", 999, 999, "", nil},
		{"测试abc", 1, 1, "测", nil},
		{"测试abc", 5, 5, "c", nil},
	}

	for i, tc := range evalCases {
		res, err := evalSQLSubstring(tc.s, tc.startIdx, tc.length)
		if res != tc.resExpected || err != tc.errExpected {
			t.Errorf("Eval Case %d failed: %v %v", i, res, err)
		}
	}
}

func TestEvalSQLSplitPart(t *testing.T) {
	evalCases := []struct {
		text        string
		delimiter   string
		field       int
		resExpected string
		errExpected bool
	}{
		// Basic positive field numbers
		{"a,b,c", ",", 1, "a", false},
		{"a,b,c", ",", 2, "b", false},
		{"a,b,c", ",", 3, "c", false},

		// Negative field numbers count from the end
		{"a,b,c", ",", -1, "c", false},
		{"a,b,c", ",", -2, "b", false},
		{"a,b,c", ",", -3, "a", false},

		// Out of range returns empty string
		{"a,b,c", ",", 4, "", false},
		{"a,b,c", ",", -4, "", false},

		// Leading, trailing and consecutive delimiters keep empty fields
		{",a,", ",", 1, "", false},
		{",a,", ",", 2, "a", false},
		{",a,", ",", 3, "", false},
		{"a,,b", ",", 2, "", false},
		{"a,,b", ",", -2, "", false},

		// Empty text has a single empty field
		{"", ",", 1, "", false},
		{"", ",", -1, "", false},
		{"", ",", 2, "", false},

		// Empty delimiter: the whole text is the single field
		{"abc", "", 1, "abc", false},
		{"abc", "", -1, "abc", false},
		{"abc", "", 2, "", false},
		{"abc", "", -2, "", false},
		{"", "", 1, "", false},

		// Multi-character delimiter matched as a complete literal
		{"a::b::c", "::", 1, "a", false},
		{"a::b::c", "::", 2, "b", false},
		{"a::b::c", "::", -1, "c", false},
		{"aaa", "aa", 1, "", false}, // non-overlapping split: ["", "a"]
		{"aaa", "aa", 2, "a", false},

		// Unicode delimiter
		{"a界b界c", "界", 1, "a", false},
		{"a界b界c", "界", 2, "b", false},
		{"a界b界c", "界", -1, "c", false},

		// Field number 0 is an error
		{"a,b,c", ",", 0, "", true},
		{"", "", 0, "", true},
	}

	for i, tc := range evalCases {
		res, err := evalSQLSplitPart(tc.text, tc.delimiter, tc.field)
		if res != tc.resExpected || (err != nil) != tc.errExpected {
			t.Errorf("Eval Case %d failed: got %q err=%v, want %q err=%v", i, res, err, tc.resExpected, tc.errExpected)
		}
	}
}

func TestSplitPartValue(t *testing.T) {
	successCases := []struct {
		name     string
		text     *Value
		delim    *Value
		field    *Value
		expected string
	}{
		{
			name:     "constant-string-args",
			text:     FromString("a,b,c"),
			delim:    FromString(","),
			field:    FromInt(2),
			expected: "b",
		},
		{
			name:     "csv-untyped-bytes-args", // CSV fields are untyped byte slices
			text:     FromBytes([]byte("a,b,c")),
			delim:    FromBytes([]byte(",")),
			field:    FromBytes([]byte("2")),
			expected: "b",
		},
		{
			name:     "csv-untyped-negative-field",
			text:     FromBytes([]byte("a,b,c")),
			delim:    FromString(","),
			field:    FromBytes([]byte("-1")),
			expected: "c",
		},
		{
			name:     "empty-delimiter-whole-text",
			text:     FromString("a,b,c"),
			delim:    FromString(""),
			field:    FromInt(1),
			expected: "a,b,c",
		},
		{
			name:     "unicode-delimiter",
			text:     FromString("a界b界c"),
			delim:    FromString("界"),
			field:    FromInt(-1),
			expected: "c",
		},
		{
			name:     "out-of-range-empty",
			text:     FromString("a,b,c"),
			delim:    FromString(","),
			field:    FromInt(9),
			expected: "",
		},
	}

	for _, tc := range successCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := splitPart(tc.text, tc.delim, tc.field)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, ok := res.ToString()
			if !ok {
				t.Fatalf("expected string result, got type %s", res.GetTypeString())
			}
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}

	// NULL or MISSING arguments yield SQL NULL.
	nullCases := []struct {
		name  string
		text  *Value
		delim *Value
		field *Value
	}{
		{"null-text", FromNull(), FromString(","), FromInt(1)},
		{"null-delimiter", FromString("a,b"), FromNull(), FromInt(1)},
		{"null-field", FromString("a,b"), FromString(","), FromNull()},
		{"all-null", FromNull(), FromNull(), FromNull()},
		{"missing-text", FromMissing(), FromString(","), FromInt(1)},
		{"missing-delimiter", FromString("a,b"), FromMissing(), FromInt(1)},
		{"missing-field", FromString("a,b"), FromString(","), FromMissing()},
	}
	for _, tc := range nullCases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := splitPart(tc.text, tc.delim, tc.field)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !res.IsNull() {
				t.Errorf("expected NULL, got %s (%s)", res.String(), res.GetTypeString())
			}
		})
	}

	// Typed non-string text/delimiter (e.g. JSON numbers or booleans)
	// must not be silently converted to text; non-integer or zero field
	// numbers must error.
	errorCases := []struct {
		name  string
		text  *Value
		delim *Value
		field *Value
	}{
		{"int-text", FromInt(5), FromString(","), FromInt(1)},
		{"float-text", FromFloat(1.5), FromString(","), FromInt(1)},
		{"bool-text", FromBool(true), FromString(","), FromInt(1)},
		{"int-delimiter", FromString("a,b"), FromInt(1), FromInt(1)},
		{"bool-delimiter", FromString("a,b"), FromBool(false), FromInt(1)},
		{"string-field", FromString("a,b"), FromString(","), FromString("1")},
		{"bool-field", FromString("a,b"), FromString(","), FromBool(true)},
		{"float-field", FromString("a,b"), FromString(","), FromFloat(2.0)},
		{"fractional-field", FromString("a,b"), FromString(","), FromFloat(1.5)},
		{"zero-field", FromString("a,b"), FromString(","), FromInt(0)},
		{"csv-non-numeric-field", FromString("a,b"), FromString(","), FromBytes([]byte("abc"))},
		{"csv-fractional-field", FromString("a,b"), FromString(","), FromBytes([]byte("1.5"))},
	}
	for _, tc := range errorCases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := splitPart(tc.text, tc.delim, tc.field); err == nil {
				t.Errorf("expected an error, got nil")
			}
		})
	}
}

func TestSplitPartAnalysis(t *testing.T) {
	successQueries := []string{
		`SELECT SPLIT_PART(s.col, ',', 1) FROM s3object s`,
		`select split_part('a,b', ',', 1) from s3object`,
		`SELECT SPLIT_PART(s.col, '/', -1) FROM s3object s`,
		`SELECT * FROM s3object WHERE SPLIT_PART(col, ',', 1) = 'a'`,
		`SELECT SPLIT_PART(SPLIT_PART(col, ',', 1), '-', 1) FROM s3object`,
		`SELECT SPLIT_PART(col, '::', 2) FROM s3object`,
	}
	for i, q := range successQueries {
		if _, err := ParseSelectStatement(q); err != nil {
			t.Errorf("success case %d: unexpected error: %v: %s", i, err, q)
		}
	}

	errorQueries := []string{
		`SELECT SPLIT_PART('a,b', ',') FROM s3object`,
		`SELECT SPLIT_PART('a,b', ',', 1, 2) FROM s3object`,
		`SELECT SPLIT_PART('a,b') FROM s3object`,
		`SELECT SPLIT_PART() FROM s3object`,
		`SELECT * FROM s3object WHERE SPLIT_PART(col, ',') = 'a'`,
	}
	for i, q := range errorQueries {
		if _, err := ParseSelectStatement(q); err == nil {
			t.Errorf("error case %d: expected an error, got nil: %s", i, q)
		}
	}
}
