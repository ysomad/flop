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

// This file contains a lexer and parser for AIP-160 filter expressions.
// The EBNF is at https://google.aip.dev/assets/misc/ebnf-filtering.txt
// The function call syntax is not supported which simplifies the parser.
//
// Implemented EBNF (in terms of lexer tokens):
// filter: [expression];
// expression: sequence {WS AND WS sequence};
// sequence: factor {WS factor};
// factor: term {WS OR WS term};
// term: [NEGATE] simple;
// simple: restriction | composite;
// restriction: comparable [COMPARATOR arg];
// comparable: member;
// member: (TEXT | STRING) {DOT (TEXT | STRING)};
// composite: LPAREN expression RPAREN;
// arg: comparable | composite;
import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	kindComparator = "COMPARATOR"
	kindNegate     = "NEGATE"
	kindAnd        = "AND"
	kindOr         = "OR"
	kindDot        = "DOT"
	kindLParen     = "LPAREN"
	kindRParen     = "RPAREN"
	kindComma      = "COMMA"
	kindString     = "STRING"
	kindText       = "TEXT"
	kindEnd        = "END"
)

// lexerRE has one group for each kind of token that can be lexed, in the order of the kind consts above. There are two cases for kindNegate to handle whitespace correctly.
var lexerRE = regexp.MustCompile(
	`^(?:(<=|>=|!=|<|>|=|\:)|(NOT\s)|(-)|(AND\s)|(OR\s)|(\.)|(\()|(\))|(,)|("(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*')|([^\s\.,<>=!:\(\)]+))`,
)

// negativeNumberRE matches a '-' that starts a numeric literal rather than
// a negation. It uses the same character class as the TEXT group above.
var negativeNumberRE = regexp.MustCompile(`^-[0-9][^\s\.,<>=!:\(\)]*`)

type token struct {
	kind  string
	value string
	// space reports whether whitespace preceded the token.
	space bool
}

// parser reads an AIP-160 filter directly from its input. It is the whole
// state of one parse: the unread input, and the token peeked past it.
type parser struct {
	input  string
	peeked *token
}

func (p *parser) peek() (*token, error) {
	if p.peeked == nil {
		var err error
		p.peeked, err = p.next()
		if err != nil {
			return nil, err
		}
	}
	return p.peeked, nil
}

func (p *parser) next() (*token, error) {
	if p.peeked != nil {
		next := p.peeked
		p.peeked = nil
		return next, nil
	}
	trimmed := strings.TrimLeft(p.input, " \t\r\n")
	space := len(trimmed) != len(p.input)
	p.input = trimmed
	t, err := p.lex()
	if err != nil {
		return nil, err
	}
	t.space = space
	return t, nil
}

// lex reads the next token from the head of the input, which the caller has
// already stripped of leading whitespace.
func (p *parser) lex() (*token, error) {
	if p.input == "" {
		return &token{kind: kindEnd}, nil
	}
	if p.input[0] == '\'' || p.input[0] == '"' {
		quote := p.input[0]
		for i := 1; i < len(p.input); i++ {
			if p.input[i] == '\\' {
				i++
				continue
			}
			if p.input[i] == quote {
				value := p.input[:i+1]
				p.input = p.input[i+1:]
				return &token{kind: kindString, value: value}, nil
			}
		}
		return nil, fmt.Errorf("unterminated quoted string")
	}
	// At EOF these are still operators, so dangling operators are diagnosed.
	switch p.input {
	case "AND", "OR", "NOT":
		value := p.input
		p.input = ""
		kind := value
		if value == "NOT" {
			kind = kindNegate
		}
		return &token{kind: kind, value: value}, nil
	}
	if value := negativeNumberRE.FindString(p.input); value != "" {
		p.input = p.input[len(value):]
		return &token{kind: kindText, value: value}, nil
	}
	matches := lexerRE.FindStringSubmatch(p.input)
	if matches == nil {
		return nil, fmt.Errorf("error: unable to lex token from %q", p.input)
	}
	p.input = p.input[len(matches[0]):]
	if matches[1] != "" {
		return &token{kind: kindComparator, value: matches[1]}, nil
	}
	if matches[2] != "" {
		// Needs to be fixed up to compensate for the trailing \s in the match which prevents
		// matching "NOTother" as a negated "other".
		length := len(matches[2])
		return &token{kind: kindNegate, value: matches[2][:length-1]}, nil
	}
	if matches[3] != "" {
		return &token{kind: kindNegate, value: matches[3]}, nil
	}
	if matches[4] != "" {
		// Needs to be fixed up to compensate for the trailing \s in the match which prevents
		// matching "ANDother" as a "AND" "other".
		length := len(matches[4])
		return &token{kind: kindAnd, value: matches[4][:length-1]}, nil
	}
	if matches[5] != "" {
		// Needs to be fixed up to compensate for the trailing \s in the match which prevents
		// matching "ORother" as a "OR" "other".
		length := len(matches[5])
		return &token{kind: kindOr, value: matches[5][:length-1]}, nil
	}
	if matches[6] != "" {
		return &token{kind: kindDot, value: matches[6]}, nil
	}
	if matches[7] != "" {
		return &token{kind: kindLParen, value: matches[7]}, nil
	}
	if matches[8] != "" {
		return &token{kind: kindRParen, value: matches[8]}, nil
	}
	if matches[9] != "" {
		return &token{kind: kindComma, value: matches[9]}, nil
	}
	if matches[10] != "" {
		return &token{kind: kindString, value: matches[10]}, nil
	}
	if matches[11] != "" {
		return &token{kind: kindText, value: matches[11]}, nil
	}
	return nil, fmt.Errorf("error: unhandled lexer regexp match %q", matches[0])
}

