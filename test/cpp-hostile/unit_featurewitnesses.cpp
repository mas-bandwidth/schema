// the featurewitnesses unit (testdata/conformance/feature-witnesses/full): a
// NAMED ABSENCE. The C++ target carries no text list (docs/SPEC-TABLES.md
// §2.13, compiler/carries_cpp.go), so it generates no code for the unit and
// every case of its root answers skip, naming why.
#include "hostile.h"

extern "C" const hc_binding_t * hc_bindings_featurewitnesses( int * count )
{
    static hc_binding_t row = {};
    row.schema = "testdata/conformance/feature-witnesses/full/Features.schema";
    row.table = "Root";
    row.absent = "the C++ target carries no text list (docs/SPEC-TABLES.md 2.13, compiler/carries_cpp.go)";
    *count = 1;
    return &row;
}
