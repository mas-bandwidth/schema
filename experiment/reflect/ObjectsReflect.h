// ObjectsReflect.h — HAND-WRITTEN STAND-IN for what the schema compiler would
// emit from Types.schema and Objects.schema. EXPERIMENT (issue #105).
//
// This is the leg that carries the ENUM and the NESTED-TYPE cases TestData does
// not reach.
//
// THINGS SCHEMA COULD NOT MECHANICALLY DERIVE — none. One member deserves a
// callout because C++ cannot recover it: `declared`. ShipData_Deep stores
// `::VecMath` (the cpp_native mapping, SPEC §4.2) but the WIRE is Vec3's. The
// storage type and the declared type are different types, and only the schema
// knows the pairing — a C++-side reflection library would have to be told.

#pragma once

#include "schema_reflect.h"

#include "Types.h"
#include "Objects.h"
#include "Enums.h"
#include "Constants.h"
#include "vec_math.h"

namespace example {
namespace reflect {

using schema_reflect::Kind;

// ---- type Vec3 -----------------------------------------------------------

struct Vec3_x { static constexpr Kind kind = Kind::Double; static constexpr auto ptr = &Vec3::x; SCHEMA_FIELD_REF( Vec3, x ) static constexpr const char * name = "x"; };
struct Vec3_y { static constexpr Kind kind = Kind::Double; static constexpr auto ptr = &Vec3::y; SCHEMA_FIELD_REF( Vec3, y ) static constexpr const char * name = "y"; };
struct Vec3_z { static constexpr Kind kind = Kind::Double; static constexpr auto ptr = &Vec3::z; SCHEMA_FIELD_REF( Vec3, z ) static constexpr const char * name = "z"; };

// ---- type Quat -----------------------------------------------------------

struct Quat_x { static constexpr Kind kind = Kind::Double; static constexpr auto ptr = &Quat::x; SCHEMA_FIELD_REF( Quat, x ) static constexpr const char * name = "x"; };
struct Quat_y { static constexpr Kind kind = Kind::Double; static constexpr auto ptr = &Quat::y; SCHEMA_FIELD_REF( Quat, y ) static constexpr const char * name = "y"; };
struct Quat_z { static constexpr Kind kind = Kind::Double; static constexpr auto ptr = &Quat::z; SCHEMA_FIELD_REF( Quat, z ) static constexpr const char * name = "z"; };
struct Quat_w { static constexpr Kind kind = Kind::Double; static constexpr auto ptr = &Quat::w; SCHEMA_FIELD_REF( Quat, w ) static constexpr const char * name = "w"; };

// ---- type Handle ---------------------------------------------------------

struct Handle_object_id       { static constexpr Kind kind = Kind::RangedInt;  static constexpr auto ptr = &Handle::object_id; SCHEMA_FIELD_REF( Handle, object_id )       static constexpr const char * name = "object_id";
                                static constexpr int64_t min = 0, max = MaxObjects - 1; };
struct Handle_object_sequence { static constexpr Kind kind = Kind::BitsNarrow; static constexpr auto ptr = &Handle::object_sequence; SCHEMA_FIELD_REF( Handle, object_sequence ) static constexpr const char * name = "object_sequence";
                                static constexpr int bits = 8; };

// ---- object view ShipData_Deep ------------------------------------------

struct Ship_ship_type       { static constexpr Kind kind = Kind::Enum;   static constexpr auto ptr = &ShipData_Deep::ship_type; SCHEMA_FIELD_REF( ShipData_Deep, ship_type )       static constexpr const char * name = "ship_type";       static constexpr int64_t max = 5; };
struct Ship_position        { static constexpr Kind kind = Kind::Nested; static constexpr auto ptr = &ShipData_Deep::position; SCHEMA_FIELD_REF( ShipData_Deep, position )        static constexpr const char * name = "position";        using declared = Vec3; };
struct Ship_rotation        { static constexpr Kind kind = Kind::Nested; static constexpr auto ptr = &ShipData_Deep::rotation; SCHEMA_FIELD_REF( ShipData_Deep, rotation )        static constexpr const char * name = "rotation";        using declared = Quat; };
struct Ship_linear_velocity { static constexpr Kind kind = Kind::Nested; static constexpr auto ptr = &ShipData_Deep::linear_velocity; SCHEMA_FIELD_REF( ShipData_Deep, linear_velocity ) static constexpr const char * name = "linear_velocity"; using declared = Vec3; };
struct Ship_flags           { static constexpr Kind kind = Kind::Bits;   static constexpr auto ptr = &ShipData_Deep::flags; SCHEMA_FIELD_REF( ShipData_Deep, flags )           static constexpr const char * name = "flags";           static constexpr int bits = 4; };
struct Ship_team            { static constexpr Kind kind = Kind::Enum;   static constexpr auto ptr = &ShipData_Deep::team; SCHEMA_FIELD_REF( ShipData_Deep, team )            static constexpr const char * name = "team";            static constexpr int64_t max = 2; };
struct Ship_health          { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::health; SCHEMA_FIELD_REF( ShipData_Deep, health )          static constexpr const char * name = "health"; };
struct Ship_thrust          { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::thrust; SCHEMA_FIELD_REF( ShipData_Deep, thrust )          static constexpr const char * name = "thrust"; };
struct Ship_angular_velocity{ static constexpr Kind kind = Kind::Nested; static constexpr auto ptr = &ShipData_Deep::angular_velocity; SCHEMA_FIELD_REF( ShipData_Deep, angular_velocity )static constexpr const char * name = "angular_velocity";using declared = Vec3; };
struct Ship_laser_cooldown  { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::laser_cooldown; SCHEMA_FIELD_REF( ShipData_Deep, laser_cooldown )  static constexpr const char * name = "laser_cooldown"; };
struct Ship_missile_cooldown{ static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::missile_cooldown; SCHEMA_FIELD_REF( ShipData_Deep, missile_cooldown )static constexpr const char * name = "missile_cooldown"; };
struct Ship_speed_current   { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::speed_current; SCHEMA_FIELD_REF( ShipData_Deep, speed_current )   static constexpr const char * name = "speed_current"; };
struct Ship_speed_velocity  { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::speed_velocity; SCHEMA_FIELD_REF( ShipData_Deep, speed_velocity )  static constexpr const char * name = "speed_velocity"; };
struct Ship_stick_current   { static constexpr Kind kind = Kind::Nested; static constexpr auto ptr = &ShipData_Deep::stick_current; SCHEMA_FIELD_REF( ShipData_Deep, stick_current )   static constexpr const char * name = "stick_current";   using declared = Vec3; };
struct Ship_stick_velocity  { static constexpr Kind kind = Kind::Nested; static constexpr auto ptr = &ShipData_Deep::stick_velocity; SCHEMA_FIELD_REF( ShipData_Deep, stick_velocity )  static constexpr const char * name = "stick_velocity";  using declared = Vec3; };
struct Ship_sens_current    { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::sensitivity_current; SCHEMA_FIELD_REF( ShipData_Deep, sensitivity_current )  static constexpr const char * name = "sensitivity_current"; };
struct Ship_sens_velocity   { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::sensitivity_velocity; SCHEMA_FIELD_REF( ShipData_Deep, sensitivity_velocity ) static constexpr const char * name = "sensitivity_velocity"; };
struct Ship_roll_current    { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::roll_current; SCHEMA_FIELD_REF( ShipData_Deep, roll_current )    static constexpr const char * name = "roll_current"; };
struct Ship_roll_velocity   { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::roll_velocity; SCHEMA_FIELD_REF( ShipData_Deep, roll_velocity )   static constexpr const char * name = "roll_velocity"; };
struct Ship_aim_current     { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::aim_current; SCHEMA_FIELD_REF( ShipData_Deep, aim_current )     static constexpr const char * name = "aim_current"; };
struct Ship_aim_velocity    { static constexpr Kind kind = Kind::Float;  static constexpr auto ptr = &ShipData_Deep::aim_velocity; SCHEMA_FIELD_REF( ShipData_Deep, aim_velocity )    static constexpr const char * name = "aim_velocity"; };
struct Ship_laser_index     { static constexpr Kind kind = Kind::RangedInt; static constexpr auto ptr = &ShipData_Deep::laser_index; SCHEMA_FIELD_REF( ShipData_Deep, laser_index )   static constexpr const char * name = "laser_index";
                              static constexpr int64_t min = 0, max = ShipMaxLasers - 1; };
struct Ship_missile_index   { static constexpr Kind kind = Kind::RangedInt; static constexpr auto ptr = &ShipData_Deep::missile_index; SCHEMA_FIELD_REF( ShipData_Deep, missile_index ) static constexpr const char * name = "missile_index";
                              static constexpr int64_t min = 0, max = ShipMaxMissiles - 1; };
struct Ship_target          { static constexpr Kind kind = Kind::Nested; static constexpr auto ptr = &ShipData_Deep::target; SCHEMA_FIELD_REF( ShipData_Deep, target )          static constexpr const char * name = "target";          using declared = Handle; };
struct Ship_lock_start_time { static constexpr Kind kind = Kind::Double; static constexpr auto ptr = &ShipData_Deep::lock_start_time; SCHEMA_FIELD_REF( ShipData_Deep, lock_start_time ) static constexpr const char * name = "lock_start_time"; };

} // namespace reflect
} // namespace example

