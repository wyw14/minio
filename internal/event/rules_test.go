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

package event

import (
	"reflect"
	"testing"
)

func TestNewPattern(t *testing.T) {
	testCases := []struct {
		prefix         string
		suffix         string
		expectedResult string
	}{
		{"", "", ""},
		{"*", "", "*"},
		{"", "*", "*"},
		{"images/", "", "images/*"},
		{"images/*", "", "images/*"},
		{"", "jpg", "*jpg"},
		{"", "*jpg", "*jpg"},
		{"images/", "jpg", "images/*jpg"},
		{"images/*", "jpg", "images/*jpg"},
		{"images/", "*jpg", "images/*jpg"},
		{"images/*", "*jpg", "images/*jpg"},
		{"201*/images/", "jpg", "201*/images/*jpg"},
	}

	for i, testCase := range testCases {
		result := NewPattern(testCase.prefix, testCase.suffix)

		if result != testCase.expectedResult {
			t.Fatalf("test %v: result: expected: %v, got: %v", i+1, testCase.expectedResult, result)
		}
	}
}

func int64p(v int64) *int64 {
	return &v
}

func TestRuleMatch(t *testing.T) {
	testCases := []struct {
		rule           Rule
		objectName     string
		objectSize     *int64
		expectedResult bool
	}{
		// Pattern matching, no size filter configured: events with and
		// without a confirmable object size keep matching.
		{Rule{Pattern: "*"}, "photos.jpg", nil, true},
		{Rule{Pattern: "2010*"}, "2010/photos.jpg", nil, true},
		{Rule{Pattern: "2010*"}, "2000/photos.jpg", nil, false},

		// Minimum object size, inclusive boundary.
		{Rule{Pattern: "*", MinObjectSize: 100, HasMinSize: true}, "photos.jpg", int64p(100), true},
		{Rule{Pattern: "*", MinObjectSize: 100, HasMinSize: true}, "photos.jpg", int64p(101), true},
		{Rule{Pattern: "*", MinObjectSize: 100, HasMinSize: true}, "photos.jpg", int64p(99), false},

		// Maximum object size, inclusive boundary.
		{Rule{Pattern: "*", MaxObjectSize: 100, HasMaxSize: true}, "photos.jpg", int64p(100), true},
		{Rule{Pattern: "*", MaxObjectSize: 100, HasMaxSize: true}, "photos.jpg", int64p(99), true},
		{Rule{Pattern: "*", MaxObjectSize: 100, HasMaxSize: true}, "photos.jpg", int64p(101), false},

		// Both bounds configured, inclusive boundaries.
		{Rule{Pattern: "*", MinObjectSize: 100, MaxObjectSize: 200, HasMinSize: true, HasMaxSize: true}, "photos.jpg", int64p(100), true},
		{Rule{Pattern: "*", MinObjectSize: 100, MaxObjectSize: 200, HasMinSize: true, HasMaxSize: true}, "photos.jpg", int64p(200), true},
		{Rule{Pattern: "*", MinObjectSize: 100, MaxObjectSize: 200, HasMinSize: true, HasMaxSize: true}, "photos.jpg", int64p(150), true},
		{Rule{Pattern: "*", MinObjectSize: 100, MaxObjectSize: 200, HasMinSize: true, HasMaxSize: true}, "photos.jpg", int64p(99), false},
		{Rule{Pattern: "*", MinObjectSize: 100, MaxObjectSize: 200, HasMinSize: true, HasMaxSize: true}, "photos.jpg", int64p(201), false},

		// A zero byte object is compared as zero bytes.
		{Rule{Pattern: "*", MinObjectSize: 0, HasMinSize: true}, "empty", int64p(0), true},
		{Rule{Pattern: "*", MaxObjectSize: 0, HasMaxSize: true}, "empty", int64p(0), true},
		{Rule{Pattern: "*", MinObjectSize: 1, HasMinSize: true}, "empty", int64p(0), false},
		{Rule{Pattern: "*", MinObjectSize: 0, MaxObjectSize: 0, HasMinSize: true, HasMaxSize: true}, "empty", int64p(0), true},
		{Rule{Pattern: "*", MinObjectSize: 0, MaxObjectSize: 0, HasMinSize: true, HasMaxSize: true}, "non-empty", int64p(1), false},

		// Size filter configured but the event does not carry a
		// confirmable object size (e.g. removal events): no match.
		{Rule{Pattern: "*", MinObjectSize: 0, HasMinSize: true}, "photos.jpg", nil, false},
		{Rule{Pattern: "*", MaxObjectSize: 1024, HasMaxSize: true}, "photos.jpg", nil, false},
		{Rule{Pattern: "*", MinObjectSize: 0, MaxObjectSize: 0, HasMinSize: true, HasMaxSize: true}, "photos.jpg", nil, false},

		// Size filter only applies when the pattern matches as well.
		{Rule{Pattern: "2010*", MinObjectSize: 100, HasMinSize: true}, "2000/photos.jpg", int64p(100), false},
		{Rule{Pattern: "2010*", MaxObjectSize: 100, HasMaxSize: true}, "2010/photos.jpg", int64p(99), true},
	}

	for i, testCase := range testCases {
		result := testCase.rule.Match(testCase.objectName, testCase.objectSize)

		if result != testCase.expectedResult {
			t.Fatalf("test %v: result: expected: %v, got: %v (rule: %+v, size: %v)", i+1, testCase.expectedResult, result, testCase.rule, testCase.objectSize)
		}
	}
}

