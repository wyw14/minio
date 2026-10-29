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

package s3select

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/klauspost/cpuid/v2"
	"github.com/minio/minio-go/v7"
	"github.com/minio/simdjson-go"
)

// tryCastCase is one end-to-end TRY_CAST query expectation. Exactly
// one of want/wantOpenErr/wantReadErr applies.
type tryCastCase struct {
	name        string
	query       string
	input       string // overrides the default input when non-empty
	want        string
	wantOpenErr bool // parse/analysis/request error
	wantReadErr bool // error event while processing records
}

const tryCastJSONRequest = `<?xml version="1.0" encoding="UTF-8"?>
<SelectObjectContentRequest>
    <Expression>%s</Expression>
    <ExpressionType>SQL</ExpressionType>
    <InputSerialization>
        <CompressionType>NONE</CompressionType>
        <JSON>
            <Type>LINES</Type>
        </JSON>
    </InputSerialization>
    <OutputSerialization>
        <JSON>
        </JSON>
    </OutputSerialization>
    <RequestProgress>
        <Enabled>FALSE</Enabled>
    </RequestProgress>
</SelectObjectContentRequest>`

const tryCastCSVRequest = `<?xml version="1.0" encoding="UTF-8"?>
<SelectObjectContentRequest>
    <Expression>%s</Expression>
    <ExpressionType>SQL</ExpressionType>
    <InputSerialization>
        <CompressionType>NONE</CompressionType>
        <CSV>
            <FileHeaderInfo>USE</FileHeaderInfo>
            <QuoteCharacter>"</QuoteCharacter>
        </CSV>
    </InputSerialization>
    <OutputSerialization>
        <JSON>
        </JSON>
    </OutputSerialization>
    <RequestProgress>
        <Enabled>FALSE</Enabled>
    </RequestProgress>
</SelectObjectContentRequest>`

// execTryCastQuery runs a single S3 Select request end to end and
// returns the trimmed payload plus any open-time or record-processing
// error.
func execTryCastQuery(t *testing.T, requestXMLTemplate, query, input string) (result string, openErr, readErr error) {
	t.Helper()
	var escaped bytes.Buffer
	xml.EscapeText(&escaped, []byte(query))
	req := fmt.Appendf(nil, requestXMLTemplate, escaped.String())

	s3Select, err := NewS3Select(bytes.NewReader(req))
	if err != nil {
		return "", err, nil
	}
	if err = s3Select.Open(newStringRSC(input)); err != nil {
		return "", err, nil
	}

	w := &testResponseWriter{}
	s3Select.Evaluate(w)
	s3Select.Close()

	resp := http.Response{
		StatusCode:    http.StatusOK,
		Body:          io.NopCloser(bytes.NewReader(w.response)),
		ContentLength: int64(len(w.response)),
	}
	res, err := minio.NewSelectResults(&resp, "testbucket")
	if err != nil {
		return "", nil, err
	}
	got, err := io.ReadAll(res)
	return strings.TrimSpace(string(got)), nil, err
}

func assertTryCastResult(t *testing.T, tc tryCastCase, engine, got string, openErr, readErr error) {
	t.Helper()
	switch {
	case tc.wantOpenErr:
		if openErr == nil {
			t.Errorf("[%s] expected open error, got result %q (readErr %v)", engine, got, readErr)
		}
	case tc.wantReadErr:
		if openErr != nil {
			t.Errorf("[%s] expected record processing error, query failed to open: %v", engine, openErr)
		} else if readErr == nil {
			t.Errorf("[%s] expected record processing error, got result %q", engine, got)
		}
	default:
		if openErr != nil {
			t.Errorf("[%s] unexpected open error: %v", engine, openErr)
		} else if readErr != nil {
			t.Errorf("[%s] unexpected record processing error: %v", engine, readErr)
		} else if got != tc.want {
			t.Errorf("[%s] result mismatch\nquery: %s\ngot:  %q\nwant: %q", engine, tc.query, got, tc.want)
		}
	}
}

