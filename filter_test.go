package flop

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ysomad/flop/internal/assert"
)

// renderExpr writes a compiled filter as a compact infix string, so a table row
// can state the tree it expects without building one.
func renderExpr(e Expr) string {
	switch node := e.(type) {
	case nil:
		return ""
	case And:
		return joinExprs(node.Exprs, " AND ")
	case Or:
		return joinExprs(node.Exprs, " OR ")
	case Not:
		return "NOT " + renderExpr(node.Expr)
	case Cmp:
		return node.Field.Ref() + " " + node.Op.String() + " " + renderValue(node.Value)
	}
	return fmt.Sprintf("unknown node %T", e)
}

func joinExprs(exprs []Expr, sep string) string {
	parts := make([]string, 0, len(exprs))
	for _, expr := range exprs {
		parts = append(parts, renderExpr(expr))
	}
	return "(" + strings.Join(parts, sep) + ")"
}

func renderValue(v any) string {
	switch value := v.(type) {
	case nil:
		return "null"
	case string:
		return strconv.Quote(value)
	case time.Time:
		return value.Format(time.RFC3339Nano)
	}
	return fmt.Sprintf("%v", v)
}

// filterSchema declares the fields the filter rows name, including the spare
// ordered fields a row needs to compare four operators at once.
func filterSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := NewSchema(
		NewField("id").Ref("u.id").Int().Unique(),
		NewField("display_name").Ref("u.name").String().Filterable().Sortable().Implicit(),
		NewField("created_at").Ref("u.created_at").Time().Filterable().Sortable(),
		NewField("latency").Ref("u.latency").Duration().Filterable().Sortable(),
		NewField("active").Ref("u.active").Bool().Filterable(),
		NewField("rating").Ref("u.rating").Float().Filterable(),
		NewField("age").Ref("u.age").Int().Filterable(),
		NewField("age2").Ref("u.age2").Int().Filterable(),
		NewField("age3").Ref("u.age3").Int().Filterable(),
		NewField("age4").Ref("u.age4").Int().Filterable(),
		NewField("secret").Ref("u.secret").String(),
		NewField("metadata", "tags").Ref("m.tags").String().Filterable(),
	).Build()
	assert.NoError(t, err)
	return s
}

// malformedFilter is an AST no parser produces, so both the compiler and the
// validator have to refuse it.
type malformedFilter struct {
	name   string
	filter *Filter
}

func malformedFilters() []malformedFilter {
	mem := member{Value: value{Value: "display_name"}}
	valid := term{Simple: simple{Restriction: &restriction{Member: mem}}}
	expressionOf := func(t term) *expression {
		return &expression{Sequences: []sequence{{Factors: []factor{{Terms: []term{t}}}}}}
	}
	filterOf := func(e *expression) *Filter { return &Filter{expression: e} }
	restrictionOf := func(r *restriction) *Filter {
		return filterOf(expressionOf(term{Simple: simple{Restriction: r}}))
	}
	return []malformedFilter{
		{name: "empty expression", filter: filterOf(&expression{})},
		{
			name:   "empty sequence beside valid",
			filter: filterOf(&expression{Sequences: append(expressionOf(valid).Sequences, sequence{})}),
		},
		{
			name:   "empty factor",
			filter: filterOf(&expression{Sequences: []sequence{{Factors: []factor{{}}}}}),
		},
		{name: "empty term", filter: filterOf(expressionOf(term{}))},
		{
			name: "both simple alternatives",
			filter: filterOf(expressionOf(term{Simple: simple{
				Restriction: valid.Simple.Restriction,
				Composite:   expressionOf(valid),
			}})),
		},
		{name: "missing argument", filter: restrictionOf(&restriction{Member: mem, Comparator: "="})},
		{
			name:   "argument without comparator",
			filter: restrictionOf(&restriction{Member: mem, Arg: &arg{Member: mem}}),
		},
		{name: "missing member", filter: restrictionOf(&restriction{})},
		{
			name:   "missing argument member",
			filter: restrictionOf(&restriction{Member: mem, Comparator: "=", Arg: &arg{}}),
		},
		{
			name:   "empty member segment",
			filter: restrictionOf(&restriction{Member: member{Value: mem.Value, Fields: []value{{}}}}),
		},
		{
			name:   "unsupported comparator",
			filter: restrictionOf(&restriction{Member: mem, Comparator: "IN", Arg: &arg{Member: mem}}),
		},
		{
			name: "composite argument",
			filter: restrictionOf(&restriction{
				Member:     mem,
				Comparator: "=",
				Arg:        &arg{Composite: expressionOf(valid)},
			}),
		},
	}
}

