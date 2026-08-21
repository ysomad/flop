package aip132

import (
	"fmt"
	"regexp"
	"strings"

	participle "github.com/alecthomas/participle/v2"
	"github.com/alecthomas/participle/v2/lexer"
)

const stringLiteralExpr = `[a-zA-Z_][a-zA-Z_0-9]*`

var stringLiteralRE = regexp.MustCompile(`^` + stringLiteralExpr + `$`)

var (
	orderByLexer = lexer.MustSimple([]lexer.SimpleRule{
		{Name: "Spaces", Pattern: `[ ]+`},
		{Name: "String", Pattern: stringLiteralExpr},
		{Name: "QuotedString", Pattern: "`(``|[^`])*`"},
		{Name: "Operators", Pattern: "[.,]"},
	})

	orderByParser = participle.MustBuild[orderByList](participle.Lexer(orderByLexer))
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
	for _, segment := range segments {
		if s.Len() > 0 {
			s.WriteString(".")
		}
		if stringLiteralRE.MatchString(segment) {
			s.WriteString(segment)
		} else {
			s.WriteString("`")
			s.WriteString(strings.ReplaceAll(segment, "`", "``"))
			s.WriteString("`")
		}
	}
	return FieldPath{
		segments:  segments,
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

func (f FieldPath) GetSegments() []string {
	return f.segments
}

// ParseOrderBy parses an AIP-132 order_by list. The method validates the
// syntax is correct and each identifier appears at most once, but
// it does not validate the identifiers themselves are valid.
func ParseOrderBy(text string) ([]OrderBy, error) {
	// Empty order_by list.
	if strings.Trim(text, " ") == "" {
		return nil, nil
	}

	expr, err := orderByParser.ParseString("", text)
	if err != nil {
		return nil, fmt.Errorf("syntax error: %w", err)
	}

	var result []OrderBy
	for _, clause := range expr.SortOrder {
		result = append(result, OrderBy{
			FieldPath:  NewFieldPath(clause.FieldPath.Path()...),
			Descending: clause.Order.Desc,
		})
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

type orderByList struct {
	SortOrder []*orderByClause `parser:"@@ ( Spaces? ',' @@ )* Spaces?"`
}

type orderByClause struct {
	FieldPath *fieldPath `parser:"@@"`
	Order     *order     `parser:"@@"`
}

type order struct {
	Desc bool `parser:"@( Spaces 'desc' )?"`
}

type fieldPath struct {
	Segments []*segment `parser:"Spaces? @@ ( '.' @@ )*"`
}

// Path returns the field path as a list of path segments.
func (f *fieldPath) Path() []string {
	result := make([]string, 0, len(f.Segments))
	for _, segment := range f.Segments {
		result = append(result, segment.Value())
	}
	return result
}

type segment struct {
	StringValue  *string `parser:"@String"`
	QuotedString *string `parser:"| @QuotedString"`
}

func (s *segment) Value() string {
	if s.QuotedString != nil {
		// Remove the outer backticks and replace all occurrences
		// of double backticks with single backticks.
		unquotedString := (*s.QuotedString)[1 : len(*s.QuotedString)-1]
		return strings.ReplaceAll(unquotedString, "``", "`")
	}
	if s.StringValue != nil {
		return *s.StringValue
	}
	// Should never happen if parsing succeeds.
	panic("invalid syntax")
}

// OrderByString returns the AIP-132 representation of an order.
//
// It is what the cursor binding is taken over, so an order that resolves the
// same way fingerprints the same way.
func OrderByString(order []OrderBy) string {
	if len(order) == 0 {
		return ""
	}
	var s strings.Builder
	for i, field := range order {
		if i > 0 {
			s.WriteString(", ")
		}
		s.WriteString(field.FieldPath.String())
		if field.Descending {
			s.WriteString(" desc")
		}
	}
	return s.String()
}
