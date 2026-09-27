// Copyright 2022 The LUCI Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// Modified in 2026 by the flop authors. See NOTICE for source and attribution
// details.

package flop

// This file contains a scanner and parser for AIP-132 order_by clauses.
//
// Implemented grammar, where WS is zero or more spaces and SP is one or more:
// orderByList: WS clause {WS COMMA WS clause} WS EOF;
// clause:      fieldPath [SP "desc"];
// fieldPath:   segment {WS DOT WS segment};
// segment:     LITERAL | QUOTED;
// LITERAL:     [a-zA-Z_][a-zA-Z_0-9]*;
// QUOTED:      backtick quoted text, where a doubled backtick is a literal one.
//
// AIP-132 states redundant space characters are insignificant, so spaces are
// allowed around commas and dots and at either end of the clause. Only the
// space separating a field from its "desc" suffix is required.
import (
	"fmt"
	"slices"
	"strings"
)

// OrderBy represents a part of an AIP-132 order_by clause.
type OrderBy struct {
	// The field path. This is the path of the field in the
	// resource message that the AIP-132 List RPC is listing.
	FieldPath FieldPath
	// Whether the field should be sorted in descending order.
	Descending bool
}

// FieldPath represents the path to a field in a message.
//
// For example, for the given message:
//
//	message MyThing {
//	   message Bar {
//	       string foobar = 2;
//	   }
//	   string foo = 1;
//	   Bar bar = 2;
//	   map<string, Bar> named_bars = 3;
//	}
//
// Some valid paths would be: foo, bar.foobar and
// named_bars.`bar-key`.foobar.
type FieldPath struct {
	// The field path as its segments.
	segments []string

	// The canonical representation of the field path.
	canonical string
}

// NewFieldPath initializes a new field path with the given segments.
func NewFieldPath(segments ...string) FieldPath {
	var s strings.Builder
	for _, seg := range segments {
		if s.Len() > 0 {
			s.WriteString(".")
		}
		if isFieldLiteral(seg) {
			s.WriteString(seg)
		} else {
			s.WriteString("`")
			s.WriteString(strings.ReplaceAll(seg, "`", "``"))
			s.WriteString("`")
		}
	}
	return FieldPath{
		segments:  slices.Clone(segments),
		canonical: s.String(),
	}
}

// Equals returns iff two field paths refer to exactly the
// same field.
func (f FieldPath) Equals(other FieldPath) bool {
	return f.canonical == other.canonical
}

// String returns a canonical representation of the field path,
// following AIP-132 / AIP-161 syntax.
func (f FieldPath) String() string {
	return f.canonical
}

// Segments returns a copy of the path's unquoted segments.
func (f FieldPath) Segments() []string {
	return slices.Clone(f.segments)
}

// ParseOrderBy parses an AIP-132 order_by list. The method validates the
// syntax is correct and each identifier appears at most once, but
// it does not validate the identifiers themselves are valid.
func ParseOrderBy(text string) ([]OrderBy, error) {
	// Empty order_by list.
	if strings.Trim(text, " ") == "" {
		return nil, nil
	}

	s := scanner{input: text}
	result, err := s.list()
	if err != nil {
		return nil, fmt.Errorf("syntax error: %w", err)
	}

	uniqueFieldPaths := make(map[string]struct{})
	for _, orderBy := range result {
		if _, ok := uniqueFieldPaths[orderBy.FieldPath.String()]; ok {
			return nil, fmt.Errorf("field appears multiple times: %q", orderBy.FieldPath)
		}
		uniqueFieldPaths[orderBy.FieldPath.String()] = struct{}{}
	}

	return result, nil
}

// isFieldLiteral reports whether a path segment can be written unquoted.
func isFieldLiteral(s string) bool {
	if s == "" || !isLiteralStart(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		if !isLiteralByte(s[i]) {
			return false
		}
	}
	return true
}

