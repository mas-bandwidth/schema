// Includes every generated header, uses the generated types, and prints OK.
// second.cpp includes the same headers into a second translation unit, so a
// successful link also proves the headers are multiple-inclusion safe.

#include <cstdio>

#include "Constants.h"
#include "Contexts.h"
#include "Enums.h"
#include "Messages.h"
#include "Objects.h"
#include "Types.h"

// defined in second.cpp — proves cross-TU linkage over the same headers
int touch_generated_types();

int main()
{
    using namespace example;

    // constants fold and export (SPEC §4.2)
    static_assert(MaxPositionUnits == MaxWorldMeters * PositionUnits, "constants compose");
    static_assert(NumTeams == 3, "NumTeams = Team.Max + 1 (None rides the wire, Max = 2)");
    static_assert(ProtocolId != 0, "the unit has a protocol id");

    // enums: None = 0 implicit, variants dense from 1 (SPEC §4.2)
    static_assert(static_cast<int>(Team::None) == 0, "None = 0");
    static_assert(static_cast<int>(Team::Blue) == 2, "variants pack from 1");
    static_assert(static_cast<int>(MessageType::Block) == 1, "message tags sorted by name");
    static_assert(static_cast<int>(ObjectType::DynamicProp) == 1, "object tags sorted by name");

    // flags: one bit per variant from bit 0 (SPEC §4.2)
    static_assert(ShipFlags_FiringLaser == 1ull << 0, "flags bits assign in declaration order");
    static_assert(ShipFlags_Aiming == 1ull << 3, "flags bits assign in declaration order");

    // zero initialization is the rule (Glenn, 2026-08-05)
    RigidBody body;
    if (body.at_rest || body.linear_velocity.x != 0.0 || body.orientation.w != 0.0)
        return 1;

    ClientShipState client_ship;
    if (client_ship.predicted_explode || client_ship.num_colliders != 0)
        return 1;

    ServerShipState server_ship;
    if (server_ship.team != Team::None || server_ship.health != 0.0f)
        return 1;

    ServerMissileState missile;
    if (missile.timer != 0.0)
        return 1;

    ShipData_Deep deep;
    ShipData_Shallow shallow;
    ShipData_Interpolate interp;
    if (deep.lock_start_time != 0.0 || shallow.position_x != 0 || interp.health != 0)
        return 1;

    TurretState turret;
    DynamicPropState prop;
    if (turret.turret_index != 0 || prop.prop_type != PropType::None)
        return 1;

    Chat chat;
    chat.text_length = 0;
    Heartbeat heartbeat;
    (void)heartbeat;

    InputPacket packet;
    if (packet.inputs_count != 0)
        return 1;

    if (touch_generated_types() != 0)
        return 1;

    printf("OK\n");
    return 0;
}
