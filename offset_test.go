package flop

import (
	"math"
	"testing"

	"github.com/alecthomas/assert/v2"
)

func TestOffset(t *testing.T) {
	t.Parallel()

	type args struct {
		page     int32
		pageSize int32
	}
	tests := []struct {
		name    string
		args    args
		want    int64
		wantErr errorFunc
	}{
		{
			name:    "zero page number selects the first page",
			args:    args{pageSize: 10},
			want:    0,
			wantErr: assert.NoError,
		},
		{
			name:    "third page",
			args:    args{page: 3, pageSize: 20},
			want:    40,
			wantErr: assert.NoError,
		},
		{
			// The offset is widened before it is multiplied, so a page number
			// and size that would overflow an int32 still resolve.
			name:    "offset wider than an int32",
			args:    args{page: math.MaxInt32, pageSize: 100},
			want:    (math.MaxInt32 - 1) * 100,
			wantErr: assert.NoError,
		},
		{
			name:    "negative page number",
			args:    args{page: -1, pageSize: 10},
			wantErr: assert.Error,
		},
		{
			name:    "zero page size",
			args:    args{page: 1},
			wantErr: assert.Error,
		},
		{
			name:    "negative page size",
			args:    args{page: 1, pageSize: -1},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := Offset(test.args.page, test.args.pageSize)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.want, got)
		})
	}
}

func TestTotalPages(t *testing.T) {
	t.Parallel()

	type args struct {
		totalItems int64
		pageSize   int32
	}
	tests := []struct {
		name string
		args args
		want int64
	}{
		{name: "exact pages", args: args{totalItems: 40, pageSize: 20}, want: 2},
		{name: "partial last page", args: args{totalItems: 41, pageSize: 20}, want: 3},
		{name: "fewer items than a page", args: args{totalItems: 1, pageSize: 20}, want: 1},
		{name: "no items", args: args{pageSize: 20}, want: 0},
		{name: "negative items", args: args{totalItems: -1, pageSize: 20}, want: 0},
		{name: "zero page size", args: args{totalItems: 40}, want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, test.want, TotalPages(test.args.totalItems, test.args.pageSize))
		})
	}
}

func TestNewOffsetPage(t *testing.T) {
	t.Parallel()
	rows := []int32{1, 2}

	type args struct {
		rows       []int32
		page       int32
		pageSize   int32
		totalItems int64
	}
	tests := []struct {
		name    string
		args    args
		want    OffsetPage[int32]
		wantErr errorFunc
	}{
		{
			name: "exact pages",
			args: args{rows: rows, page: 2, pageSize: 2, totalItems: 4},
			want: OffsetPage[int32]{
				Items: rows, Page: 2, TotalPages: 2, TotalItems: 4,
			},
			wantErr: assert.NoError,
		},
		{
			name: "partial last page",
			args: args{rows: rows, page: 3, pageSize: 2, totalItems: 5},
			want: OffsetPage[int32]{
				Items: rows, Page: 3, TotalPages: 3, TotalItems: 5,
			},
			wantErr: assert.NoError,
		},
		{
			name: "zero page number selects the first page",
			args: args{rows: rows, pageSize: 2, totalItems: 2},
			want: OffsetPage[int32]{
				Items: rows, Page: 1, TotalPages: 1, TotalItems: 2,
			},
			wantErr: assert.NoError,
		},
		{
			name: "no items",
			args: args{rows: []int32{}, page: 1, pageSize: 2},
			want: OffsetPage[int32]{
				Items: []int32{}, Page: 1,
			},
			wantErr: assert.NoError,
		},
		{
			name:    "negative page number",
			args:    args{rows: rows, page: -1, pageSize: 2},
			wantErr: assert.Error,
		},
		{
			name:    "zero page size",
			args:    args{rows: rows, page: 1},
			wantErr: assert.Error,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, gotErr := NewOffsetPage(
				test.args.rows,
				test.args.page,
				test.args.pageSize,
				test.args.totalItems,
			)
			test.wantErr(t, gotErr)
			if gotErr != nil {
				return
			}
			assert.Equal(t, test.want, got)
		})
	}
}
