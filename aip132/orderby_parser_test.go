package aip132_test

import (
	"testing"

	"github.com/alecthomas/assert/v2"
	"github.com/ysomad/flop/aip132"
)

type errorFunc = func(testing.TB, error, ...any)

func TestParseOrderBy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    []aip132.OrderBy
		wantErr errorFunc
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
		{name: "duplicate field", input: "id, id desc", wantErr: assert.Error},
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

func TestFieldPath(t *testing.T) {
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
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := aip132.NewFieldPath(test.segments...)
			assert.Equal(t, test.want, path.String())
			assert.Equal(t, test.segments, path.GetSegments())
			assert.True(t, path.Equals(aip132.NewFieldPath(test.segments...)))
			assert.False(t, path.Equals(aip132.NewFieldPath("other")))
		})
	}
}

func TestOrderByString(t *testing.T) {
	t.Parallel()
	type args struct {
		order []aip132.OrderBy
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{name: "nil order", args: args{order: nil}, want: ""},
		{name: "empty order", args: args{order: []aip132.OrderBy{}}, want: ""},
		{
			name: "single ascending",
			args: args{order: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("create_time")},
			}},
			want: "create_time",
		},
		{
			name: "single descending",
			args: args{order: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("create_time"), Descending: true},
			}},
			want: "create_time desc",
		},
		{
			name: "multiple fields mixed direction",
			args: args{order: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("create_time"), Descending: true},
				{FieldPath: aip132.NewFieldPath("user", "name")},
				{FieldPath: aip132.NewFieldPath("id"), Descending: true},
			}},
			want: "create_time desc, user.name, id desc",
		},
		{
			name: "quoted segment",
			args: args{order: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("metadata", "odd name", "value")},
			}},
			want: "metadata.`odd name`.value",
		},
		{
			name: "escaped backtick",
			args: args{order: []aip132.OrderBy{
				{FieldPath: aip132.NewFieldPath("metadata", "a`b"), Descending: true},
			}},
			want: "metadata.`a``b` desc",
		},
		{
			name: "zero field path",
			args: args{order: []aip132.OrderBy{{}}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, aip132.OrderByString(tt.args.order))
		})
	}
}
