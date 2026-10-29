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

import (
	"reflect"
	"regexp"
	"testing"
)

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

func TestEvalSQLRegexpCount(t *testing.T) {
	evalCases := []struct {
		text, pattern string
		countExpected int64
		errExpected   bool
	}{
		// Basic non-overlapping matches
		{"banana", "an", 2, false},
		{"banana", "ana", 1, false},
		{"aaa", "a", 3, false},
		{"a1b2c3", `[0-9]`, 3, false},
		{"aAa", "a", 2, false},

		// No match returns 0
		{"abc", "z", 0, false},
		{"", "a", 0, false},

		// Empty pattern and patterns that can match the empty
		// string. The count must match FindAllStringIndex, which
		// advances by a rune after an empty match instead of looping.
		{"abc", "", 4, false},
		{"", "", 1, false},
		{"", "a*", 1, false},
		// Go does not report an empty match adjacent to the end of a
		// preceding non-empty match, so "a*" on "aaa" counts once.
		{"aaa", "a*", 1, false},
		{"bbb", "a*", 4, false},
		{"你好", "a*", 3, false},

		// UTF-8 multibyte text is handled correctly
		{"你好你好", "你好", 2, false},
		{"Café café", "é", 2, false},
		{"テストテスト", "テスト", 2, false},

		// Invalid regular expression returns an error, not 0
		{"abc", "(", 0, true},
		{"abc", "[a-", 0, true},
		{"abc", "*", 0, true},
	}

	for i, tc := range evalCases {
		res, err := evalSQLRegexpCount(tc.text, tc.pattern)
		if tc.errExpected {
			if err == nil {
				t.Errorf("Case %d: expected an error for pattern %q, got count %d", i, tc.pattern, res)
			}
			continue
		}
		if err != nil {
			t.Errorf("Case %d: unexpected error: %v", i, err)
			continue
		}
		if res != tc.countExpected {
			t.Errorf("Case %d: expected %d, got %d (text=%q pattern=%q)", i, tc.countExpected, res, tc.text, tc.pattern)
		}
	}
}

// TestEvalSQLRegexpCountMatchesFindAllStringIndex is a differential test
// ensuring the function agrees with regexp.FindAllStringIndex for a set
// of patterns, including zero-length matches and UTF-8 text.
func TestEvalSQLRegexpCountMatchesFindAllStringIndex(t *testing.T) {
	texts := []string{
		"",
		"abc",
		"banana",
		"aaa",
		"你好，世界",
		"a你b好a你",
		"line1\nline2\n",
	}
	patterns := []string{
		"",
		"a",
		"a*",
		"an",
		"[a-z]+",
		".",
		`\b`,
		"你",
		"a?",
		"x",
	}

	for _, text := range texts {
		for _, pattern := range patterns {
			re, err := regexp.Compile(pattern)
			if err != nil {
				t.Fatalf("invalid reference pattern %q: %v", pattern, err)
			}
			expected := int64(len(re.FindAllStringIndex(text, -1)))
			got, err := evalSQLRegexpCount(text, pattern)
			if err != nil {
				t.Errorf("unexpected error for text=%q pattern=%q: %v", text, pattern, err)
				continue
			}
			if got != expected {
				t.Errorf("text=%q pattern=%q: expected %d, got %d", text, pattern, expected, got)
			}
		}
	}
}

func TestRegexpCountValue(t *testing.T) {
	cases := []struct {
		name        string
		text        *Value
		pattern     *Value
		expected    *Value
		errExpected bool
	}{
		{
			name:     "basic",
			text:     FromString("banana"),
			pattern:  FromString("an"),
			expected: FromInt(2),
		},
		{
			name:     "no-match-returns-zero",
			text:     FromString("abc"),
			pattern:  FromString("z"),
			expected: FromInt(0),
		},
		{
			name:     "greedy-match",
			text:     FromString("aaa"),
			pattern:  FromString("a*"),
			expected: FromInt(1),
		},
		{
			name:     "zero-length-matches",
			text:     FromString("bbb"),
			pattern:  FromString("a*"),
			expected: FromInt(4),
		},
		{
			name:     "utf8",
			text:     FromString("你好你好"),
			pattern:  FromString("你好"),
			expected: FromInt(2),
		},
		{
			name:     "null-text",
			text:     FromNull(),
			pattern:  FromString("a"),
			expected: FromNull(),
		},
		{
			name:     "null-pattern",
			text:     FromString("abc"),
			pattern:  FromNull(),
			expected: FromNull(),
		},
		{
			name:     "missing-text",
			text:     FromMissing(),
			pattern:  FromString("a"),
			expected: FromNull(),
		},
		{
			name:     "missing-pattern",
			text:     FromString("abc"),
			pattern:  FromMissing(),
			expected: FromNull(),
		},
		{
			name:     "bytes-text-is-string",
			text:     FromBytes([]byte("a1a")),
			pattern:  FromString(`\d`),
			expected: FromInt(1),
		},
		{
			name:        "non-string-text",
			text:        FromInt(1),
			pattern:     FromString("a"),
			errExpected: true,
		},
		{
			name:        "non-string-pattern",
			text:        FromString("abc"),
			pattern:     FromBool(true),
			errExpected: true,
		},
		{
			name:        "invalid-pattern",
			text:        FromString("a(b"),
			pattern:     FromString("("),
			errExpected: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := regexpCount(tc.text, tc.pattern)
			if tc.errExpected {
				if err == nil {
					t.Fatalf("expected a query error, got value %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}

func TestRegexpCountAnalysis(t *testing.T) {
	validQueries := []string{
		`SELECT REGEXP_COUNT(title, 'e') FROM S3OBJECT`,
		`SELECT regexp_count(s.title, s.pattern) FROM S3OBJECT s`,
		`SELECT REGEXP_COUNT(LOWER(s.title), 'e+') FROM S3OBJECT s`,
		`SELECT id FROM S3OBJECT WHERE REGEXP_COUNT(title, 'e') > 0`,
		`SELECT REGEXP_COUNT('const', 'o') FROM S3OBJECT`,
	}
	for i, q := range validQueries {
		if _, err := ParseSelectStatement(q); err != nil {
			t.Errorf("case %d (%q): unexpected error: %v", i, q, err)
		}
	}

	invalidQueries := []string{
		`SELECT REGEXP_COUNT(title) FROM S3OBJECT`,
		`SELECT REGEXP_COUNT(title, 'e', 1) FROM S3OBJECT`,
		`SELECT REGEXP_COUNT() FROM S3OBJECT`,
	}
	for i, q := range invalidQueries {
		if _, err := ParseSelectStatement(q); err == nil {
			t.Errorf("case %d (%q): expected a query error for wrong argument count", i, q)
		}
	}
}