namespace schema_reflect {

template <> struct Reflect<example::Vec3>
{
    static constexpr const char * name = "Vec3";
    using fields = FieldList< example::reflect::Vec3_x, example::reflect::Vec3_y, example::reflect::Vec3_z >;
};

template <> struct Reflect<example::Quat>
{
    static constexpr const char * name = "Quat";
    using fields = FieldList< example::reflect::Quat_x, example::reflect::Quat_y, example::reflect::Quat_z, example::reflect::Quat_w >;
};

template <> struct Reflect<example::Handle>
{
    static constexpr const char * name = "Handle";
    using fields = FieldList< example::reflect::Handle_object_id, example::reflect::Handle_object_sequence >;
};

template <> struct Reflect<example::ShipData_Deep>
{
    static constexpr const char * name = "ShipData_Deep";
    using fields = FieldList<
        example::reflect::Ship_ship_type,
        example::reflect::Ship_position,
        example::reflect::Ship_rotation,
        example::reflect::Ship_linear_velocity,
        example::reflect::Ship_flags,
        example::reflect::Ship_team,
        example::reflect::Ship_health,
        example::reflect::Ship_thrust,
        example::reflect::Ship_angular_velocity,
        example::reflect::Ship_laser_cooldown,
        example::reflect::Ship_missile_cooldown,
        example::reflect::Ship_speed_current,
        example::reflect::Ship_speed_velocity,
        example::reflect::Ship_stick_current,
        example::reflect::Ship_stick_velocity,
        example::reflect::Ship_sens_current,
        example::reflect::Ship_sens_velocity,
        example::reflect::Ship_roll_current,
        example::reflect::Ship_roll_velocity,
        example::reflect::Ship_aim_current,
        example::reflect::Ship_aim_velocity,
        example::reflect::Ship_laser_index,
        example::reflect::Ship_missile_index,
        example::reflect::Ship_target,
        example::reflect::Ship_lock_start_time >;
};

} // namespace schema_reflect