func isLiteralStart(c byte) bool {
	return c == '_' || ('a' <= c && c <= 'z') || ('A' <= c && c <= 'Z')
}

func isLiteralByte(c byte) bool {
	return isLiteralStart(c) || ('0' <= c && c <= '9')
}

// scanner parses an order_by clause directly from its bytes. Every
// character the grammar recognises is ASCII, so a byte at a time is enough:
// a multi-byte rune can only appear inside a quoted segment, where it is
// copied through, or outside one, where it is a syntax error either way.
type scanner struct {
	input string
	pos   int
}

func (s *scanner) eof() bool {
	return s.pos >= len(s.input)
}

// spaces consumes a run of spaces and reports whether it consumed any.
func (s *scanner) spaces() bool {
	start := s.pos
	for s.pos < len(s.input) && s.input[s.pos] == ' ' {
		s.pos++
	}
	return s.pos > start
}

func (s *scanner) accept(c byte) bool {
	if s.eof() || s.input[s.pos] != c {
		return false
	}
	s.pos++
	return true
}

func (s *scanner) literal() (string, bool) {
	if s.eof() || !isLiteralStart(s.input[s.pos]) {
		return "", false
	}
	start := s.pos
	s.pos++
	for s.pos < len(s.input) && isLiteralByte(s.input[s.pos]) {
		s.pos++
	}
	return s.input[start:s.pos], true
}

func (s *scanner) quoted() (string, error) {
	s.pos++ // opening backtick
	var b strings.Builder
	for s.pos < len(s.input) {
		c := s.input[s.pos]
		if c != '`' {
			b.WriteByte(c)
			s.pos++
			continue
		}
		// A doubled backtick is an escaped one, a lone backtick closes.
		if s.pos+1 < len(s.input) && s.input[s.pos+1] == '`' {
			b.WriteByte('`')
			s.pos += 2
			continue
		}
		s.pos++
		return b.String(), nil
	}
	return "", fmt.Errorf("unterminated quoted segment at offset %d", s.pos)
}

func (s *scanner) segment() (string, error) {
	if !s.eof() && s.input[s.pos] == '`' {
		return s.quoted()
	}
	if lit, ok := s.literal(); ok {
		return lit, nil
	}
	return "", s.unexpected()
}

func (s *scanner) fieldPath() ([]string, error) {
	seg, err := s.segment()
	if err != nil {
		return nil, err
	}
	segments := []string{seg}
	for {
		save := s.pos
		s.spaces()
		if !s.accept('.') {
			s.pos = save
			return segments, nil
		}
		s.spaces()
		seg, err := s.segment()
		if err != nil {
			return nil, err
		}
		segments = append(segments, seg)
	}
}

func (s *scanner) clause() (OrderBy, error) {
	segments, err := s.fieldPath()
	if err != nil {
		return OrderBy{}, err
	}
	// fieldPath has already taken any trailing " . segment", so a space here
	// followed by exactly "desc" can only be the suffix. Note this means
	// "a. desc" is the single field a.desc, not field a in descending order.
	save := s.pos
	if s.spaces() {
		if lit, ok := s.literal(); ok && lit == "desc" {
			return OrderBy{FieldPath: NewFieldPath(segments...), Descending: true}, nil
		}
	}
	s.pos = save
	return OrderBy{FieldPath: NewFieldPath(segments...)}, nil
}

func (s *scanner) list() ([]OrderBy, error) {
	var result []OrderBy
	for {
		s.spaces()
		clause, err := s.clause()
		if err != nil {
			return nil, err
		}
		result = append(result, clause)
		s.spaces()
		if !s.accept(',') {
			break
		}
	}
	if !s.eof() {
		return nil, s.unexpected()
	}
	return result, nil
}

func (s *scanner) unexpected() error {
	if s.eof() {
		return fmt.Errorf("unexpected end of input at offset %d", s.pos)
	}
	r := []rune(s.input[s.pos:])[0]
	return fmt.Errorf("unexpected %q at offset %d", r, s.pos)
}
