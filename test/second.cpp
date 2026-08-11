// The second translation unit: includes every generated header again, so the
// link step proves the headers define no duplicate symbols across TUs.

#include "ConstantsWire.h"
#include "ContextsWire.h"
#include "EnumsWire.h"
#include "MessagesWire.h"
#include "ObjectsWire.h"
#include "RenderWire.h"
#include "TypesWire.h"
#include "WireWire.h"

// the table headers too: their inline functions must also be ODR-safe
#include "ConstantsTable.h"
#include "ContextsTable.h"
#include "EnumsTable.h"
#include "MessagesTable.h"
#include "ObjectsTable.h"
#include "RenderTable.h"
#include "TypesTable.h"
#include "WireTable.h"

int touch_generated_types()
{
    using namespace example;
    Synchronize sync;
    ShipCreate create;
    Handle handle;
    ClientMissileState missile;
    return static_cast<int>(sync.sync_frame) + create.has_flags + handle.object_id +
           static_cast<int>(missile.missile_type != MissileType::None);
}