func TestSchema_ParseFilter(t *testing.T) {
	t.Parallel()
	type args struct {
		text string
	}
	tests := []struct {
		name         string
		schema       *Schema
		args         args
		want         string
		wantContains string
		wantSentinel error
		wantErr      assert.ErrorFunc
	}{
		{name: "empty", args: args{text: ""}, want: "filter{}", wantErr: assert.NoError},
		{name: "blank", args: args{text: "  "}, want: "filter{}", wantErr: assert.NoError},
		{
			name: "restriction",
			args: args{text: `display_name = "bob"`},
			want: `filter{expression{sequence{factor{term{simple{restriction{member{value{"display_name"}},` +
				`"=",arg{member{value{quoted,"bob"}}}}}}}}}}`,
			wantErr: assert.NoError,
		},
		{
			name:    "implicit search",
			args:    args{text: "bob"},
			want:    `filter{expression{sequence{factor{term{simple{restriction{member{value{"bob"}}}}}}}}}`,
			wantErr: assert.NoError,
		},

		{
			name: "no implicit field to search",
			schema: NewSchema(
				NewField("name").Ref("u.name").String().Filterable(),
			).MustBuild(),
			args:    args{text: "bob"},
			wantErr: assert.Error,
		},
		{name: "undeclared field", args: args{text: "nope = 1"}, wantErr: assert.Error},
		{name: "declared but not filterable", args: args{text: `secret = "x"`}, wantErr: assert.Error},
		{name: "syntax error", args: args{text: "a = "}, wantErr: assert.Error},
		{name: "has on an int", args: args{text: "age:30"}, wantErr: assert.Error},
		{name: "ordering on a string", args: args{text: `display_name < "bob"`}, wantErr: assert.Error},
		{name: "ordering on a bool", args: args{text: "active > true"}, wantErr: assert.Error},
		{name: "int takes no text", args: args{text: "age = old"}, wantErr: assert.Error},
		{name: "int takes no quoted number", args: args{text: `age = "30"`}, wantErr: assert.Error},
		{name: "int overflow", args: args{text: "age = 18446744073709551616"}, wantErr: assert.Error},
		{name: "float takes no text", args: args{text: "rating = high"}, wantErr: assert.Error},
		{name: "float is finite", args: args{text: "rating = Inf"}, wantErr: assert.Error},
		{name: "bool is case sensitive", args: args{text: "active = TRUE"}, wantErr: assert.Error},
		{name: "bool takes no quoted literal", args: args{text: `active = "true"`}, wantErr: assert.Error},
		{name: "time is rfc 3339", args: args{text: `created_at > "yesterday"`}, wantErr: assert.Error},
		{name: "duration needs a unit", args: args{text: "latency = 250"}, wantErr: assert.Error},
		{name: "duration rejects text", args: args{text: "latency = later"}, wantErr: assert.Error},
		{name: "duration has no day unit", args: args{text: "latency = 1d"}, wantErr: assert.Error},
		{
			name:    "duration overflow",
			args:    args{text: "latency = 999999999999999999999999h"},
			wantErr: assert.Error,
		},
		{name: "has on a duration", args: args{text: "latency:1s"}, wantErr: assert.Error},
		{name: "duration null is not ordered", args: args{text: "latency > null"}, wantErr: assert.Error},
		{name: "null is not ordered", args: args{text: "created_at > null"}, wantErr: assert.Error},
		{name: "composite argument", args: args{text: "age = (1 OR 2)"}, wantErr: assert.Error},
		{
			// Rejecting a wildcard has to reach numeric coercion, not stop at
			// the pattern the wildcard implies.
			name:         "wildcard against a non string field",
			args:         args{text: "rating = 5*"},
			wantContains: "takes a number",
			wantSentinel: ErrInvalidFilter,
			wantErr:      assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := test.schema
			if s == nil {
				s = filterSchema(t)
			}
			got, gotErr := s.ParseFilter(test.args.text)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				if test.wantSentinel != nil {
					assert.IsError(t, gotErr, test.wantSentinel)
				}
				if test.wantContains != "" && !strings.Contains(gotErr.Error(), test.wantContains) {
					t.Fatalf("error %q does not mention %q", gotErr, test.wantContains)
				}
				return
			}
			assert.Equal(t, test.want, got.String())
		})
	}
}

