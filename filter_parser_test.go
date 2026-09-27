package flop

import (
	"strconv"
	"strings"
	"testing"

	"github.com/ysomad/flop/internal/assert"
)

func TestParseFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		// want is the node rendering, wantGroup the grouping the parser chose,
		// and wantMsg the exact error text. Each is checked only when set.
		want      string
		wantGroup string
		wantMsg   string
		wantErr   assert.ErrorFunc
	}{
		{name: "empty", input: "", want: "filter{}", wantErr: assert.NoError},
		{
			name:    "restriction",
			input:   "a = 1",
			want:    `filter{expression{sequence{factor{term{simple{restriction{member{value{"a"}},"=",arg{member{value{"1"}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		{
			name:    "global restriction",
			input:   "chair",
			want:    `filter{expression{sequence{factor{term{simple{restriction{member{value{"chair"}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		// The two rows below cover the modifications this copy makes; see the
		// header of filter_parser.go.
		{
			name:    "single rune single quoted string",
			input:   "a = 'x'",
			want:    `filter{expression{sequence{factor{term{simple{restriction{member{value{"a"}},"=",arg{member{value{quoted,"x"}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		{
			name:    "negative number is not a negation",
			input:   "a > -30",
			want:    `filter{expression{sequence{factor{term{simple{restriction{member{value{"a"}},">",arg{member{value{"-30"}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		{
			name:    "negation of a restriction",
			input:   "-a = 1",
			want:    `filter{expression{sequence{factor{term{-simple{restriction{member{value{"a"}},"=",arg{member{value{"1"}}}}}}}}}}`,
			wantErr: assert.NoError,
		},

		// Every alternative of the lexer pattern is anchored, so input the
		// lexer cannot read is reported instead of matching further along.
		{name: "unlexable character", input: "a ! b", wantErr: assert.Error},
		{name: "unlexable in an argument", input: "a = !", wantErr: assert.Error},

		{name: "missing argument", input: "a = ", wantErr: assert.Error},
		{name: "missing right operand", input: "a = 1 AND ", wantErr: assert.Error},
		{name: "negation without a term", input: "NOT ", wantErr: assert.Error},
		{name: "unbalanced parenthesis", input: "(a = 1", wantErr: assert.Error},
		{name: "empty composite", input: "()", wantErr: assert.Error},
		{name: "trailing token", input: "a)", wantErr: assert.Error},
		{name: "dot without a field", input: "a. = 1", wantErr: assert.Error},
		{name: "invalid escape", input: `a = "\q"`, wantErr: assert.Error},

		{name: "unterminated quote", input: `a = '`, wantErr: assert.Error},
		{name: "empty", input: " \n\t", wantGroup: "", wantErr: assert.NoError},
		{name: "comparison", input: "price >= 12.5", wantGroup: "price >= 12.5", wantErr: assert.NoError},
		{
			name:      "all comparison operators",
			input:     "a != 1 AND b < 2 AND c <= 3 AND d > 4",
			wantGroup: "(a != 1 AND b < 2 AND c <= 3 AND d > 4)",
			wantErr:   assert.NoError,
		},
		{name: "minimum int64", input: "value = -9223372036854775808", wantGroup: "value = -9223372036854775808", wantErr: assert.NoError},
		{name: "maximum uint64", input: "value = 18446744073709551615", wantGroup: "value = 18446744073709551615", wantErr: assert.NoError},
		{name: "negative number is not a negation", input: "total > -30", wantGroup: "total > -30", wantErr: assert.NoError},
		{name: "escaped unicode string", input: `name = "line\n世界"`, wantGroup: `name = "line\n世界"`, wantErr: assert.NoError},
		{name: "escaped quote", input: `"a\"b"`, wantGroup: `"a\"b"`, wantErr: assert.NoError},
		{name: "multi rune single quoted literal", input: `'it\'s'`, wantGroup: `"it's"`, wantErr: assert.NoError},
		{name: "double quote inside single quotes", input: `'a"b'`, wantGroup: `"a\"b"`, wantErr: assert.NoError},
		{name: "escaped wildcard", input: `name = "a\*b"`, wantGroup: `name = "a*b"`, wantErr: assert.NoError},
		{name: "quoted keyword is text", input: `"and"`, wantGroup: `"and"`, wantErr: assert.NoError},
		{name: "aip precedence", input: "a = 1 OR b = 2 AND c = 3", wantGroup: "((a = 1 OR b = 2) AND c = 3)", wantErr: assert.NoError},
		{name: "sequence", input: `"blue" "chair"`, wantGroup: `("blue" "chair")`, wantErr: assert.NoError},
		{name: "word text", input: "chair", wantGroup: "chair", wantErr: assert.NoError},
		{name: "negation", input: "-deleted = true", wantGroup: "-deleted = true", wantErr: assert.NoError},
		{
			name:      "not and literal values",
			input:     "NOT (active = false OR deleted = null)",
			wantGroup: "-(active = false OR deleted = null)",
			wantErr:   assert.NoError,
		},
		{name: "field value", input: "state = active", wantGroup: "state = active", wantErr: assert.NoError},
		{name: "parenthesized", input: "(a = 1)", wantGroup: "a = 1", wantErr: assert.NoError},
		{name: "trailing whitespace", input: "a ", wantGroup: "a", wantErr: assert.NoError},
		{name: "terminal wildcard", input: `labels.* = "value"`, wantGroup: `labels.* = "value"`, wantErr: assert.NoError},
		{name: "exponent", input: "1e+2", wantGroup: "1e+2", wantErr: assert.NoError},
		{name: "has", input: `tags:"blue"`, wantGroup: `tags : "blue"`, wantErr: assert.NoError},
		{name: "has bare identifier", input: "tags:blue", wantGroup: "tags : blue", wantErr: assert.NoError},
		{name: "has dotted path", input: `metadata.tags:"blue"`, wantGroup: `metadata.tags : "blue"`, wantErr: assert.NoError},
		{name: "negated has", input: `NOT tags:"blue"`, wantGroup: `-tags : "blue"`, wantErr: assert.NoError},
		{name: "question mark escape is not a Go escape", input: `'a\?b'`, wantErr: assert.Error},
		// Values carry no type until a schema reads them, so a filter the old
		// parser rejected while converting a literal now parses and is refused
		// by the schema instead.
		{name: "integer overflow", input: "value = 18446744073709551616", wantGroup: "value = 18446744073709551616", wantErr: assert.NoError},
		{name: "malformed number", input: "value = 1e", wantGroup: "value = 1e", wantErr: assert.NoError},
		{name: "lowercase and is a sequence", input: "a = 1 and b = 2", wantGroup: "(a = 1 and b = 2)", wantErr: assert.NoError},
		{name: "wildcard inside path", input: "labels.*.value = 1", wantGroup: "labels.*.value = 1", wantErr: assert.NoError},

		{name: "double negation is not a term", input: `NOT NOT name = "x"`, wantErr: assert.Error},
		{name: "function call syntax", input: "distance(location, point) < 10", wantErr: assert.Error},
		{name: "comma where a value is expected", input: "age = ,", wantErr: assert.Error},
		{name: "leading comma", input: ",", wantErr: assert.Error},
		{name: "comma after a complete expression", input: "a = 1, b = 2", wantErr: assert.Error},
		{name: "invalid operator", input: "active ! true", wantErr: assert.Error},
		{name: "invalid string escape", input: `name = "\q"`, wantErr: assert.Error},
		{name: "unbalanced composite", input: "(a = 1", wantErr: assert.Error},
		{name: "empty composite", input: "()", wantErr: assert.Error},
		{name: "not without operand", input: "NOT ", wantErr: assert.Error},
		{name: "minus without operand", input: "-", wantErr: assert.Error},
		{name: "and without right operand", input: "a = 1 AND ", wantErr: assert.Error},
		{name: "and before invalid operand", input: "a = 1 AND )", wantErr: assert.Error},
		{name: "sequence before invalid operand", input: "a (", wantErr: assert.Error},
		{name: "or without right operand", input: "a = 1 OR ", wantErr: assert.Error},
		{name: "or before invalid operand", input: "a = 1 OR )", wantErr: assert.Error},
		{name: "unexpected closing parenthesis", input: "a)", wantErr: assert.Error},
		{name: "invalid traversal", input: "a. = 1", wantErr: assert.Error},

		// Whitespace is significant between the factors of a sequence, around
		// AND and OR, and after a negating '-', which abuts what it negates.
		{name: "detached negation", input: "- 30", wantErr: assert.Error},
		{name: "detached negation of a restriction", input: "- a = 1", wantErr: assert.Error},
		{name: "detached negation of a composite", input: "- (a = 1)", wantErr: assert.Error},
		{name: "whitespace around a traversal", input: "a . b", wantErr: assert.Error},
		{name: "whitespace after a traversal", input: "a. b", wantErr: assert.Error},
		{name: "whitespace before a traversal", input: "a .b", wantErr: assert.Error},
		{name: "number split by whitespace", input: "value = 1 .5", wantErr: assert.Error},
		{name: "factors without whitespace", input: `"a"b`, wantErr: assert.Error},
		{name: "composites without whitespace", input: "(a)(b)", wantErr: assert.Error},
		{name: "composite without whitespace", input: "a(b)", wantErr: assert.Error},
		{name: "and without whitespace before it", input: "(a)AND b", wantErr: assert.Error},
		{name: "or without whitespace before it", input: `"x"OR y`, wantErr: assert.Error},
		{name: "negation without whitespace before it", input: "(a)NOT b", wantErr: assert.Error},

		{name: "attached negation", input: "-30", wantGroup: "-30", wantErr: assert.NoError},
		{name: "duration", input: "wait < 2h", wantGroup: "wait < 2h", wantErr: assert.NoError},
		{name: "traversal", input: "a.b = 1", wantGroup: "a.b = 1", wantErr: assert.NoError},
		{name: "and between composites", input: "(a) AND (b)", wantGroup: "(a AND b)", wantErr: assert.NoError},

		// Two spellings of one filter parse to one tree, which is what a cursor
		// binding taken over the rendering rests on.
		{name: "padded restriction", input: "  a = 1  ", want: `filter{expression{sequence{factor{term{simple{restriction{member{value{"a"}},"=",arg{member{value{"1"}}}}}}}}}}`, wantErr: assert.NoError},
		{name: "tight restriction", input: "a=1", want: `filter{expression{sequence{factor{term{simple{restriction{member{value{"a"}},"=",arg{member{value{"1"}}}}}}}}}}`, wantErr: assert.NoError},

		{name: "dangling and", input: "a AND", wantMsg: "expected sequence after AND", wantErr: assert.Error},
		{name: "dangling or", input: "a OR", wantMsg: "expected term after OR", wantErr: assert.Error},
		{name: "dangling or with trailing space", input: "a OR ", wantMsg: "expected term after OR", wantErr: assert.Error},
		{
			name:    "dangling not",
			input:   "NOT",
			wantMsg: `expected simple term after negation "NOT"`,
			wantErr: assert.Error,
		},

		{name: "unterminated single quote", input: `name = 'hello`, wantErr: assert.Error},
		{name: "unterminated double quote", input: `name = "hello`, wantErr: assert.Error},
		{name: "escape at the end of a single quoted body", input: `name = 'hello\`, wantErr: assert.Error},
		{name: "escape at the end of a double quoted body", input: `name = "hello\`, wantErr: assert.Error},
		{name: "invalid escape in a single quoted body", input: `name = '\q'`, wantErr: assert.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseFilter(test.input)
			test.wantErr(t, err)
			if err != nil {
				if test.wantMsg != "" {
					assert.Equal(t, test.wantMsg, err.Error())
				}
				return
			}
			if test.want != "" {
				assert.Equal(t, test.want, got.String())
			}
			if test.wantGroup != "" || got.expression == nil {
				assert.Equal(t, test.wantGroup, render(got))
			}
			if got.expression == nil {
				// A filter carrying no expression renders as a nil one does.
				assert.Equal(t, (*Filter)(nil).String(), got.String())
			}
		})
	}
}

// render walks a parsed filter and returns a compact form of the tree: the
// grouping the parser chose, made explicit. TestParseFilter asserts the exact
// node rendering; this asserts how the input was grouped.
func render(f *Filter) string {
	if f == nil || f.expression == nil {
		return ""
	}
	return renderExpression(f.expression)
}

func renderExpression(e *expression) string {
	parts := make([]string, 0, len(e.Sequences))
	for _, sequence := range e.Sequences {
		factors := make([]string, 0, len(sequence.Factors))
		for _, factor := range sequence.Factors {
			terms := make([]string, 0, len(factor.Terms))
			for _, term := range factor.Terms {
				terms = append(terms, renderTerm(term))
			}
			factors = append(factors, group(terms, " OR "))
		}
		parts = append(parts, group(factors, " "))
	}
	return group(parts, " AND ")
}

func renderTerm(t term) string {
	var b strings.Builder
	if t.Negated {
		b.WriteString("-")
	}
	switch {
	case t.Simple.Composite != nil:
		b.WriteString(renderExpression(t.Simple.Composite))
	case t.Simple.Restriction != nil:
		r := t.Simple.Restriction
		b.WriteString(renderMember(r.Member))
		if r.Comparator != "" {
			b.WriteString(" " + r.Comparator + " ")
			if r.Arg.Composite != nil {
				b.WriteString(renderExpression(r.Arg.Composite))
			} else {
				b.WriteString(renderMember(r.Arg.Member))
			}
		}
	}
	return b.String()
}

// renderMember quotes a member that was quoted in the source, so a row can tell
// the bare identifier active apart from the string "active".
func renderMember(m member) string {
	if m.Quoted() {
		return strconv.Quote(m.Path())
	}
	return m.Path()
}

func group(parts []string, sep string) string {
	if len(parts) == 1 {
		return parts[0]
	}
	return "(" + strings.Join(parts, sep) + ")"
}

func Test_member_Path(t *testing.T) {
	t.Parallel()
	const unicode = "Hello 世界 "
	tests := []struct {
		name  string
		input string
		// fromArg reads the argument's member instead of the restriction's.
		fromArg bool
		want    string
	}{
		{name: "identifier", input: "name", want: "name"},
		{name: "dotted", input: "user.name", want: "user.name"},
		{name: "number split on the dot", input: "1.5", want: "1.5"},
		{name: "quoted", input: `"a b"`, want: "a b"},
		{name: "quoted field", input: `user."odd name"`, want: "user.odd name"},

		{name: "empty double quoted body", input: `name = ""`, fromArg: true, want: ""},
		{name: "empty single quoted body", input: "name = ''", fromArg: true, want: ""},
		{
			name:    "long unicode double quoted body",
			input:   `name = "` + strings.Repeat(unicode, 1000) + `"`,
			fromArg: true,
			want:    strings.Repeat(unicode, 1000),
		},
		{
			name:    "long unicode single quoted body",
			input:   "name = '" + strings.Repeat(unicode, 1000) + "'",
			fromArg: true,
			want:    strings.Repeat(unicode, 1000),
		},
		{
			name:    "escapes in a double quoted body",
			input:   `name = "line\n\t\r\\\u4e16\x41"`,
			fromArg: true,
			want:    "line\n\t\r\\世A",
		},
		{
			name:    "escapes in a single quoted body",
			input:   `name = 'line\n\t\r\\\u4e16\x41'`,
			fromArg: true,
			want:    "line\n\t\r\\世A",
		},
		{name: "both quotes escaped in a double quoted body", input: `name = "\'\""`, fromArg: true, want: `'"`},
		{name: "both quotes escaped in a single quoted body", input: `name = '\'\"'`, fromArg: true, want: `'"`},
		{name: "escaped wildcard in a double quoted body", input: `name = "a\*b"`, fromArg: true, want: "a*b"},
		{name: "escaped wildcard in a single quoted body", input: `name = 'a\*b'`, fromArg: true, want: "a*b"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, testMember(t, test.input, test.fromArg).Path())
		})
	}
}

func Test_member_Quoted(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		fromArg bool
		want    bool
	}{
		{name: "identifier", input: "name"},
		{name: "dotted", input: "user.name"},
		{name: "number split on the dot", input: "1.5"},
		{name: "quoted", input: `"a b"`, want: true},
		{name: "quoted field", input: `user."odd name"`, want: true},
		{name: "double quoted argument", input: `name = "bob"`, fromArg: true, want: true},
		{name: "single quoted argument", input: "name = 'bob'", fromArg: true, want: true},
		{name: "bare argument", input: "name = bob", fromArg: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, testMember(t, test.input, test.fromArg).Quoted())
		})
	}
}

// testMember parses one restriction and hands back the member a row addresses.
func testMember(t *testing.T, input string, fromArg bool) member {
	t.Helper()
	filter, err := ParseFilter(input)
	assert.NoError(t, err)
	restriction := filter.expression.Sequences[0].Factors[0].Terms[0].Simple.Restriction
	if fromArg {
		return restriction.Arg.Member
	}
	return restriction.Member
}

// FuzzParseFilter checks that no input panics, that a filter that parses is
// never nil, and that the rendering a filter reports is stable across parses.
// The last one is what a cursor binding rests on: the same filter replayed must
// produce the same fingerprint.
func FuzzParseFilter(f *testing.F) {
	seeds := []string{
		"",
		"a = 1",
		`name = "book*"`,
		"a = 1 OR b = 2 AND c = 3",
		`NOT (active = false OR deleted = null)`,
		`"blue" "chair"`,
		`tags:"blue"`,
		"labels.* = 1",
		"1e+2",
		"-9223372036854775808",
		"18446744073709551615",
		"wait < 2h",
		`'it\'s'`,
		"- 30",
		"(a)AND b",
		strings.Repeat("(", 70) + "a" + strings.Repeat(")", 70),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	f.Fuzz(func(t *testing.T, input string) {
		parsed, err := ParseFilter(input)
		if err != nil {
			return
		}
		if parsed == nil {
			t.Fatalf("filter %q parsed to nothing without reporting an error", input)
		}

		reparsed, err := ParseFilter(input)
		if err != nil {
			t.Fatalf("filter %q parsed once but not twice: %v", input, err)
		}
		if reparsed.String() != parsed.String() {
			t.Fatalf("filter rendering is not stable: %q then %q from %q", parsed.String(), reparsed.String(), input)
		}
	})
}
