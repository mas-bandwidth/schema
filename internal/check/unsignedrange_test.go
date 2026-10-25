package check_test

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// SPEC §4.6: an unsigned integer's max-only range is exactly [0, max],
// including its projection, protocol id and generated Go codec.
func TestUnsignedRangeOmittedMinIsZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		storage string
		max     string
	}{
		{"uint8", "0"},
		{"uint8", "255"},
		{"uint16", "65535"},
		{"uint32", "100000"},
		{"uint32", "4294967295"},
		{"uint64", "18446744073709551615"},
		{"uint32", "Ceiling"},
	} {
		t.Run(tc.storage+"/"+tc.max, func(t *testing.T) {
			t.Parallel()
			source := "package probe\nconst Ceiling = 100000\ntype State { health %s | %smax = %s }\n"
			implicit := build(t, fmt.Sprintf(source, tc.storage, "", tc.max))
			explicit := build(t, fmt.Sprintf(source, tc.storage, "min = 0, ", tc.max))
			if got, want := ir.WireProjection(implicit), ir.WireProjection(explicit); got != want {
				t.Errorf("omitted min moved the projection:\n%s\nwant:\n%s", got, want)
			}
			if implicit.ProtocolId != explicit.ProtocolId {
				t.Errorf("omitted min moved the protocol id: %#x != %#x", implicit.ProtocolId, explicit.ProtocolId)
			}
			got, err := golang.Generate(implicit)
			if err != nil {
				t.Fatal(err)
			}
			want, err := golang.Generate(explicit)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(want) {
				t.Fatalf("generated Go file count = %d, want %d", len(got), len(want))
			}
			for name, data := range want {
				if !bytes.Equal(got[name], data) {
					t.Errorf("omitted min changed generated Go file %s", name)
				}
			}
		})
	}
	for _, tc := range []struct {
		name, field, want string
	}{
		{"unsigned min only", "health uint32 | min = 0", "field health: min without max"},
		{"signed max only", "health int32 | max = 100000", "field health: max without min"},
		{"signed min only", "health int32 | min = 0", "field health: min without max"},
		{"unsigned negative max", "health uint32 | max = -1", "inverted range"},
		{"unsigned max outside storage", "health uint8 | max = 256", "does not fit its declared storage"},
		{"uint128 remains raw", "health uint128 | max = 100000", "not valid on uint128"},
		{"ufixed requires both bounds", "health ufixed(16, 16) | max = 1000", "min without max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			source := "package probe\ntype State { " + tc.field + " }\n"
			f, errs := parser.Parse("Probe.schema", []byte(source))
			if len(errs) != 0 {
				t.Fatalf("parse: %v", errs)
			}
			_, errs = check.Unit([]check.SourceFile{{Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(source), AST: f}})
			for _, err := range errs {
				if strings.Contains(err.Error(), tc.want) {
					return
				}
			}
			t.Fatalf("want refusal containing %q, got %v", tc.want, errs)
		})
	}
}