func TestSchema_ValidateFilter(t *testing.T) {
	t.Parallel()
	schema := testSchema(t)
	valid, err := schema.ParseFilter(`display_name = "bob"`)
	assert.NoError(t, err)

	type args struct {
		filter *Filter
	}
	tests := []struct {
		name    string
		args    args
		wantErr assert.ErrorFunc
	}{
		{name: "nil filter", args: args{}, wantErr: assert.NoError},
		{name: "parsed filter", args: args{filter: valid}, wantErr: assert.NoError},
	}
	for _, malformed := range malformedFilters() {
		tests = append(tests, struct {
			name    string
			args    args
			wantErr assert.ErrorFunc
		}{name: malformed.name, args: args{filter: malformed.filter}, wantErr: assert.Error})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			gotErr := schema.ValidateFilter(test.args.filter)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				assert.IsError(t, gotErr, ErrInvalidFilter)
			}
		})
	}
}

func TestSchema_CompileFilter(t *testing.T) {
	t.Parallel()
	type args struct {
		text   string
		filter *Filter
	}
	tests := []struct {
		name    string
		schema  *Schema
		args    args
		want    string
		wantErr assert.ErrorFunc
	}{
		{name: "empty", args: args{text: ""}, want: "", wantErr: assert.NoError},
		{name: "blank", args: args{text: "  "}, want: "", wantErr: assert.NoError},
		{
			name:    "string equality",
			args:    args{text: `display_name = "bob"`},
			want:    `u.name = "bob"`,
			wantErr: assert.NoError,
		},
		{
			name:    "string inequality",
			args:    args{text: `display_name != "bob"`},
			want:    `u.name != "bob"`,
			wantErr: assert.NoError,
		},
		{
			name:    "unquoted string argument",
			args:    args{text: "display_name = bob"},
			want:    `u.name = "bob"`,
			wantErr: assert.NoError,
		},
		{
			name:    "has compiles to a substring pattern",
			args:    args{text: `display_name:"bob"`},
			want:    `u.name LIKE "%bob%"`,
			wantErr: assert.NoError,
		},
		{
			// A value cannot smuggle its own wildcards into the pattern.
			name:    "has escapes the pattern metacharacters",
			args:    args{text: `display_name:"100%_a\\b"`},
			want:    `u.name LIKE "%100\\%\\_a\\\\b%"`,
			wantErr: assert.NoError,
		},
		{
			// A wildcard the client wrote wins over the %...% the has operator
			// searches with by default.
			name:    "has with a trailing wildcard",
			args:    args{text: `display_name:"bo*"`},
			want:    `u.name LIKE "bo%"`,
			wantErr: assert.NoError,
		},
		{
			name:    "equality with wildcards is a pattern",
			args:    args{text: `display_name = "*bo*"`},
			want:    `u.name LIKE "%bo%"`,
			wantErr: assert.NoError,
		},
		{
			name:    "equality without a wildcard stays exact",
			args:    args{text: `display_name = "bo"`},
			want:    `u.name = "bo"`,
			wantErr: assert.NoError,
		},
		{
			name:    "inequality with wildcards negates the pattern",
			args:    args{text: `display_name != "bo*"`},
			want:    `NOT u.name LIKE "bo%"`,
			wantErr: assert.NoError,
		},
		{
			name:    "interior wildcard",
			args:    args{text: `display_name = "b*o"`},
			want:    `u.name LIKE "b%o"`,
			wantErr: assert.NoError,
		},
		{
			name:    "a lone wildcard matches everything",
			args:    args{text: `display_name = "*"`},
			want:    `u.name LIKE "%"`,
			wantErr: assert.NoError,
		},
		{
			// The client's own percent stays escaped beside the wildcard it
			// wrote, so it matches a literal percent.
			name:    "wildcard beside an escaped percent",
			args:    args{text: `display_name = "50%*"`},
			want:    `u.name LIKE "50\\%%"`,
			wantErr: assert.NoError,
		},
		{
			name:    "implicit search honours a wildcard",
			args:    args{text: `"bo*"`},
			want:    `u.name LIKE "bo%"`,
			wantErr: assert.NoError,
		},
		{
			name:    "multi segment path",
			args:    args{text: `metadata.tags = "blue"`},
			want:    `m.tags = "blue"`,
			wantErr: assert.NoError,
		},
		{name: "int", args: args{text: "age = 30"}, want: "u.age = 30", wantErr: assert.NoError},
		{
			name:    "negative int",
			args:    args{text: "age > -30"},
			want:    "u.age > -30",
			wantErr: assert.NoError,
		},
		{
			name:    "all ordered comparators",
			args:    args{text: "age < 1 AND age2 <= 2 AND age3 > 3 AND age4 >= 4"},
			want:    "(u.age < 1 AND u.age2 <= 2 AND u.age3 > 3 AND u.age4 >= 4)",
			wantErr: assert.NoError,
		},
		{name: "float", args: args{text: "rating >= 4.5"}, want: "u.rating >= 4.5", wantErr: assert.NoError},
		{name: "bool true", args: args{text: "active = true"}, want: "u.active = true", wantErr: assert.NoError},
		{name: "bool false", args: args{text: "active = false"}, want: "u.active = false", wantErr: assert.NoError},
		{
			name:    "time",
			args:    args{text: `created_at > "2026-08-15T09:00:00Z"`},
			want:    "u.created_at > 2026-08-15T09:00:00Z",
			wantErr: assert.NoError,
		},
		{
			name:    "duration",
			args:    args{text: "latency < 250ms"},
			want:    "u.latency < 250ms",
			wantErr: assert.NoError,
		},
		{
			name:    "quoted fractional duration",
			args:    args{text: `latency = "1.5s"`},
			want:    "u.latency = 1.5s",
			wantErr: assert.NoError,
		},
		{
			name:    "compound duration",
			args:    args{text: "latency >= 2h30m"},
			want:    "u.latency >= 2h30m0s",
			wantErr: assert.NoError,
		},
		{
			name:    "zero duration",
			args:    args{text: "latency = 0s"},
			want:    "u.latency = 0s",
			wantErr: assert.NoError,
		},
		{
			name:    "negative duration",
			args:    args{text: "latency > -1.5s"},
			want:    "u.latency > -1.5s",
			wantErr: assert.NoError,
		},
		{
			name: "all duration comparators",
			args: args{text: "latency = 1s AND latency != 2s AND latency < 3s" +
				" AND latency <= 4s AND latency > 5s AND latency >= 6s"},
			want: "(u.latency = 1s AND u.latency != 2s AND u.latency < 3s" +
				" AND u.latency <= 4s AND u.latency > 5s AND u.latency >= 6s)",
			wantErr: assert.NoError,
		},
		{
			name:    "duration null",
			args:    args{text: "latency = null"},
			want:    "u.latency = null",
			wantErr: assert.NoError,
		},
		{
			name:    "duration not null",
			args:    args{text: "latency != null"},
			want:    "u.latency != null",
			wantErr: assert.NoError,
		},
		{
			name:    "null",
			args:    args{text: "created_at = null"},
			want:    "u.created_at = null",
			wantErr: assert.NoError,
		},
		{
			name:    "not null",
			args:    args{text: "created_at != null"},
			want:    "u.created_at != null",
			wantErr: assert.NoError,
		},
		{
			name:    "conjunction",
			args:    args{text: "active = true AND age = 30"},
			want:    "(u.active = true AND u.age = 30)",
			wantErr: assert.NoError,
		},
		{
			// A sequence means the same as AND once matching is exact.
			name:    "sequence is a conjunction",
			args:    args{text: "active = true age = 30"},
			want:    "(u.active = true AND u.age = 30)",
			wantErr: assert.NoError,
		},
		{
			name:    "disjunction",
			args:    args{text: "age = 30 OR age = 40"},
			want:    "(u.age = 30 OR u.age = 40)",
			wantErr: assert.NoError,
		},
		{
			name:    "aip precedence",
			args:    args{text: "age = 1 OR age2 = 2 AND age3 = 3"},
			want:    "((u.age = 1 OR u.age2 = 2) AND u.age3 = 3)",
			wantErr: assert.NoError,
		},
		{
			name:    "negation",
			args:    args{text: "-active = true"},
			want:    "NOT u.active = true",
			wantErr: assert.NoError,
		},
		{
			name:    "not a composite",
			args:    args{text: "NOT (active = true OR age = 30)"},
			want:    "NOT (u.active = true OR u.age = 30)",
			wantErr: assert.NoError,
		},
		{
			name:    "implicit search of one field",
			args:    args{text: "bob"},
			want:    `u.name LIKE "%bob%"`,
			wantErr: assert.NoError,
		},
		{
			name:    "implicit search escapes the pattern",
			args:    args{text: `"50%"`},
			want:    `u.name LIKE "%50\\%%"`,
			wantErr: assert.NoError,
		},
		{
			name:    "not sortable is still filterable",
			args:    args{text: "active = true"},
			want:    "u.active = true",
			wantErr: assert.NoError,
		},
		{
			name: "every implicit field is searched",
			schema: NewSchema(
				NewField("name").Ref("u.name").String().Implicit(),
				NewField("email").Ref("u.email").String().Implicit(),
				NewField("age").Ref("u.age").Int().Filterable(),
			).MustBuild(),
			args:    args{text: "bob"},
			want:    `(u.name LIKE "%bob%" OR u.email LIKE "%bob%")`,
			wantErr: assert.NoError,
		},
	}
	for _, malformed := range malformedFilters() {
		tests = append(tests, struct {
			name    string
			schema  *Schema
			args    args
			want    string
			wantErr assert.ErrorFunc
		}{name: malformed.name, args: args{filter: malformed.filter}, wantErr: assert.Error})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := test.schema
			if s == nil {
				s = filterSchema(t)
			}
			filter := test.args.filter
			if filter == nil {
				parsed, err := s.ParseFilter(test.args.text)
				assert.NoError(t, err)
				filter = parsed
			}
			got, gotErr := s.CompileFilter(filter)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				assert.IsError(t, gotErr, ErrInvalidFilter)
				return
			}
			assert.Equal(t, test.want, renderExpr(got))
		})
	}
}

