package aip132_test

import (
	"testing"

	"github.com/ysomad/flop/aip132"
	"github.com/ysomad/flop/internal/assert"
)

func TestParseOrderBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    []aip132.OrderBy
		wantErr assert.ErrorFunc
	}{
		{name: "empty", input: "", want: nil, wantErr: assert.NoError},
		{name: "spaces only", input: "   ", want: nil, wantErr: assert.NoError},
		{
			name:    "single field",
			input:   "create_time",
			want:    []aip132.OrderBy{{FieldPath: aip132.NewFieldPath("create_time")}},
			wantErr: assert.NoError,
		},
		{
			name:  "descending and nested path",
			input: "create_time desc, user.name",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("create_time"), Descending: true},
				{FieldPath: aip132.NewFieldPath("user", "name")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "quoted segment",
			input: "metadata.`odd name`.value",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("metadata", "odd name", "value")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "escaped backtick",
			input: "metadata.`a``b`",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("metadata", "a`b")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "spaces around dot are insignificant",
			input: "user . name",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("user", "name")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "spaces at every junction",
			input: " a .b, c. d desc ",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("a", "b")},
				{FieldPath: aip132.NewFieldPath("c", "d"), Descending: true},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "spaces around dot with quoted segment",
			input: "metadata . `odd name` . value",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("metadata", "odd name", "value")},
			},
			wantErr: assert.NoError,
		},
		{
			// A dot binds tighter than the desc suffix, so this is one field.
			name:  "dot wins over desc suffix",
			input: "create_time. desc",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("create_time", "desc")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "empty quoted segment",
			input: "``",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "field named desc in descending order",
			input: "desc desc",
			want: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("desc"), Descending: true},
			},
			wantErr: assert.NoError,
		},
		{name: "duplicate field", input: "id, id desc", wantErr: assert.Error},
		{name: "desc is not prefix matched", input: "id descending", wantErr: assert.Error},
		{name: "dot after desc suffix", input: "a desc . b", wantErr: assert.Error},
		{name: "unterminated quoted segment", input: "`abc", wantErr: assert.Error},
		{name: "tab is not a space", input: "id\tdesc", wantErr: assert.Error},
		{name: "ascending is not a keyword", input: "id asc", wantErr: assert.Error},
		{name: "uppercase desc", input: "id DESC", wantErr: assert.Error},
		{name: "trailing comma", input: "id,", wantErr: assert.Error},
		{name: "empty field", input: "id,,name", wantErr: assert.Error},
		{name: "double dot", input: "user..name", wantErr: assert.Error},
		{name: "leading digit", input: "1st_name", wantErr: assert.Error},
		{name: "non-ascii", input: "имя", wantErr: assert.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := aip132.ParseOrderBy(test.input)
			test.wantErr(t, err)
			if err != nil {
				return
			}
			assert.Equal(t, test.want, got)
		})
	}
}

func TestNewFieldPath(t *testing.T) {
	t.Parallel()
	type args struct {
		segments []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{name: "single", args: args{segments: []string{"name"}}, want: []string{"name"}},
		{name: "nested", args: args{segments: []string{"user", "name"}}, want: []string{"user", "name"}},
		{name: "quoted", args: args{segments: []string{"metadata", "odd name"}}, want: []string{"metadata", "odd name"}},
		{name: "escaped backtick", args: args{segments: []string{"a`b"}}, want: []string{"a`b"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// The path is taken over the segments, so a caller changing them
			// afterwards cannot reach it.
			segments := append([]string{}, test.args.segments...)
			path := aip132.NewFieldPath(segments...)
			segments[0] = "changed"
			assert.Equal(t, test.want, path.Segments())
		})
	}
}

func TestFieldPath_String(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		segments []string
		want     string
	}{
		{name: "single", segments: []string{"name"}, want: "name"},
		{name: "nested", segments: []string{"user", "name"}, want: "user.name"},
		{name: "quoted", segments: []string{"metadata", "odd name"}, want: "metadata.`odd name`"},
		{name: "escaped backtick", segments: []string{"a`b"}, want: "`a``b`"},
		{name: "dot inside a segment", segments: []string{"outer", "odd.name"}, want: "outer.`odd.name`"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, aip132.NewFieldPath(test.segments...).String())
		})
	}
}

func TestFieldPath_Segments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		segments []string
		want     []string
	}{
		{name: "single", segments: []string{"name"}, want: []string{"name"}},
		{name: "nested", segments: []string{"user", "name"}, want: []string{"user", "name"}},
		{name: "quoted", segments: []string{"metadata", "odd name"}, want: []string{"metadata", "odd name"}},
		{name: "escaped backtick", segments: []string{"a`b"}, want: []string{"a`b"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := aip132.NewFieldPath(test.segments...)
			// What is handed back is a copy, so writing to it changes nothing.
			path.Segments()[0] = "changed"
			assert.Equal(t, test.want, path.Segments())
		})
	}
}

func TestFieldPath_Equals(t *testing.T) {
	t.Parallel()
	type args struct {
		other aip132.FieldPath
	}
	tests := []struct {
		name     string
		segments []string
		args     args
		want     bool
	}{
		{name: "single", segments: []string{"name"}, args: args{other: aip132.NewFieldPath("name")}, want: true},
		{
			name:     "nested",
			segments: []string{"user", "name"},
			args:     args{other: aip132.NewFieldPath("user", "name")},
			want:     true,
		},
		{
			name:     "quoted",
			segments: []string{"metadata", "odd name"},
			args:     args{other: aip132.NewFieldPath("metadata", "odd name")},
			want:     true,
		},
		{name: "escaped backtick", segments: []string{"a`b"}, args: args{other: aip132.NewFieldPath("a`b")}, want: true},
		{name: "another path", segments: []string{"name"}, args: args{other: aip132.NewFieldPath("other")}},
		{name: "prefix", segments: []string{"user", "name"}, args: args{other: aip132.NewFieldPath("user")}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, aip132.NewFieldPath(test.segments...).Equals(test.args.other))
		})
	}
}
