package main

import (
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

// checker is the handful of assertions these tests use, on the standard library
// alone (schema's go.mod carries no assertion module): assert reports and goes
// on, require stops the test.
type checker struct{ stop bool }

var (
	assert  = checker{}
	require = checker{stop: true}
)

func (c checker) fail(t *testing.T, msg []any, format string, args ...any) bool {
	t.Helper()
	s := fmt.Sprintf(format, args...)
	if len(msg) > 0 {
		if f, ok := msg[0].(string); ok && len(msg) > 1 {
			s += ": " + fmt.Sprintf(f, msg[1:]...)
		} else {
			s += ": " + fmt.Sprint(msg...)
		}
	}
	if c.stop {
		t.Fatal(s)
	}
	t.Error(s)
	return false
}

func (c checker) Equal(t *testing.T, want, got any, msg ...any) bool {
	t.Helper()
	if reflect.DeepEqual(want, got) {
		return true
	}
	return c.fail(t, msg, "want %#v, got %#v", want, got)
}

func (c checker) NotEqual(t *testing.T, a, b any, msg ...any) bool {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		return true
	}
	return c.fail(t, msg, "both are %#v", a)
}

func (c checker) True(t *testing.T, v bool, msg ...any) bool {
	t.Helper()
	if v {
		return true
	}
	return c.fail(t, msg, "want true")
}

func (c checker) False(t *testing.T, v bool, msg ...any) bool {
	t.Helper()
	if !v {
		return true
	}
	return c.fail(t, msg, "want false")
}

func (c checker) Contains(t *testing.T, s, sub string, msg ...any) bool {
	t.Helper()
	if strings.Contains(s, sub) {
		return true
	}
	return c.fail(t, msg, "%q does not contain %q", s, sub)
}

func (c checker) NotContains(t *testing.T, s, sub string, msg ...any) bool {
	t.Helper()
	if !strings.Contains(s, sub) {
		return true
	}
	return c.fail(t, msg, "%q contains %q", s, sub)
}

func (c checker) Regexp(t *testing.T, pattern, s string, msg ...any) bool {
	t.Helper()
	if regexp.MustCompile(pattern).MatchString(s) {
		return true
	}
	return c.fail(t, msg, "%q does not match %s", s, pattern)
}

func (c checker) LessOrEqual(t *testing.T, a, b int, msg ...any) bool {
	t.Helper()
	if a <= b {
		return true
	}
	return c.fail(t, msg, "%d is more than %d", a, b)
}

func (c checker) Len(t *testing.T, v any, n int, msg ...any) bool {
	t.Helper()
	if got := reflect.ValueOf(v).Len(); got != n {
		return c.fail(t, msg, "length %d, want %d: %#v", got, n, v)
	}
	return true
}

func (c checker) Empty(t *testing.T, v any, msg ...any) bool {
	t.Helper()
	r := reflect.ValueOf(v)
	if !r.IsValid() || r.Len() == 0 {
		return true
	}
	return c.fail(t, msg, "want empty, got %#v", v)
}

func (c checker) Nil(t *testing.T, v any, msg ...any) bool {
	t.Helper()
	r := reflect.ValueOf(v)
	if !r.IsValid() || (r.Kind() == reflect.Pointer || r.Kind() == reflect.Map || r.Kind() == reflect.Slice) && r.IsNil() {
		return true
	}
	return c.fail(t, msg, "want nil, got %#v", v)
}

func (c checker) NoError(t *testing.T, err error, msg ...any) bool {
	t.Helper()
	if err == nil {
		return true
	}
	return c.fail(t, msg, "unexpected error: %v", err)
}

func (c checker) ErrorContains(t *testing.T, err error, sub string, msg ...any) bool {
	t.Helper()
	if err != nil && strings.Contains(err.Error(), sub) {
		return true
	}
	return c.fail(t, msg, "error %v does not contain %q", err, sub)
}

func (c checker) ErrorIs(t *testing.T, err, target error, msg ...any) bool {
	t.Helper()
	if errors.Is(err, target) {
		return true
	}
	return c.fail(t, msg, "error %v is not %v", err, target)
}

func (c checker) ErrorAs(t *testing.T, err error, target any, msg ...any) bool {
	t.Helper()
	if errors.As(err, target) {
		return true
	}
	return c.fail(t, msg, "error %v is not a %T", err, target)
}