func TestOp_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		op   Op
		want string
	}{
		{name: "eq", op: OpEq, want: "="},
		{name: "ne", op: OpNe, want: "!="},
		{name: "lt", op: OpLt, want: "<"},
		{name: "le", op: OpLe, want: "<="},
		{name: "gt", op: OpGt, want: ">"},
		{name: "ge", op: OpGe, want: ">="},
		{name: "like", op: OpLike, want: "LIKE"},
		{name: "unset", op: 0, want: "Op(0)"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, test.op.String())
		})
	}
}

type unsupportedExpr struct{ Expr }

func TestValidateExpr(t *testing.T) {
	t.Parallel()
	field := testSchema(t).Fields()[0]
	cmp := Cmp{Field: field, Op: OpEq, Value: int64(1)}
	for _, test := range []struct {
		name  string
		expr  Expr
		valid bool
	}{
		{name: "nil root", valid: true},
		{name: "comparison", expr: cmp, valid: true},
		{name: "null", expr: Cmp{Field: field, Op: OpNe}, valid: true},
		{name: "nested", expr: And{Exprs: []Expr{cmp, Or{Exprs: []Expr{Not{Expr: cmp}}}}}, valid: true},
		{name: "empty and", expr: And{}},
		{name: "empty or", expr: Or{}},
		{name: "nil and operand", expr: And{Exprs: []Expr{cmp, nil}}},
		{name: "nil or operand", expr: Or{Exprs: []Expr{nil}}},
		{name: "nil not operand", expr: Not{}},
		{name: "no field", expr: Cmp{Op: OpEq}},
		{name: "zero field", expr: Cmp{Field: &Field{}, Op: OpEq}},
		{name: "unknown op", expr: Cmp{Field: field, Op: Op(99), Value: int64(1)}},
		{name: "null range", expr: Cmp{Field: field, Op: OpGt}},
		{name: "pointer node", expr: &cmp},
		{name: "typed nil", expr: (*Cmp)(nil)},
		{name: "unsupported node", expr: unsupportedExpr{}},
		{name: "unsupported value", expr: Cmp{Field: field, Op: OpEq, Value: []int{1}}},
		{name: "non string like", expr: Cmp{Field: field, Op: OpLike, Value: int64(1)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := ValidateExpr(test.expr)
			if test.valid {
				assert.NoError(t, err)
			} else {
				assert.IsError(t, err, ErrDeclaration)
			}
		})
	}
}

