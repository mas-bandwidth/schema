// The second translation unit: includes every generated header again, so the
// link step proves the headers define no duplicate symbols across TUs.

#include "ArmDefaultsWire.h"
#include "ClausesWire.h"
#include "ConstantsWire.h"
#include "DegenerateWire.h"
#include "EnumsWire.h"
#include "JoinsWire.h"
#include "RenderWire.h"
#include "TypesWire.h"
#include "WireWire.h"


int touch_generated_types()
{
    using namespace example;
    Test test;
    ShipCreate create;
    Handle handle;
    return static_cast<int>(test.test_a) + create.has_flags + handle.object_id;
}