func TestRulesAdd(t *testing.T) {
	rulesCase1 := make(Rules)

	rulesCase2 := make(Rules)
	rulesCase2.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})

	rulesCase3 := make(Rules)
	rulesCase3.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})

	rulesCase4 := make(Rules)
	rulesCase4.Add(Rule{Pattern: NewPattern("", "*.jpg")}, TargetID{"1", "webhook"})

	rulesCase5 := make(Rules)

	rulesCase6 := make(Rules)
	rulesCase6.Add(Rule{Pattern: NewPattern("", "*.jpg")}, TargetID{"1", "webhook"})

	rulesCase7 := make(Rules)
	rulesCase7.Add(Rule{Pattern: NewPattern("", "*.jpg")}, TargetID{"1", "webhook"})

	rulesCase8 := make(Rules)
	rulesCase8.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})

	testCases := []struct {
		rules          Rules
		rule           Rule
		targetID       TargetID
		expectedResult int
	}{
		{rulesCase1, Rule{Pattern: NewPattern("*", "")}, TargetID{"1", "webhook"}, 1},
		{rulesCase2, Rule{Pattern: NewPattern("*", "")}, TargetID{"2", "amqp"}, 2},
		{rulesCase3, Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"}, 1},
		{rulesCase4, Rule{Pattern: NewPattern("*", "")}, TargetID{"1", "webhook"}, 2},
		{rulesCase5, Rule{Pattern: NewPattern("", "*.jpg")}, TargetID{"1", "webhook"}, 1},
		{rulesCase6, Rule{Pattern: NewPattern("", "*")}, TargetID{"2", "amqp"}, 2},
		{rulesCase7, Rule{Pattern: NewPattern("", "*.jpg")}, TargetID{"1", "webhook"}, 1},
		{rulesCase8, Rule{Pattern: NewPattern("", "*.jpg")}, TargetID{"1", "webhook"}, 2},
	}

	for i, testCase := range testCases {
		testCase.rules.Add(testCase.rule, testCase.targetID)
		result := len(testCase.rules)

		if result != testCase.expectedResult {
			t.Fatalf("test %v: result: expected: %v, got: %v", i+1, testCase.expectedResult, result)
		}
	}
}

