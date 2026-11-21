package ctable

import (
	"strings"
	"testing"
)

const nestedCountMinSchema = `package nested

fixed table Child
{
    vals [2..4]uint8
}

fixed table Loose
{
    nest [..4]Child
}

fixed table Tight
{
    nest [1..4]Child
}
`

// cFunctionBody is the body of the one generated function whose definition
// line holds head, through its closing brace at column zero.
func cFunctionBody(t *testing.T, src, head string) string {
	t.Helper()
	at := strings.Index(src, head)
	if at < 0 {
		var near []string
		for l := range strings.SplitSeq(src, "\n") {
			if strings.Contains(l, "_body_(") && strings.Contains(l, "void") {
				near = append(near, l)
			}
		}
		t.Fatalf("no %q in the generated header; its bounds bodies:\n%s", head, strings.Join(near, "\n"))
	}
	rest := src[at:]
	body, _, closed := strings.Cut(rest, "\n}\n")
	if !closed {
		t.Fatalf("%q has no closing brace", head)
	}
	return body
}

// A child's count minimum is a bound of every record that holds the child
// (docs/SPEC.md §5.1 row 3), whatever the holding array's own minimum: the
// fixed read's bounds pass and the debug write check walk a counted array of
// records whose one bound is a positive minimum inside the record, and the
// array's own minimum is still held (F-187).
func TestCFixedFormHoldsACountMinimumInsideEachCountedChild(t *testing.T) {
	t.Parallel()
	h := tableH(t, nestedCountMinSchema)
	for _, c := range []struct{ head, want string }{
		{"schema_nested_loose_fixed_clamp_body_( Loose * value", "schema_nested_child_fixed_clamp_body_( &value->nest[i], out_of_range, damaged );"},
		{"schema_nested_tight_fixed_clamp_body_( Tight * value", "schema_nested_child_fixed_clamp_body_( &value->nest[i], out_of_range, damaged );"},
		{"schema_nested_tight_fixed_clamp_body_( Tight * value", "if ( value->nest_count < 1 ) { (*out_of_range)++; }"},
		{"schema_nested_loose_fixed_write_check_body_( const Loose * value", "schema_nested_child_fixed_write_check_body_( &value->nest[i], out_of_range, damaged );"},
		{"schema_nested_tight_fixed_write_check_body_( const Tight * value", "schema_nested_child_fixed_write_check_body_( &value->nest[i], out_of_range, damaged );"},
		{"schema_nested_tight_fixed_write_check_body_( const Tight * value", "if ( value->nest_count < 1 ) { (*out_of_range)++; }"},
		{"schema_nested_child_fixed_clamp_body_( Child * value", "if ( value->vals_count < 2 ) { (*out_of_range)++; }"},
	} {
		if body := cFunctionBody(t, h, c.head); !strings.Contains(body, c.want) {
			t.Errorf("%s does not hold the bound: no %q in\n%s", c.head, c.want, body)
		}
	}
}
