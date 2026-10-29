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
	"strings"

	"github.com/minio/pkg/v3/wildcard"
)

// NewPattern - create new pattern for prefix/suffix.
func NewPattern(prefix, suffix string) (pattern string) {
	if prefix != "" {
		if !strings.HasSuffix(prefix, "*") {
			prefix += "*"
		}

		pattern = prefix
	}

	if suffix != "" {
		if !strings.HasPrefix(suffix, "*") {
			suffix = "*" + suffix
		}

		pattern += suffix
	}

	pattern = strings.ReplaceAll(pattern, "**", "*")

	return pattern
}

// Rule - represents an event notification matching rule made of an
// object name wildcard pattern and an optional inclusive object size
// range, specified in bytes.
//
// When HasMinSize and/or HasMaxSize are false the corresponding bound
// does not apply. A zero byte object is still matched against the
// configured bounds (MinObjectSize/MaxObjectSize may be zero).
type Rule struct {
	Pattern       string
	MinObjectSize int64
	MaxObjectSize int64
	HasMinSize    bool
	HasMaxSize    bool
}

// Match - returns true if the given object name and (optional) object
// size satisfy this rule. When the rule carries a size filter but the
// object size is not confirmable - objectSize is nil, e.g. for object
// removal events - the rule does not match.
func (rule Rule) Match(objectName string, objectSize *int64) bool {
	if !wildcard.MatchSimple(rule.Pattern, objectName) {
		return false
	}

	if !rule.HasMinSize && !rule.HasMaxSize {
		return true
	}

	// A size filter is configured but the event does not carry a
	// confirmable object size, so the rule cannot match.
	if objectSize == nil {
		return false
	}

	if rule.HasMinSize && *objectSize < rule.MinObjectSize {
		return false
	}

	if rule.HasMaxSize && *objectSize > rule.MaxObjectSize {
		return false
	}

	return true
}

// Rules - event rules
type Rules map[Rule]TargetIDSet

// Add - adds rule and target ID.
func (rules Rules) Add(rule Rule, targetID TargetID) {
	rules[rule] = NewTargetIDSet(targetID).Union(rules[rule])
}

// MatchSimple - returns true one of the matching object name in rules.
func (rules Rules) MatchSimple(objectName string) bool {
	for rule := range rules {
		if wildcard.MatchSimple(rule.Pattern, objectName) {
			return true
		}
	}
	return false
}

// Match - returns TargetIDSet matching object name and size in rules.
// A nil objectSize means the event does not carry a confirmable object
// size; rules with size filters do not match in that case.
func (rules Rules) Match(objectName string, objectSize *int64) TargetIDSet {
	targetIDs := NewTargetIDSet()

	for rule, targetIDSet := range rules {
		if rule.Match(objectName, objectSize) {
			targetIDs = targetIDs.Union(targetIDSet)
		}
	}

	return targetIDs
}

// Clone - returns copy of this rules.
func (rules Rules) Clone() Rules {
	rulesCopy := make(Rules)

	for rule, targetIDSet := range rules {
		rulesCopy[rule] = targetIDSet.Clone()
	}

	return rulesCopy
}

// Union - returns union with given rules as new rules.
func (rules Rules) Union(rules2 Rules) Rules {
	nrules := rules.Clone()

	for rule, targetIDSet := range rules2 {
		nrules[rule] = nrules[rule].Union(targetIDSet)
	}

	return nrules
}

// Difference - returns difference with given rules as new rules.
func (rules Rules) Difference(rules2 Rules) Rules {
	nrules := make(Rules)

	for rule, targetIDSet := range rules {
		if nv := targetIDSet.Difference(rules2[rule]); len(nv) > 0 {
			nrules[rule] = nv
		}
	}

	return nrules
}