// TestTryCastJSON covers constants, typed JSON scalars, NULL/missing
// fields, WHERE usage, aggregation arguments and a stream of
// alternating good/bad records. Each case runs through both JSON
// reader implementations.
func TestTryCastJSON(t *testing.T) {
	const input = `{"id":1,"num":"123","flt":"1.5","flag":"true","ts":"2020-01-02T15:04:05Z"}
{"id":2,"num":"oops","flt":"bad","flag":"maybe","ts":"not-a-timestamp"}
{"id":3,"num":"-7","flt":"2.5","flag":"false","ts":"2021-06-01T00:00:05Z"}
`

	cases := []tryCastCase{
		// Projection over consecutive good/bad/good records must keep
		// going and emit NULL for the dirty middle record.
		{
			name:  "projection-mixed-records",
			query: `SELECT id, TRY_CAST(num AS INT) AS i, TRY_CAST(flt AS FLOAT) AS f, TRY_CAST(flag AS BOOL) AS b, TRY_CAST(ts AS TIMESTAMP) AS t FROM s3object s`,
			want: `{"id":1,"i":123,"f":1.5,"b":true,"t":"2020-01-02T15:04:05Z"}
{"id":2,"i":null,"f":null,"b":null,"t":null}
{"id":3,"i":-7,"f":2.5,"b":false,"t":"2021-06-01T00:00:05Z"}`,
		},
		// INTEGER is an alias of INT and keywords are case-insensitive.
		{
			name:  "integer-alias-lowercase",
			query: `select id, try_cast(num as integer) as i from s3object s`,
			want: `{"id":1,"i":123}
{"id":2,"i":null}
{"id":3,"i":-7}`,
		},
		// Missing field in every record behaves like NULL.
		{
			name:  "missing-field",
			query: `SELECT id, TRY_CAST(s.absent AS INT) AS x FROM s3object s`,
			want: `{"id":1,"x":null}
{"id":2,"x":null}
{"id":3,"x":null}`,
		},
		// Explicit JSON null scalar.
		{
			name: "explicit-null-scalar",
			input: `{"v":null}
`,
			query: `SELECT TRY_CAST(v AS INT) AS x FROM s3object s`,
			want:  `{"x":null}`,
		},

		// Constants - successful conversions.
		{name: "const-int-ok", query: `SELECT TRY_CAST('77' AS INT) AS r FROM s3object s WHERE id = 1`, want: `{"r":77}`},
		{name: "const-float-ok", query: `SELECT TRY_CAST('1.25' AS FLOAT) AS r FROM s3object s WHERE id = 1`, want: `{"r":1.25}`},
		{name: "const-bool-ok", query: `SELECT TRY_CAST('true' AS BOOL) AS r FROM s3object s WHERE id = 1`, want: `{"r":true}`},
		{name: "const-string-ok", query: `SELECT TRY_CAST(123 AS STRING) AS r FROM s3object s WHERE id = 1`, want: `{"r":"123"}`},
		{name: "const-timestamp-ok", query: `SELECT TRY_CAST('2020-01-02T15:04:05Z' AS TIMESTAMP) AS r FROM s3object s WHERE id = 1`, want: `{"r":"2020-01-02T15:04:05Z"}`},
		// Constants - bad values become NULL.
		{name: "const-int-bad", query: `SELECT TRY_CAST('zz' AS INT) AS r FROM s3object s WHERE id = 1`, want: `{"r":null}`},
		{name: "const-float-bad", query: `SELECT TRY_CAST('x' AS FLOAT) AS r FROM s3object s WHERE id = 1`, want: `{"r":null}`},
		{name: "const-bool-bad", query: `SELECT TRY_CAST('y' AS BOOL) AS r FROM s3object s WHERE id = 1`, want: `{"r":null}`},
		{name: "const-timestamp-bad", query: `SELECT TRY_CAST('bad' AS TIMESTAMP) AS r FROM s3object s WHERE id = 1`, want: `{"r":null}`},
		{name: "const-null", query: `SELECT TRY_CAST(NULL AS INT) AS r FROM s3object s WHERE id = 1`, want: `{"r":null}`},

		// WHERE filtering keeps working across good and bad rows.
		{
			name:  "where-int-is-not-null",
			query: `SELECT id FROM s3object s WHERE TRY_CAST(num AS INT) IS NOT NULL`,
			want: `{"id":1}
{"id":3}`,
		},
		{
			name:  "where-int-is-null",
			query: `SELECT id FROM s3object s WHERE TRY_CAST(num AS INT) IS NULL`,
			want:  `{"id":2}`,
		},
		{
			name:  "where-bool-eq-true",
			query: `SELECT id FROM s3object s WHERE TRY_CAST(flag AS BOOL) = TRUE`,
			want:  `{"id":1}`,
		},
		{
			name:  "where-int-eq-value",
			query: `SELECT id FROM s3object s WHERE TRY_CAST(num AS INT) = -7`,
			want:  `{"id":3}`,
		},
		{
			name:  "where-missing-is-null",
			query: `SELECT id FROM s3object s WHERE TRY_CAST(absent AS INT) IS NULL`,
			want: `{"id":1}
{"id":2}
{"id":3}`,
		},

		// Aggregation over mixed records: bad values become NULL and are
		// skipped by the aggregate functions, as regular NULLs are.
		{
			name:  "aggregate-args",
			query: `SELECT SUM(TRY_CAST(num AS INT)) AS s, COUNT(TRY_CAST(num AS INT)) AS c, AVG(TRY_CAST(flt AS FLOAT)) AS a, MIN(TRY_CAST(flt AS FLOAT)) AS mn, MAX(TRY_CAST(flt AS FLOAT)) AS mx FROM s3object s`,
			want:  `{"s":116,"c":2,"a":2,"mn":1.5,"mx":2.5}`,
		},

		// Errors that must still be reported.
		{name: "input-expr-divide-by-zero", query: `SELECT TRY_CAST(1/0 AS INT) AS r FROM s3object s`, wantReadErr: true},
		{name: "cast-still-fails-on-bad-value", query: `SELECT CAST(num AS INT) AS i FROM s3object s`, wantReadErr: true},
		{name: "cast-const-still-fails", query: `SELECT CAST('oops' AS INT) AS r FROM s3object s`, wantReadErr: true},
		{name: "unknown-target-type", query: `SELECT TRY_CAST('1' AS DECIMAL) AS r FROM s3object s`, wantOpenErr: true},
		{name: "bogus-target-type", query: `SELECT TRY_CAST('1' AS BLOB) AS r FROM s3object s`, wantOpenErr: true},
		{name: "malformed-syntax", query: `SELECT TRY_CAST('1' AS INT FROM s3object s`, wantOpenErr: true},
	}

	for _, tc := range cases {
		in := input
		if tc.input != "" {
			in = tc.input
		}
		t.Run(tc.name, func(t *testing.T) {
			// Non-SIMD reader: disable AVX2 for the run when needed.
			if cpuid.CPU.Supports(cpuid.AVX2) {
				cpuid.CPU.Disable(cpuid.AVX2)
				got, openErr, readErr := execTryCastQuery(t, tryCastJSONRequest, tc.query, in)
				cpuid.CPU.Enable(cpuid.AVX2)
				assertTryCastResult(t, tc, "json", got, openErr, readErr)
			} else {
				got, openErr, readErr := execTryCastQuery(t, tryCastJSONRequest, tc.query, in)
				assertTryCastResult(t, tc, "json", got, openErr, readErr)
			}

			// SIMD reader, on CPUs that support it.
			if simdjson.SupportedCPU() {
				got, openErr, readErr := execTryCastQuery(t, tryCastJSONRequest, tc.query, in)
				assertTryCastResult(t, tc, "simdjson", got, openErr, readErr)
			}
		})
	}
}