// Filter possibly empty.
//
// These are based on the EBNF at https://google.aip.dev/assets/misc/ebnf-filtering.txt
// Note that the syntax for functions is not currently supported.
type Filter struct {
	expression *expression // Optional, may be nil.
}

func (v *Filter) String() string {
	var s strings.Builder
	s.WriteString("filter{")
	if v != nil && v.expression != nil {
		s.WriteString(v.expression.String())
	}
	s.WriteString("}")
	return s.String()
}

// expression may either be a conjunction (AND) of sequences or a simple
// sequence.
//
// Note, the AND is case-sensitive.
//
// Example: `a b AND c AND d`
//
// The expression `(a b) AND c AND d` is equivalent to the example.
type expression struct {
	// Sequences are always joined by an AND operator
	Sequences []sequence
}

func (v *expression) String() string {
	var s strings.Builder
	s.WriteString("expression{")
	for i, c := range v.Sequences {
		if i > 0 {
			s.WriteString(",")
		}
		s.WriteString(c.String())
	}
	s.WriteString("}")
	return s.String()
}

// sequence is composed of one or more whitespace (WS) separated factors.
//
// A sequence expresses a logical relationship between 'factors' where
// the ranking of a filter result may be scored according to the number
// factors that match and other such criteria as the proximity of factors
// to each other within a document.
//
// When filters are used with exact match semantics rather than fuzzy
// match semantics, a sequence is equivalent to AND.
//
// Example: `New York Giants OR Yankees`
//
// The expression `New York (Giants OR Yankees)` is equivalent to the
// example.
type sequence struct {
	// Factors are always joined by an (implicit) AND operator
	Factors []factor
}

func (v sequence) String() string {
	var s strings.Builder
	s.WriteString("sequence{")
	for i, c := range v.Factors {
		if i > 0 {
			s.WriteString(",")
		}
		s.WriteString(c.String())
	}
	s.WriteString("}")
	return s.String()
}

// factor may either be a disjunction (OR) of terms or a simple term.
//
// Note, the OR is case-sensitive.
//
// Example: `a < 10 OR a >= 100`
type factor struct {
	// Terms are always joined by an OR operator
	Terms []term
}

func (v factor) String() string {
	var s strings.Builder
	s.WriteString("factor{")
	for i, c := range v.Terms {
		if i > 0 {
			s.WriteString(",")
		}
		s.WriteString(c.String())
	}
	s.WriteString("}")
	return s.String()
}

// term may either be unary or simple expressions.
//
// Unary expressions negate the simple expression, either mathematically `-`
// or logically `NOT`. The negation styles may be used interchangeably.
//
// Note, the `NOT` is case-sensitive and must be followed by at least one
// whitespace (WS).
//
// Examples:
// * logical not     : `NOT (a OR b)`
// * alternative not : `-file:".java"`
// * negation        : `-30`
type term struct {
	Negated bool
	Simple  simple
}

func (v term) String() string {
	var s strings.Builder
	s.WriteString("term{")
	if v.Negated {
		s.WriteString("-")
	}
	s.WriteString(v.Simple.String())
	s.WriteString("}")
	return s.String()
}

// simple expressions may either be a restriction or a nested (composite)
// expression.
type simple struct {
	Restriction *restriction
	// Composite is a parenthesized expression, commonly used to group
	// terms or clarify operator precedence.
	//
	// Example: `(msg.endsWith('world') AND retries < 10)`
	Composite *expression
}

