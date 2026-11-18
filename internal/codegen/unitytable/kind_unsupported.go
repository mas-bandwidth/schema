package unitytable

import (
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// kindUnsupported claims every field no other codec does: a float, a 128-bit
// or timestamp integer, an enum, flags, a string, bytes, a nested table, a
// union, an array, a map, a pointer, an optional, a guarded or deprecated
// field. Its descriptor is emitted like any other, so the surface names every
// construct; it has no storage member and no wire code, and a table holding
// one carries no Load or Save until the card that carries the construct adds
// its own kind file (kinds.go, THE PER-KIND LAYOUT). The conformance driver
// answers such a root absent.
type kindUnsupported struct{}

func init() { register(kindUnsupported{}) }

func (kindUnsupported) name() string                     { return "unsupported" }
func (kindUnsupported) codec() int                       { return 0 }
func (kindUnsupported) priority() int                    { return 1 << 30 }
func (kindUnsupported) carried() bool                    { return false }
func (kindUnsupported) runtime() string                  { return "" }
func (kindUnsupported) claims(*ir.Field) bool            { return true }
func (kindUnsupported) storage(*ir.Field) string         { return "" }
func (kindUnsupported) defaultValue(*ir.Field) string    { return "" }
func (kindUnsupported) defaultBits(*ir.Field) uint64     { return 0 }
func (kindUnsupported) differs(*ir.Field, string) string { return "false" }
func (kindUnsupported) width(*ir.Field) int              { return 0 }

func (kindUnsupported) emitAsserts(*strings.Builder, *ir.Field, string) {}
func (kindUnsupported) emitPayload(*strings.Builder, *ir.Field, string) {}
func (kindUnsupported) emitRead(*strings.Builder, *ir.Field, string)    {}
