// Package assert holds the assertions flop's tests make, so the module needs no
// test dependencies.
package assert

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

// ErrorFunc is what a table row states about the error it expects, either
// [NoError] or [Error].
type ErrorFunc = func(*testing.T, error, ...any)

// NoError asserts that err is nil.
func NoError(t *testing.T, err error, msgAndArgs ...any) {
	if err == nil {
		return
	}
	t.Helper()
	t.Fatalf("%s\n%+v", message("did not expect an error but got:", msgAndArgs...), err)
}

// Error asserts that err is not nil.
func Error(t *testing.T, err error, msgAndArgs ...any) {
	if err != nil {
		return
	}
	t.Helper()
	t.Fatal(message("expected an error", msgAndArgs...))
}

// IsError asserts that some error in err's tree matches target.
func IsError(t *testing.T, err, target error, msgAndArgs ...any) {
	if errors.Is(err, target) {
		return
	}
	t.Helper()
	t.Fatalf("%s\nerror:  %+v\ntarget: %+v",
		message("expected the error tree to contain the target:", msgAndArgs...), err, target)
}

// Equal asserts that expected and actual are deeply equal.
func Equal[T any](t *testing.T, expected, actual T, msgAndArgs ...any) {
	if reflect.DeepEqual(expected, actual) {
		return
	}
	t.Helper()
	t.Fatalf("%s\nexpected: %#v\nactual:   %#v",
		message("expected values to be equal:", msgAndArgs...), expected, actual)
}

// Zero asserts that value is its zero value, counting an empty slice, map or
// array as zero.
func Zero[T any](t *testing.T, value T, msgAndArgs ...any) {
	var zero T
	if reflect.DeepEqual(value, zero) || emptyContainer(value) {
		return
	}
	t.Helper()
	t.Fatalf("%s\n%#v", message("expected a zero value but got:", msgAndArgs...), value)
}

// True asserts that ok is true.
func True(t *testing.T, ok bool, msgAndArgs ...any) {
	if ok {
		return
	}
	t.Helper()
	t.Fatal(message("expected expression to be true", msgAndArgs...))
}

// False asserts that ok is false.
func False(t *testing.T, ok bool, msgAndArgs ...any) {
	if !ok {
		return
	}
	t.Helper()
	t.Fatal(message("expected expression to be false", msgAndArgs...))
}

// Panics asserts that fn panics.
func Panics(t *testing.T, fn func(), msgAndArgs ...any) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error(message("expected function to panic", msgAndArgs...))
		}
	}()
	fn()
}

// emptyContainer reports whether value is a slice, map or array of no elements,
// nil or not.
func emptyContainer(value any) bool {
	v := reflect.ValueOf(value)
	kind := v.Kind()
	if kind != reflect.Slice && kind != reflect.Map && kind != reflect.Array {
		return false
	}
	return v.Len() == 0
}

// message renders the optional trailing arguments an assertion takes, falling
// back to dflt when a call states none.
func message(dflt string, msgAndArgs ...any) string {
	if len(msgAndArgs) == 0 {
		return dflt
	}
	format, ok := msgAndArgs[0].(string)
	if !ok {
		panic("assert: message argument must be a format string")
	}
	return fmt.Sprintf(format, msgAndArgs[1:]...)
}