func TestRulesMatch(t *testing.T) {
	rulesCase1 := make(Rules)

	rulesCase2 := make(Rules)
	rulesCase2.Add(Rule{Pattern: NewPattern("*", "*")}, TargetID{"1", "webhook"})

	rulesCase3 := make(Rules)
	rulesCase3.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})
	rulesCase3.Add(Rule{Pattern: NewPattern("", "*.png")}, TargetID{"2", "amqp"})

	rulesCase4 := make(Rules)
	rulesCase4.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})

	rulesCase5 := make(Rules)
	rulesCase5.Add(Rule{Pattern: "*", MinObjectSize: 100, MaxObjectSize: 200, HasMinSize: true, HasMaxSize: true}, TargetID{"1", "webhook"})
	rulesCase5.Add(Rule{Pattern: "*"}, TargetID{"2", "amqp"})

	testCases := []struct {
		rules          Rules
		objectName     string
		objectSize     *int64
		expectedResult TargetIDSet
	}{
		{rulesCase1, "photos.jpg", nil, NewTargetIDSet()},
		{rulesCase2, "photos.jpg", nil, NewTargetIDSet(TargetID{"1", "webhook"})},
		{rulesCase3, "2010/photos.jpg", nil, NewTargetIDSet(TargetID{"1", "webhook"})},
		{rulesCase4, "2000/photos.jpg", nil, NewTargetIDSet()},

		// Size filtered rule and unfiltered rule coexist: an object
		// outside the size range only matches the unfiltered target.
		{rulesCase5, "photos.jpg", int64p(150), NewTargetIDSet(TargetID{"1", "webhook"}, TargetID{"2", "amqp"})},
		{rulesCase5, "photos.jpg", int64p(50), NewTargetIDSet(TargetID{"2", "amqp"})},
		// Zero byte objects are compared as zero bytes.
		{rulesCase5, "photos.jpg", int64p(0), NewTargetIDSet(TargetID{"2", "amqp"})},
		// No confirmable size (e.g. removal event): size filtered rule
		// does not match, unfiltered rule still does.
		{rulesCase5, "photos.jpg", nil, NewTargetIDSet(TargetID{"2", "amqp"})},
	}

	for i, testCase := range testCases {
		result := testCase.rules.Match(testCase.objectName, testCase.objectSize)

		if !reflect.DeepEqual(testCase.expectedResult, result) {
			t.Fatalf("test %v: result: expected: %v, got: %v", i+1, testCase.expectedResult, result)
		}
	}
}

func TestRulesClone(t *testing.T) {
	rulesCase1 := make(Rules)

	rulesCase2 := make(Rules)
	rulesCase2.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})

	rulesCase3 := make(Rules)
	rulesCase3.Add(Rule{Pattern: NewPattern("", "*.jpg")}, TargetID{"1", "webhook"})

	testCases := []struct {
		rules    Rules
		prefix   string
		targetID TargetID
	}{
		{rulesCase1, "2010*", TargetID{"1", "webhook"}},
		{rulesCase2, "2000*", TargetID{"2", "amqp"}},
		{rulesCase3, "2010*", TargetID{"1", "webhook"}},
	}

	for i, testCase := range testCases {
		result := testCase.rules.Clone()

		if !reflect.DeepEqual(result, testCase.rules) {
			t.Fatalf("test %v: result: expected: %v, got: %v", i+1, testCase.rules, result)
		}

		result.Add(Rule{Pattern: NewPattern(testCase.prefix, "")}, testCase.targetID)
		if reflect.DeepEqual(result, testCase.rules) {
			t.Fatalf("test %v: result: expected: not equal, got: equal", i+1)
		}
	}
}

func TestRulesUnion(t *testing.T) {
	rulesCase1 := make(Rules)
	rules2Case1 := make(Rules)
	expectedResultCase1 := make(Rules)

	rulesCase2 := make(Rules)
	rules2Case2 := make(Rules)
	rules2Case2.Add(Rule{Pattern: NewPattern("*", "")}, TargetID{"1", "webhook"})
	expectedResultCase2 := make(Rules)
	expectedResultCase2.Add(Rule{Pattern: NewPattern("*", "")}, TargetID{"1", "webhook"})

	rulesCase3 := make(Rules)
	rulesCase3.Add(Rule{Pattern: NewPattern("", "*")}, TargetID{"1", "webhook"})
	rules2Case3 := make(Rules)
	expectedResultCase3 := make(Rules)
	expectedResultCase3.Add(Rule{Pattern: NewPattern("", "*")}, TargetID{"1", "webhook"})

	rulesCase4 := make(Rules)
	rulesCase4.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})
	rules2Case4 := make(Rules)
	rules2Case4.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})
	expectedResultCase4 := make(Rules)
	expectedResultCase4.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})

	rulesCase5 := make(Rules)
	rulesCase5.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})
	rulesCase5.Add(Rule{Pattern: NewPattern("", "*.png")}, TargetID{"2", "amqp"})
	rules2Case5 := make(Rules)
	rules2Case5.Add(Rule{Pattern: NewPattern("*", "")}, TargetID{"1", "webhook"})
	expectedResultCase5 := make(Rules)
	expectedResultCase5.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})
	expectedResultCase5.Add(Rule{Pattern: NewPattern("", "*.png")}, TargetID{"2", "amqp"})
	expectedResultCase5.Add(Rule{Pattern: NewPattern("*", "")}, TargetID{"1", "webhook"})

	testCases := []struct {
		rules          Rules
		rules2         Rules
		expectedResult Rules
	}{
		{rulesCase1, rules2Case1, expectedResultCase1},
		{rulesCase2, rules2Case2, expectedResultCase2},
		{rulesCase3, rules2Case3, expectedResultCase3},
		{rulesCase4, rules2Case4, expectedResultCase4},
		{rulesCase5, rules2Case5, expectedResultCase5},
	}

	for i, testCase := range testCases {
		result := testCase.rules.Union(testCase.rules2)

		if !reflect.DeepEqual(testCase.expectedResult, result) {
			t.Fatalf("test %v: result: expected: %v, got: %v", i+1, testCase.expectedResult, result)
		}
	}
}

