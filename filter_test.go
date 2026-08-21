package flop

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alecthomas/assert/v2"
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
		return node.Field.Column() + " " + node.Op.String() + " " + renderValue(node.Value)
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

func TestSchema_ParseFilter(t *testing.T) {
	t.Parallel()
	type args struct {
		text string
	}
	tests := []struct {
		name    string
		args    args
		want    string
		wantErr errorFunc
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
			name:    "wildcard against a non string field",
			args:    args{text: `total = 5*`},
			wantErr: assert.Error,
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

		{name: "undeclared field", args: args{text: "nope = 1"}, wantErr: assert.Error},
		{name: "declared but not filterable", args: args{text: `secret = "x"`}, wantErr: assert.Error},
		{name: "not sortable is still filterable", args: args{text: "active = true"}, want: "u.active = true", wantErr: assert.NoError},
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
		{name: "null is not ordered", args: args{text: "created_at > null"}, wantErr: assert.Error},
		{name: "composite argument", args: args{text: "age = (1 OR 2)"}, wantErr: assert.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s := filterSchema(t)
			filter, gotErr := s.ParseFilter(test.args.text)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			expr, err := s.Compile(filter)
			assert.NoError(t, err)
			assert.Equal(t, test.want, renderExpr(expr))
		})
	}
}

// filterSchema declares the fields the filter rows above name, including the
// spare ordered fields a row needs to compare four operators at once.
func filterSchema(t *testing.T) *Schema {
	t.Helper()
	s, err := NewSchema(
		NewField("id").Column("u.id").Int().Unique().Build(),
		NewField("display_name").Column("u.name").String().Filterable().Sortable().Implicit().Build(),
		NewField("created_at").Column("u.created_at").Time().Filterable().Sortable().Build(),
		NewField("active").Column("u.active").Bool().Filterable().Build(),
		NewField("rating").Column("u.rating").Float().Filterable().Build(),
		NewField("age").Column("u.age").Int().Filterable().Build(),
		NewField("age2").Column("u.age2").Int().Filterable().Build(),
		NewField("age3").Column("u.age3").Int().Filterable().Build(),
		NewField("age4").Column("u.age4").Int().Filterable().Build(),
		NewField("secret").Column("u.secret").String().Build(),
		NewField("metadata", "tags").Column("m.tags").String().Filterable().Build(),
	).Build()
	assert.NoError(t, err)
	return s
}

func TestSchema_Compile_implicitFields(t *testing.T) {
	t.Parallel()
	type args struct {
		text string
	}
	tests := []struct {
		name    string
		schema  *Schema
		args    args
		want    string
		wantErr errorFunc
	}{
		{
			name: "every implicit field is searched",
			schema: NewSchema(
				NewField("name").Column("u.name").String().Implicit().Build(),
				NewField("email").Column("u.email").String().Implicit().Build(),
				NewField("age").Column("u.age").Int().Filterable().Build(),
			).MustBuild(),
			args:    args{text: "bob"},
			want:    `(u.name LIKE "%bob%" OR u.email LIKE "%bob%")`,
			wantErr: assert.NoError,
		},
		{
			name: "no implicit field",
			schema: NewSchema(
				NewField("name").Column("u.name").String().Filterable().Build(),
			).MustBuild(),
			args:    args{text: "bob"},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			filter, gotErr := test.schema.ParseFilter(test.args.text)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			expr, err := test.schema.Compile(filter)
			assert.NoError(t, err)
			assert.Equal(t, test.want, renderExpr(expr))
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

// FuzzCompile checks that no filter a schema accepts can panic the compiler or
// leave an empty conjunction behind, which would render as "()" in SQL.
func FuzzCompile(f *testing.F) {
	seeds := []string{
		"", "  ", "bob", `display_name = "bob"`, `display_name:"bo*"`,
		"age > -30", "rating >= 4.5", "active = true", "created_at = null",
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
		NewField("id").Column("u.id").Int().Unique().Build(),
		NewField("display_name").Column("u.name").String().Filterable().Sortable().Implicit().Build(),
		NewField("created_at").Column("u.created_at").Time().Filterable().Sortable().Build(),
		NewField("active").Column("u.active").Bool().Filterable().Build(),
		NewField("rating").Column("u.rating").Float().Filterable().Build(),
		NewField("age").Column("u.age").Int().Filterable().Build(),
		NewField("metadata", "tags").Column("m.tags").String().Filterable().Build(),
	).MustBuild()

	f.Fuzz(func(t *testing.T, input string) {
		filter, err := schema.ParseFilter(input)
		if err != nil {
			return
		}
		expr, err := schema.Compile(filter)
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