func (v simple) String() string {
	var s strings.Builder
	s.WriteString("simple{")
	if v.Restriction != nil {
		s.WriteString(v.Restriction.String())
	}
	if v.Restriction != nil && v.Composite != nil {
		s.WriteString(",")
	}
	if v.Composite != nil {
		s.WriteString(v.Composite.String())
	}
	s.WriteString("}")
	return s.String()
}

// restriction express a relationship between a comparable value and a
// single argument. When the restriction only specifies a comparable
// without an operator, this is a global restriction.
//
// Note, restrictions are not whitespace sensitive.
//
// Examples:
// * equality         : `package=com.google`
// * inequality       : `msg != 'hello'`
// * greater than     : `1 > 0`
// * greater or equal : `2.5 >= 2.4`
// * less than        : `yesterday < request.time`
// * less or equal    : `experiment.rollout <= cohort(request.user)`
// * has              : `map:key`
// * global           : `prod`
//
// In addition to the global, equality, and ordering operators, filters
// also support the has (`:`) operator. The has operator is unique in
// that it can test for presence or value based on the proto3 type of
// the `comparable` value. The has operator is useful for validating the
// structure and contents of complex values.
type restriction struct {
	Member member
	// Comparators supported by list filters: <=, <. >=, >, !=, =, :
	Comparator string
	Arg        *arg
}

func (v *restriction) String() string {
	var s strings.Builder
	s.WriteString("restriction{")
	s.WriteString(v.Member.String())
	if v.Comparator != "" {
		s.WriteString(",")
		s.WriteString(strconv.Quote(v.Comparator))
	}
	if v.Arg != nil {
		s.WriteString(",")
		s.WriteString(v.Arg.String())
	}
	s.WriteString("}")
	return s.String()
}

type arg struct {
	Member member
	// Composite is a parenthesized expression, commonly used to group
	// terms or clarify operator precedence.
	//
	// Example: `(msg.endsWith('world') AND retries < 10)`
	Composite *expression
}

func (v *arg) String() string {
	var s strings.Builder
	s.WriteString("arg{")
	if v.Composite != nil {
		s.WriteString(v.Composite.String())
	} else {
		s.WriteString(v.Member.String())
	}
	s.WriteString("}")
	return s.String()
}

// member expressions are either value or DOT qualified field references.
//
// Example: `expr.type_map.1.type`
type member struct {
	Value  value
	Fields []value
}

func (v member) String() string {
	var s strings.Builder
	s.WriteString("member{")
	s.WriteString(v.Value.String())
	if len(v.Fields) > 0 {
		s.WriteString(", {")
	}
	for i, c := range v.Fields {
		if i > 0 {
			s.WriteString(",")
		}
		s.WriteString(c.String())
	}
	if len(v.Fields) > 0 {
		s.WriteString("}")
	}
	s.WriteString("}")
	return s.String()
}

// Input returns the input text used to produce the value, for used in errors.
func (v member) Input() string {
	var s strings.Builder
	s.WriteString(v.Value.Input())
	for _, f := range v.Fields {
		s.WriteString(".")
		s.WriteString(f.Input())
	}
	return s.String()
}

// Path returns the member as a dot-separated path of its raw segment values.
//
// It is what a caller matches against a declared field, and what reassembles
// an argument the lexer split on a dot, such as the float 1.5 or the duration
// 1.5s. Input renders the same segments as filter source, re-quoting the
// quoted ones, so it cannot serve either purpose.
func (v member) Path() string {
	if len(v.Fields) == 0 {
		return v.Value.Value
	}
	var b strings.Builder
	b.WriteString(v.Value.Value)
	for _, field := range v.Fields {
		b.WriteByte('.')
		b.WriteString(field.Value)
	}
	return b.String()
}

// Quoted reports whether any segment of the member was a quoted string. A
// quoted argument is text and never a number, a bool, or the null literal.
func (v member) Quoted() bool {
	if v.Value.Quoted {
		return true
	}
	for _, field := range v.Fields {
		if field.Quoted {
			return true
		}
	}
	return false
}

// value may either be a TEXT or STRING.
//
// TEXT is a free-form set of characters without whitespace (WS)
// or . (DOT) within it. The text may represent a variable, string,
// number, boolean, or alternative literal value and must be handled
// in a manner consistent with the service's intention.
//
// STRING is a quoted string which may or may not contain a special
// wildcard `*` character at the beginning or end of the string to
// indicate a prefix or suffix-based search within a restriction.
type value struct {
	Quoted bool
	Value  string
}

func (v value) String() string {
	var s strings.Builder
	s.WriteString("value{")
	if v.Quoted {
		s.WriteString("quoted,")
	}
	s.WriteString(strconv.Quote(v.Value))
	s.WriteString("}")
	return s.String()
}

