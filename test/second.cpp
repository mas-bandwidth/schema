// The second translation unit: includes every generated header again, so the
// link step proves the headers define no duplicate symbols across TUs.

#include "Constants.h"
#include "Contexts.h"
#include "Enums.h"
#include "Messages.h"
#include "Objects.h"
#include "Types.h"

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