// TestTryCastCSV covers untyped CSV fields (byte slices), empty
// fields, whitespace and out-of-range positional columns.
func TestTryCastCSV(t *testing.T) {
	const input = `id,num,flt,flag,ts
1,123,1.5,true,2020-01-02T15:04:05Z
2,oops,,maybe,not-a-timestamp
3,-7,2.5,false,2021-06-01T00:00:05Z
`

	cases := []tryCastCase{
		{
			name:  "projection-mixed-records",
			query: `SELECT TRY_CAST(num AS INT) AS i, TRY_CAST(flt AS FLOAT) AS f, TRY_CAST(flag AS BOOL) AS b, TRY_CAST(ts AS TIMESTAMP) AS t FROM s3object`,
			want: `{"i":123,"f":1.5,"b":true,"t":"2020-01-02T15:04:05Z"}
{"i":null,"f":null,"b":null,"t":null}
{"i":-7,"f":2.5,"b":false,"t":"2021-06-01T00:00:05Z"}`,
		},
		{
			name:  "where-int-is-not-null",
			query: `SELECT id FROM s3object WHERE TRY_CAST(num AS INT) IS NOT NULL`,
			want: `{"id":"1"}
{"id":"3"}`,
		},
		{
			name:  "where-int-is-null",
			query: `SELECT id FROM s3object WHERE TRY_CAST(num AS INT) IS NULL`,
			want:  `{"id":"2"}`,
		},
		{
			name:  "where-bool-eq-true",
			query: `SELECT id FROM s3object WHERE TRY_CAST(flag AS BOOL) = TRUE`,
			want:  `{"id":"1"}`,
		},
		{
			name:  "aggregate-args",
			query: `SELECT SUM(TRY_CAST(num AS INT)) AS s, COUNT(TRY_CAST(num AS INT)) AS c, AVG(TRY_CAST(flt AS FLOAT)) AS a, MIN(TRY_CAST(num AS INT)) AS mn, MAX(TRY_CAST(num AS INT)) AS mx FROM s3object`,
			want:  `{"s":116,"c":2,"a":2,"mn":-7,"mx":123}`,
		},
		{
			name:  "positional-column-out-of-range",
			query: `SELECT TRY_CAST(_9 AS INT) AS x FROM s3object`,
			want: `{"x":null}
{"x":null}
{"x":null}`,
		},
		// Untyped fields keep the CAST trimming/parsing rules.
		{
			name:  "whitespace-field",
			input: "n\n 42 \n",
			query: `SELECT TRY_CAST(n AS INT) AS i FROM s3object`,
			want:  `{"i":42}`,
		},
		{
			name:  "empty-field",
			input: "id,n\n1,\n",
			query: `SELECT TRY_CAST(n AS INT) AS i FROM s3object`,
			want:  `{"i":null}`,
		},
		// Regular CAST behavior over dirty CSV data is unchanged: it
		// aborts processing, while TRY_CAST would not.
		{name: "cast-still-fails-on-bad-value", query: `SELECT CAST(num AS INT) AS i FROM s3object`, wantReadErr: true},
		{name: "input-expr-divide-by-zero", query: `SELECT TRY_CAST(1/0 AS INT) AS r FROM s3object`, wantReadErr: true},
		{name: "unknown-target-type", query: `SELECT TRY_CAST(num AS DECIMAL) AS x FROM s3object`, wantOpenErr: true},
		{name: "malformed-syntax", query: `SELECT TRY_CAST(num AS INT FROM s3object`, wantOpenErr: true},
	}

	for _, tc := range cases {
		in := input
		if tc.input != "" {
			in = tc.input
		}
		t.Run(tc.name, func(t *testing.T) {
			got, openErr, readErr := execTryCastQuery(t, tryCastCSVRequest, tc.query, in)
			assertTryCastResult(t, tc, "csv", got, openErr, readErr)
		})
	}
}
