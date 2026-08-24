package flop

import (
	"testing"

	"github.com/ysomad/flop/internal/assert"
)

func Test_errorf(t *testing.T) {
	t.Parallel()
	type args struct {
		sentinel error
		format   string
		args     []any
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "with args",
			args: args{sentinel: ErrInvalidOrder, format: "unknown field %q", args: []any{"total"}},
			want: `flop: invalid order: unknown field "total"`,
		},
		{
			name: "without args",
			args: args{sentinel: ErrInvalidCursor, format: "malformed"},
			want: "flop: invalid cursor: malformed",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			err := errorf(test.args.sentinel, test.args.format, test.args.args...)
			assert.Equal(t, test.want, err.Error())
			assert.IsError(t, err, test.args.sentinel)
		})
	}
}
