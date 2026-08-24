package orderby_test

import (
	"testing"

	"github.com/ysomad/flop/internal/assert"
	"github.com/ysomad/flop/orderby"
)

func TestParse(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		input   string
		want    []orderby.OrderBy
		wantErr assert.ErrorFunc
	}{
		{name: "empty", input: "", want: nil, wantErr: assert.NoError},
		{name: "spaces only", input: "   ", want: nil, wantErr: assert.NoError},
		{
			name:    "single field",
			input:   "create_time",
			want:    []orderby.OrderBy{{FieldPath: orderby.NewFieldPath("create_time")}},
			wantErr: assert.NoError,
		},
		{
			name:  "descending and nested path",
			input: "create_time desc, user.name",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("create_time"), Descending: true},
				{FieldPath: orderby.NewFieldPath("user", "name")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "quoted segment",
			input: "metadata.`odd name`.value",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("metadata", "odd name", "value")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "escaped backtick",
			input: "metadata.`a``b`",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("metadata", "a`b")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "spaces around dot are insignificant",
			input: "user . name",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("user", "name")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "spaces at every junction",
			input: " a .b, c. d desc ",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("a", "b")},
				{FieldPath: orderby.NewFieldPath("c", "d"), Descending: true},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "spaces around dot with quoted segment",
			input: "metadata . `odd name` . value",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("metadata", "odd name", "value")},
			},
			wantErr: assert.NoError,
		},
		{
			// A dot binds tighter than the desc suffix, so this is one field.
			name:  "dot wins over desc suffix",
			input: "create_time. desc",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("create_time", "desc")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "empty quoted segment",
			input: "``",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("")},
			},
			wantErr: assert.NoError,
		},
		{
			name:  "field named desc in descending order",
			input: "desc desc",
			want: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("desc"), Descending: true},
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
			got, err := orderby.Parse(test.input)
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
			path := orderby.NewFieldPath(test.segments...)
			assert.Equal(t, test.want, path.String())
			assert.Equal(t, test.segments, path.GetSegments())
			assert.True(t, path.Equals(orderby.NewFieldPath(test.segments...)))
			assert.False(t, path.Equals(orderby.NewFieldPath("other")))
		})
	}
}

func TestString(t *testing.T) {
	t.Parallel()
	type args struct {
		order []orderby.OrderBy
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{name: "nil order", args: args{order: nil}, want: ""},
		{name: "empty order", args: args{order: []orderby.OrderBy{}}, want: ""},
		{
			name: "single ascending",
			args: args{order: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("create_time")},
			}},
			want: "create_time",
		},
		{
			name: "single descending",
			args: args{order: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("create_time"), Descending: true},
			}},
			want: "create_time desc",
		},
		{
			name: "multiple fields mixed direction",
			args: args{order: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("create_time"), Descending: true},
				{FieldPath: orderby.NewFieldPath("user", "name")},
				{FieldPath: orderby.NewFieldPath("id"), Descending: true},
			}},
			want: "create_time desc, user.name, id desc",
		},
		{
			name: "quoted segment",
			args: args{order: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("metadata", "odd name", "value")},
			}},
			want: "metadata.`odd name`.value",
		},
		{
			name: "escaped backtick",
			args: args{order: []orderby.OrderBy{
				{FieldPath: orderby.NewFieldPath("metadata", "a`b"), Descending: true},
			}},
			want: "metadata.`a``b` desc",
		},
		{
			name: "zero field path",
			args: args{order: []orderby.OrderBy{{}}},
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, orderby.String(tt.args.order))
		})
	}
}
