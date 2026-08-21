package aip160

import (
	"strconv"
	"strings"
	"testing"

	"github.com/alecthomas/assert/v2"
)

type errorFunc func(testing.TB, error, ...any)

func TestParseFilter(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr errorFunc
	}{
		{name: "empty", input: "", want: "filter{}", wantErr: assert.NoError},
		{
			name:    "restriction",
			input:   "a = 1",
			want:    `filter{expression{sequence{factor{term{simple{restriction{comparable{member{value{"a"}}},"=",arg{comparable{member{value{"1"}}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		{
			name:    "global restriction",
			input:   "chair",
			want:    `filter{expression{sequence{factor{term{simple{restriction{comparable{member{value{"chair"}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		// The two rows below cover the modifications this copy makes; see the
		// header of filter_parser.go.
		{
			name:    "single rune single quoted string",
			input:   "a = 'x'",
			want:    `filter{expression{sequence{factor{term{simple{restriction{comparable{member{value{"a"}}},"=",arg{comparable{member{value{quoted,"x"}}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		{
			name:    "negative number is not a negation",
			input:   "a > -30",
			want:    `filter{expression{sequence{factor{term{simple{restriction{comparable{member{value{"a"}}},">",arg{comparable{member{value{"-30"}}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		{
			name:    "negation of a restriction",
			input:   "-a = 1",
			want:    `filter{expression{sequence{factor{term{-simple{restriction{comparable{member{value{"a"}}},"=",arg{comparable{member{value{"1"}}}}}}}}}}}`,
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
		{name: "escaped wildcard is not a Go escape", input: `a = "x\*y"`, wantErr: assert.Error},
		{name: "multi rune single quoted string", input: "a = 'xy'", wantErr: assert.Error},
		{
			// A lone quote is not a string, and the TEXT class accepts it, so it
			// reads as text rather than failing.
			name:    "unterminated quote is text",
			input:   `a = '`,
			want:    `filter{expression{sequence{factor{term{simple{restriction{comparable{member{value{"a"}}},"=",arg{comparable{member{value{"'"}}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseFilter(test.input)
			test.wantErr(t, err)
			if err != nil {
				return
			}
			assert.Equal(t, test.want, got.String())
		})
	}
}

func TestMember(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		input      string
		wantPath   string
		wantQuoted bool
	}{
		{name: "identifier", input: "name", wantPath: "name"},
		{name: "dotted", input: "user.name", wantPath: "user.name"},
		{name: "number split on the dot", input: "1.5", wantPath: "1.5"},
		{name: "quoted", input: `"a b"`, wantPath: "a b", wantQuoted: true},
		{name: "quoted field", input: `user."odd name"`, wantPath: "user.odd name", wantQuoted: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			f, err := ParseFilter(test.input)
			assert.NoError(t, err)
			member := f.Expression.Sequences[0].Factors[0].Terms[0].Simple.Restriction.Comparable.Member
			assert.Equal(t, test.wantPath, member.Path())
			assert.Equal(t, test.wantQuoted, member.Quoted())
		})
	}
}

// render walks a parsed filter and returns a compact form of the tree: the
// grouping the parser chose, made explicit. TestParseFilter asserts the exact
// node rendering; this asserts how the input was grouped.
func render(f *Filter) string {
	if f == nil || f.Expression == nil {
		return ""
	}
	return renderExpression(f.Expression)
}

func renderExpression(e *Expression) string {
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

func renderTerm(t *Term) string {
	var b strings.Builder
	if t.Negated {
		b.WriteString("-")
	}
	switch {
	case t.Simple.Composite != nil:
		b.WriteString(renderExpression(t.Simple.Composite))
	case t.Simple.Restriction != nil:
		r := t.Simple.Restriction
		b.WriteString(renderMember(r.Comparable.Member))
		if r.Comparator != "" {
			b.WriteString(" " + r.Comparator + " ")
			if r.Arg.Composite != nil {
				b.WriteString(renderExpression(r.Arg.Composite))
			} else {
				b.WriteString(renderMember(r.Arg.Comparable.Member))
			}
		}
	}
	return b.String()
}

// renderMember quotes a member that was quoted in the source, so a row can tell
// the bare identifier active apart from the string "active".
func renderMember(m *Member) string {
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

func TestParseFilter_Grouping(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr errorFunc
	}{
		{name: "empty", input: " \n\t", want: "", wantErr: assert.NoError},
		{name: "comparison", input: "price >= 12.5", want: "price >= 12.5", wantErr: assert.NoError},
		{
			name:    "all comparison operators",
			input:   "a != 1 AND b < 2 AND c <= 3 AND d > 4",
			want:    "(a != 1 AND b < 2 AND c <= 3 AND d > 4)",
			wantErr: assert.NoError,
		},
		{name: "minimum int64", input: "value = -9223372036854775808", want: "value = -9223372036854775808", wantErr: assert.NoError},
		{name: "maximum uint64", input: "value = 18446744073709551615", want: "value = 18446744073709551615", wantErr: assert.NoError},
		{name: "negative number is not a negation", input: "total > -30", want: "total > -30", wantErr: assert.NoError},
		{name: "escaped unicode string", input: `name = "line\n世界"`, want: `name = "line\n世界"`, wantErr: assert.NoError},
		{name: "escaped quote", input: `"a\"b"`, want: `"a\"b"`, wantErr: assert.NoError},
		{name: "multi rune single quoted literal", input: `'it\'s'`, wantErr: assert.Error},
		{name: "double quote inside single quotes", input: `'a"b'`, wantErr: assert.Error},
		{name: "escaped wildcard is not a Go escape", input: `name = "a\*b"`, wantErr: assert.Error},
		{name: "quoted keyword is text", input: `"and"`, want: `"and"`, wantErr: assert.NoError},
		{name: "aip precedence", input: "a = 1 OR b = 2 AND c = 3", want: "((a = 1 OR b = 2) AND c = 3)", wantErr: assert.NoError},
		{name: "sequence", input: `"blue" "chair"`, want: `("blue" "chair")`, wantErr: assert.NoError},
		{name: "word text", input: "chair", want: "chair", wantErr: assert.NoError},
		{name: "negation", input: "-deleted = true", want: "-deleted = true", wantErr: assert.NoError},
		{
			name:    "not and literal values",
			input:   "NOT (active = false OR deleted = null)",
			want:    "-(active = false OR deleted = null)",
			wantErr: assert.NoError,
		},
		{name: "field value", input: "state = active", want: "state = active", wantErr: assert.NoError},
		{name: "parenthesized", input: "(a = 1)", want: "a = 1", wantErr: assert.NoError},
		{name: "trailing whitespace", input: "a ", want: "a", wantErr: assert.NoError},
		{name: "terminal wildcard", input: `labels.* = "value"`, want: `labels.* = "value"`, wantErr: assert.NoError},
		{name: "exponent", input: "1e+2", want: "1e+2", wantErr: assert.NoError},
		{name: "has", input: `tags:"blue"`, want: `tags : "blue"`, wantErr: assert.NoError},
		{name: "has bare identifier", input: "tags:blue", want: "tags : blue", wantErr: assert.NoError},
		{name: "has dotted path", input: `metadata.tags:"blue"`, want: `metadata.tags : "blue"`, wantErr: assert.NoError},
		{name: "negated has", input: `NOT tags:"blue"`, want: `-tags : "blue"`, wantErr: assert.NoError},
		{name: "question mark escape is not a Go escape", input: `'a\?b'`, wantErr: assert.Error},
		// Values carry no type until a schema reads them, so a filter the old
		// parser rejected while converting a literal now parses and is refused
		// by the schema instead.
		{name: "integer overflow", input: "value = 18446744073709551616", want: "value = 18446744073709551616", wantErr: assert.NoError},
		{name: "malformed number", input: "value = 1e", want: "value = 1e", wantErr: assert.NoError},
		{name: "lowercase and is a sequence", input: "a = 1 and b = 2", want: "(a = 1 and b = 2)", wantErr: assert.NoError},
		{name: "wildcard inside path", input: "labels.*.value = 1", want: "labels.*.value = 1", wantErr: assert.NoError},

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
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseFilter(test.input)
			test.wantErr(t, err)
			if err != nil {
				return
			}
			assert.Equal(t, test.want, render(got))
		})
	}
}

func TestParseFilter_Rendering(t *testing.T) {
	t.Parallel()

	// The rendering a filter reports is what the cursor binding is taken over,
	// so it comes from the tree: two spellings of one filter render alike.
	spaced, err := ParseFilter("  a = 1  ")
	assert.NoError(t, err)
	tight, err := ParseFilter("a=1")
	assert.NoError(t, err)
	assert.Equal(t, spaced.String(), tight.String())

	// A blank filter carries no expression, and renders the same as a nil one.
	empty, err := ParseFilter("   ")
	assert.NoError(t, err)
	assert.Zero(t, empty.Expression)
	assert.Equal(t, (*Filter)(nil).String(), empty.String())
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