// FuzzCompile checks that no filter a schema accepts can panic the compiler or
// leave an empty conjunction behind, which would render as "()" in SQL.
func FuzzCompile(f *testing.F) {
	seeds := []string{
		"", "  ", "bob", `display_name = "bob"`, `display_name:"bo*"`,
		"age > -30", "rating >= 4.5", "active = true", "created_at = null", "latency < 250ms",
		"age = 1 OR age = 2 AND active = true",
		"NOT (active = true OR age = 30)",
		`metadata.tags = "blue"`,
		"(((age = 1)))",
		strings.Repeat("(", 40) + "age = 1" + strings.Repeat(")", 40),
	}
	for _, seed := range seeds {
		f.Add(seed)
	}

	schema := NewSchema(
		NewField("id").Ref("u.id").Int().Unique(),
		NewField("display_name").Ref("u.name").String().Filterable().Sortable().Implicit(),
		NewField("created_at").Ref("u.created_at").Time().Filterable().Sortable(),
		NewField("latency").Ref("u.latency").Duration().Filterable().Sortable(),
		NewField("active").Ref("u.active").Bool().Filterable(),
		NewField("rating").Ref("u.rating").Float().Filterable(),
		NewField("age").Ref("u.age").Int().Filterable(),
		NewField("metadata", "tags").Ref("m.tags").String().Filterable(),
	).MustBuild()

	f.Fuzz(func(t *testing.T, input string) {
		filter, err := schema.ParseFilter(input)
		if err != nil {
			return
		}
		expr, err := schema.CompileFilter(filter)
		if err != nil {
			t.Fatalf("filter %q validated but did not compile: %v", input, err)
		}
		assertNoEmptyNode(t, input, expr)
	})
}

func assertNoEmptyNode(t *testing.T, input string, e Expr) {
	t.Helper()
	switch node := e.(type) {
	case nil:
	case And:
		if len(node.Exprs) == 0 {
			t.Fatalf("filter %q compiled to an empty conjunction", input)
		}
		for _, expr := range node.Exprs {
			assertNoEmptyNode(t, input, expr)
		}
	case Or:
		if len(node.Exprs) == 0 {
			t.Fatalf("filter %q compiled to an empty disjunction", input)
		}
		for _, expr := range node.Exprs {
			assertNoEmptyNode(t, input, expr)
		}
	case Not:
		if node.Expr == nil {
			t.Fatalf("filter %q compiled to a negation of nothing", input)
		}
		assertNoEmptyNode(t, input, node.Expr)
	case Cmp:
		if node.Field == nil {
			t.Fatalf("filter %q compiled to a comparison with no field", input)
		}
	default:
		t.Fatalf("filter %q compiled to unknown node %T", input, e)
	}
}