// Input returns the input text used to produce the value, for used in errors.
func (v value) Input() string {
	if v.Quoted {
		// Note this is currently not identical as it may not reproduce the
		// exact set of escape sequences but it is close enough.
		return strconv.Quote(v.Value)
	}
	return v.Value
}

// ParseFilter parses an AIP-160 filter string into an AST.
func ParseFilter(text string) (*Filter, error) {
	p := parser{input: text}
	return p.filter()
}

func (p *parser) expect(kind string) error {
	t, err := p.peek()
	if err != nil {
		return err
	}
	if t.kind != kind {
		return fmt.Errorf("expected %s but got %s(%q)", kind, t.kind, t.value)
	}
	_, err = p.next()
	return err
}

// expectSpaceBefore fails if the next token is of kind but no whitespace
// precedes it. A token of another kind is left for the caller to handle.
func (p *parser) expectSpaceBefore(kind string) error {
	t, err := p.peek()
	if err != nil {
		return err
	}
	if t.kind == kind && !t.space {
		return fmt.Errorf("expected whitespace before %s(%q)", t.kind, t.value)
	}
	return nil
}

// rejectSpaceBefore fails if the next token is of kind and whitespace precedes
// it. A token of another kind is left for the caller to handle.
func (p *parser) rejectSpaceBefore(kind string) error {
	t, err := p.peek()
	if err != nil {
		return err
	}
	if t.kind == kind && t.space {
		return fmt.Errorf("unexpected whitespace before %s(%q)", t.kind, t.value)
	}
	return nil
}

// startsFactor reports whether a token of kind can begin a factor.
func startsFactor(kind string) bool {
	switch kind {
	case kindText, kindString, kindNegate, kindLParen:
		return true
	}
	return false
}

func (p *parser) accept(kind string) (*token, error) {
	t, err := p.peek()
	if err != nil {
		return nil, err
	}
	if t.kind != kind {
		return nil, nil
	}
	return p.next()
}

func (p *parser) filter() (*Filter, error) {
	t, err := p.accept(kindEnd)
	if err != nil {
		return nil, err
	}
	if t != nil {
		return &Filter{}, nil
	}
	e, err := p.expression()
	if err != nil {
		return nil, err
	}
	return &Filter{expression: e}, p.expect(kindEnd)
}

func (p *parser) expression() (*expression, error) {
	s, ok, err := p.sequence()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	e := &expression{}
	e.Sequences = append(e.Sequences, s)
	for {
		if err := p.expectSpaceBefore(kindAnd); err != nil {
			return nil, err
		}
		and, err := p.accept(kindAnd)
		if err != nil {
			return nil, err
		}
		if and == nil {
			break
		}
		s, ok, err := p.sequence()
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, fmt.Errorf("expected sequence after AND")
		}
		e.Sequences = append(e.Sequences, s)
	}
	return e, nil
}

func (p *parser) sequence() (sequence, bool, error) {
	var s sequence
	for {
		if len(s.Factors) > 0 {
			t, err := p.peek()
			if err != nil {
				return sequence{}, false, err
			}
			if startsFactor(t.kind) && !t.space {
				return sequence{}, false, fmt.Errorf("expected whitespace before %s(%q)", t.kind, t.value)
			}
		}
		f, ok, err := p.factor()
		if err != nil {
			return sequence{}, false, err
		}
		if !ok {
			break
		}
		s.Factors = append(s.Factors, f)
	}
	if len(s.Factors) == 0 {
		return sequence{}, false, nil
	}
	return s, true, nil
}

func (p *parser) factor() (factor, bool, error) {
	t, ok, err := p.term()
	if err != nil {
		return factor{}, false, err
	}
	if !ok {
		return factor{}, false, nil
	}
	var f factor
	f.Terms = append(f.Terms, t)
	for {
		if err := p.expectSpaceBefore(kindOr); err != nil {
			return factor{}, false, err
		}
		or, err := p.accept(kindOr)
		if err != nil {
			return factor{}, false, err
		}
		if or == nil {
			break
		}
		t, ok, err := p.term()
		if err != nil {
			return factor{}, false, err
		}
		if !ok {
			return factor{}, false, fmt.Errorf("expected term after OR")
		}
		f.Terms = append(f.Terms, t)
	}
	return f, true, nil
}