func TestRulesDifference(t *testing.T) {
	rulesCase1 := make(Rules)
	rules2Case1 := make(Rules)
	expectedResultCase1 := make(Rules)

	rulesCase2 := make(Rules)
	rules2Case2 := make(Rules)
	rules2Case2.Add(Rule{Pattern: NewPattern("*", "*")}, TargetID{"1", "webhook"})
	expectedResultCase2 := make(Rules)

	rulesCase3 := make(Rules)
	rulesCase3.Add(Rule{Pattern: NewPattern("*", "*")}, TargetID{"1", "webhook"})
	rules2Case3 := make(Rules)
	expectedResultCase3 := make(Rules)
	expectedResultCase3.Add(Rule{Pattern: NewPattern("*", "*")}, TargetID{"1", "webhook"})

	rulesCase4 := make(Rules)
	rulesCase4.Add(Rule{Pattern: NewPattern("*", "*")}, TargetID{"1", "webhook"})
	rules2Case4 := make(Rules)
	rules2Case4.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})
	rules2Case4.Add(Rule{Pattern: NewPattern("", "*.png")}, TargetID{"2", "amqp"})
	expectedResultCase4 := make(Rules)
	expectedResultCase4.Add(Rule{Pattern: NewPattern("*", "*")}, TargetID{"1", "webhook"})

	rulesCase5 := make(Rules)
	rulesCase5.Add(Rule{Pattern: NewPattern("*", "")}, TargetID{"1", "webhook"})
	rulesCase5.Add(Rule{Pattern: NewPattern("", "*")}, TargetID{"2", "amqp"})
	rules2Case5 := make(Rules)
	rules2Case5.Add(Rule{Pattern: NewPattern("2010*", "")}, TargetID{"1", "webhook"})
	rules2Case5.Add(Rule{Pattern: NewPattern("", "*")}, TargetID{"2", "amqp"})
	expectedResultCase5 := make(Rules)
	expectedResultCase5.Add(Rule{Pattern: NewPattern("*", "")}, TargetID{"1", "webhook"})

	// Rules with the same pattern but different size filters are
	// distinct entries and must not remove each other.
	rulesCase6 := make(Rules)
	rulesCase6.Add(Rule{Pattern: "*", MinObjectSize: 100, HasMinSize: true}, TargetID{"1", "webhook"})
	rules2Case6 := make(Rules)
	rules2Case6.Add(Rule{Pattern: "*"}, TargetID{"1", "webhook"})
	expectedResultCase6 := make(Rules)
	expectedResultCase6.Add(Rule{Pattern: "*", MinObjectSize: 100, HasMinSize: true}, TargetID{"1", "webhook"})

	testCases := []struct {
		rules          Rules
		rules2         Rules
		expectedResult Rules
	}{
		{rulesCase1, rules2Case1, expectedResultCase1},
		{rulesCase2, rules2Case2, expectedResultCase2},
		{rulesCase3, rules2Case3, expectedResultCase3},
		{rulesCase4, rules2Case4, expectedResultCase4},
		{rulesCase5, rules2Case5, expectedResultCase5},
		{rulesCase6, rules2Case6, expectedResultCase6},
	}

	for i, testCase := range testCases {
		result := testCase.rules.Difference(testCase.rules2)

		if !reflect.DeepEqual(testCase.expectedResult, result) {
			t.Fatalf("test %v: result: expected: %v, got: %v", i+1, testCase.expectedResult, result)
		}
	}
}