func (p *parser) term() (term, bool, error) {
	n, err := p.accept(kindNegate)
	if err != nil {
		return term{}, false, err
	}
	// A '-' negates what it abuts, so "- 30" is not a term. NOT is the other
	// way around, and the lexer already required the whitespace after it.
	if n != nil && n.value == "-" {
		t, err := p.peek()
		if err != nil {
			return term{}, false, err
		}
		if t.space {
			return term{}, false, fmt.Errorf("unexpected whitespace after %q", n.value)
		}
	}
	s, ok, err := p.simple()
	if err != nil {
		return term{}, false, err
	}
	if !ok {
		if n != nil {
			return term{}, false, fmt.Errorf("expected simple term after negation %q", n.value)
		}
		return term{}, false, nil
	}
	return term{Negated: n != nil, Simple: s}, true, nil
}

func (p *parser) simple() (simple, bool, error) {
	r, err := p.restriction()
	if err != nil {
		return simple{}, false, err
	}
	if r != nil {
		return simple{Restriction: r}, true, nil
	}
	c, err := p.composite()
	if err != nil {
		return simple{}, false, err
	}
	if c != nil {
		return simple{Composite: c}, true, nil
	}
	return simple{}, false, nil
}

func (p *parser) restriction() (*restriction, error) {
	m, ok, err := p.member()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	comparator, err := p.accept(kindComparator)
	if err != nil {
		return nil, err
	}
	if comparator == nil {
		return &restriction{Member: m}, nil
	}
	arg, err := p.arg()
	if err != nil {
		return nil, err
	}
	if arg == nil {
		return nil, fmt.Errorf("expected arg after %s", comparator.value)
	}
	return &restriction{Member: m, Comparator: comparator.value, Arg: arg}, nil
}

func (p *parser) member() (member, bool, error) {
	v, ok, err := p.value()
	if err != nil {
		return member{}, false, err
	}
	if !ok {
		return member{}, false, nil
	}

	m := member{Value: v}
	for {
		if err := p.rejectSpaceBefore(kindDot); err != nil {
			return member{}, false, err
		}
		dot, err := p.accept(kindDot)
		if err != nil {
			return member{}, false, err
		}
		if dot == nil {
			break
		}
		t, err := p.peek()
		if err != nil {
			return member{}, false, err
		}
		if t.space {
			return member{}, false, fmt.Errorf("unexpected whitespace after %q", dot.value)
		}

		v, ok, err := p.value()
		if err != nil {
			return member{}, false, err
		}
		if !ok {
			return member{}, false, fmt.Errorf("expected value after '.'")
		}

		m.Fields = append(m.Fields, v)
	}
	return m, true, nil
}

// value attempts to consume a value from the input tokens, and reports whether
// it read one.
func (p *parser) value() (value, bool, error) {
	v, err := p.accept(kindString)
	if err != nil {
		return value{}, false, err
	}
	if v != nil {
		v.value, err = unquoteString(v.value)
		if err != nil {
			return value{}, false, fmt.Errorf("error unquoting string: %w", err)
		}
		return value{Quoted: true, Value: v.value}, true, nil
	}

	v, err = p.accept(kindText)
	if err != nil {
		return value{}, false, err
	}
	if v == nil {
		return value{}, false, nil
	}
	return value{Value: v.value}, true, nil
}

// unquoteString gives both quote styles the same escapes and permits any
// number of characters. Unquote alone treats single quotes as Go rune literals.
func unquoteString(text string) (string, error) {
	quote := text[0]
	text = text[1 : len(text)-1]
	var result strings.Builder
	for text != "" {
		if text[0] == '\n' || text[0] == '\r' {
			return "", fmt.Errorf("unescaped newline in string")
		}
		if len(text) >= 2 && text[0] == '\\' && (text[1] == '\'' || text[1] == '"' || text[1] == '*') {
			result.WriteByte(text[1])
			text = text[2:]
			continue
		}
		value, multibyte, tail, err := strconv.UnquoteChar(text, quote)
		if err != nil {
			return "", err
		}
		if multibyte {
			result.WriteRune(value)
		} else {
			result.WriteByte(byte(value))
		}
		text = tail
	}
	return result.String(), nil
}

func (p *parser) composite() (*expression, error) {
	lparen, err := p.accept(kindLParen)
	if err != nil {
		return nil, err
	}
	if lparen == nil {
		return nil, nil
	}
	e, err := p.expression()
	if err != nil {
		return nil, err
	}
	if e == nil {
		return nil, fmt.Errorf("expected expression")
	}
	return e, p.expect(kindRParen)
}

func (p *parser) arg() (*arg, error) {
	m, ok, err := p.member()
	if err != nil {
		return nil, err
	}
	if ok {
		return &arg{Member: m}, nil
	}
	composite, err := p.composite()
	if err != nil {
		return nil, err
	}
	if composite != nil {
		return &arg{Composite: composite}, nil
	}
	return nil, nil
}
