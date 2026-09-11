// THE MEASUREMENT THAT DECIDED THE FIXED FORM'S READER (docs/SPEC-TABLES.md
// §3.4, "held by test"). The ruling is that there is ONE reader path, and the
// price of that ruling is the distance between the plan-driven reader and
// straight-line code. This file is what measures the distance.
//
// READ B IS NOT A SECOND READER AND NO GENERATOR EMITS IT. It is hand-written
// here, one constant-offset load per leaf, for the sole purpose of standing
// beside the plan. The form-3 layout below is likewise hand-written against
// §3.4's constant-size table, so that the number is not the emitter grading
// its own homework.
//
// THE MEASUREMENT that decides the fixed-table form's reader.
//
// Same records, same host, best of N:
//
//   READ   A. the PLAN-DRIVEN reader running its IDENTITY PLAN
//          B. STRAIGHT-LINE constant-offset loads, written as a generator
//             would emit them directly
//   WRITE  C. the TEMPLATE write: one memcpy of the type's constant bytes,
//             then value stores at constant offsets
//          D. today's form-1 write (BenchMixedSave)
//
// The unit is the paired unit: bench/corpus/Bench.schema's BenchMixed, the
// same 64 logical records the paired corpus carries, decoded from the
// canonical packet corpus so no value here is independently generated.
#include <chrono>
#include <cstdint>
#include <cstdio>
#include <cstring>
#include <vector>
#include "BenchWire.h"
#include "BenchTable.h"
#include "FixedTableTable.h"

using namespace bench;

// ---------------------------------------------------------------------------
// THE FORM-3 RECORD, by hand, exactly as the spec section states it: an
// 8-byte layout hash, then every field at its declared storage width in
// declared order, nothing padded between fields.
// ---------------------------------------------------------------------------

enum : int32_t {
    CEntity = 51, CStat = 8, CHit = 13, CChat = 8, CPickup = 8,
    CEvent  = 1 + CHit,
    CMixed  = 1236,
    RecordBytes = 8 + CMixed,
};

// MixedEntity, 51 bytes
enum : int32_t { E_entity_id=0, E_pos_x=4, E_pos_y=8, E_pos_z=12, E_yaw=16,
                 E_pitch=20, E_vel_x=24, E_vel_y=28, E_vel_z=32, E_health=36,
                 E_weapon=40, E_damage=41, E_moving=49, E_firing=50 };
// MixedStat, 8 bytes
enum : int32_t { S_stat_id=0, S_delta=4 };
// arms
enum : int32_t { H_target=0, H_damage=4, H_kind=8, H_crit=12 };
enum : int32_t { C_channel=0, C_speaker=4 };
enum : int32_t { P_item=0, P_amount=4 };

// BenchMixed, 1236 bytes
enum : int32_t {
    W_sequence=0, W_ack_sequence=4, W_ack_bits=8, W_session_id=12,
    W_client_id=20, W_nonce=24, W_world_time=32, W_frame_tick=40,
    W_server_time=48,
    W_entities_count=52, W_entities=56,
    W_stats_count=464,   W_stats=468,
    W_game_event=1108,
    W_loadout=1122,
    W_name_len=1126, W_name=1130,
    W_payload_len=1145, W_payload=1149,
    W_aim_x=1165, W_aim_y=1169, W_aim_z=1173, W_recoil=1177, W_drift=1181,
    W_wide_key=1189, W_flux=1205, W_ping=1221, W_crc_hint=1223,
    W_has_extra=1227, W_extra=1228, W_idle_ticks=1232,
};

static_assert( W_idle_ticks + 4 == CMixed, "the constant size is the sum of the field constants" );

// ---------------------------------------------------------------------------
// THE WRITE, form 3: the template memcpy'd, then value stores.
// ---------------------------------------------------------------------------

alignas( 8 ) static uint8_t g_template[RecordBytes];   // the type's constant bytes
static uint64_t g_hash = 0;

template <typename T> static inline void st( uint8_t * p, int32_t at, T v )
{
    std::memcpy( p + at, &v, sizeof( T ) );
}
template <typename T> static inline T ld( const uint8_t * p, int32_t at )
{
    T v; std::memcpy( &v, p + at, sizeof( T ) ); return v;
}

static void f3_write( const BenchMixed & value, uint8_t * out )
{
    std::memcpy( out, g_template, RecordBytes );      // the constant bytes
    uint8_t * b = out + 8;                            // the body
    st<uint32_t>( b, W_sequence, value.sequence );
    st<int32_t>( b, W_ack_sequence, value.ack_sequence );
    st<uint32_t>( b, W_ack_bits, value.ack_bits );
    st<uint64_t>( b, W_session_id, value.session_id );
    st<uint32_t>( b, W_client_id, value.client_id );
    st<uint64_t>( b, W_nonce, value.nonce );
    st<int64_t>( b, W_world_time, value.world_time );
    st<uint64_t>( b, W_frame_tick, value.frame_tick );
    st<int32_t>( b, W_server_time, value.server_time );
    st<int32_t>( b, W_entities_count, value.entities_count );
    for ( int i = 0; i < 8; ++i )
    {
        uint8_t * e = b + W_entities + i * CEntity;
        const MixedEntity & s = value.entities[i];
        st<uint32_t>( e, E_entity_id, s.entity_id );
        st<int32_t>( e, E_pos_x, s.pos_x ); st<int32_t>( e, E_pos_y, s.pos_y ); st<int32_t>( e, E_pos_z, s.pos_z );
        st<uint32_t>( e, E_yaw, s.yaw ); st<uint32_t>( e, E_pitch, s.pitch );
        st<int32_t>( e, E_vel_x, s.vel_x ); st<int32_t>( e, E_vel_y, s.vel_y ); st<int32_t>( e, E_vel_z, s.vel_z );
        st<int32_t>( e, E_health, s.health );
        st<uint8_t>( e, E_weapon, (uint8_t) s.weapon );
        st<uint64_t>( e, E_damage, s.damage );
        st<uint8_t>( e, E_moving, s.moving ? 1 : 0 );
        st<uint8_t>( e, E_firing, s.firing ? 1 : 0 );
    }
    st<int32_t>( b, W_stats_count, value.stats_count );
    for ( int i = 0; i < 80; ++i )
    {
        uint8_t * e = b + W_stats + i * CStat;
        st<uint32_t>( e, S_stat_id, value.stats[i].stat_id );
        st<int32_t>( e, S_delta, value.stats[i].delta );
    }
    {
        uint8_t * u = b + W_game_event;
        st<uint8_t>( u, 0, (uint8_t) value.game_event.type );
        switch ( value.game_event.type )
        {
            case MixedEventType::Hit:
                st<uint32_t>( u + 1, H_target, value.game_event.hit.target_id );
                st<int32_t>( u + 1, H_damage, value.game_event.hit.damage );
                st<int32_t>( u + 1, H_kind, value.game_event.hit.hit_kind );
                st<uint8_t>( u + 1, H_crit, value.game_event.hit.crit ? 1 : 0 );
                break;
            case MixedEventType::Chat:
                st<int32_t>( u + 1, C_channel, value.game_event.chat.channel );
                st<uint32_t>( u + 1, C_speaker, value.game_event.chat.speaker );
                break;
            case MixedEventType::Pickup:
                st<uint32_t>( u + 1, P_item, value.game_event.pickup.item_id );
                st<int32_t>( u + 1, P_amount, value.game_event.pickup.amount );
                break;
            default: break;
        }
    }
    std::memcpy( b + W_loadout, value.loadout, 4 );
    st<int32_t>( b, W_name_len, value.player_name_length );
    std::memcpy( b + W_name, value.player_name, 15 );
    st<int32_t>( b, W_payload_len, value.payload_length );
    std::memcpy( b + W_payload, value.payload, 16 );
    st<float>( b, W_aim_x, value.aim_x ); st<float>( b, W_aim_y, value.aim_y ); st<float>( b, W_aim_z, value.aim_z );
    st<float>( b, W_recoil, value.recoil );
    st<double>( b, W_drift, value.drift );
    std::memcpy( b + W_wide_key, &value.wide_key, 16 );
    std::memcpy( b + W_flux, &value.flux, 16 );
    st<uint16_t>( b, W_ping, value.ping );
    st<uint32_t>( b, W_crc_hint, value.crc_hint );
    st<uint8_t>( b, W_has_extra, value.has_extra ? 1 : 0 );
    st<int32_t>( b, W_extra, value.extra );
    st<int32_t>( b, W_idle_ticks, value.idle_ticks );
}

// ---------------------------------------------------------------------------
// READ B: STRAIGHT-LINE constant-offset loads, as a generator would emit them.
// ---------------------------------------------------------------------------

static void f3_read_straight( const uint8_t * rec, BenchMixed & value )
{
    const uint8_t * b = rec + 8;
    value.sequence = ld<uint32_t>( b, W_sequence );
    value.ack_sequence = ld<int32_t>( b, W_ack_sequence );
    value.ack_bits = ld<uint32_t>( b, W_ack_bits );
    value.session_id = ld<uint64_t>( b, W_session_id );
    value.client_id = ld<uint32_t>( b, W_client_id );
    value.nonce = ld<uint64_t>( b, W_nonce );
    value.world_time = ld<int64_t>( b, W_world_time );
    value.frame_tick = ld<uint64_t>( b, W_frame_tick );
    value.server_time = ld<int32_t>( b, W_server_time );
    {
        int32_t n = ld<int32_t>( b, W_entities_count );
        value.entities_count = n < 0 ? 0 : ( n > 8 ? 8 : n );
    }
    for ( int i = 0; i < 8; ++i )
    {
        const uint8_t * e = b + W_entities + i * CEntity;
        MixedEntity & d = value.entities[i];
        d.entity_id = ld<uint32_t>( e, E_entity_id );
        d.pos_x = ld<int32_t>( e, E_pos_x ); d.pos_y = ld<int32_t>( e, E_pos_y ); d.pos_z = ld<int32_t>( e, E_pos_z );
        d.yaw = ld<uint32_t>( e, E_yaw ); d.pitch = ld<uint32_t>( e, E_pitch );
        d.vel_x = ld<int32_t>( e, E_vel_x ); d.vel_y = ld<int32_t>( e, E_vel_y ); d.vel_z = ld<int32_t>( e, E_vel_z );
        d.health = ld<int32_t>( e, E_health );
        d.weapon = (MixedWeapon) ld<uint8_t>( e, E_weapon );
        d.damage = ld<uint64_t>( e, E_damage );
        d.moving = ld<uint8_t>( e, E_moving ) != 0;
        d.firing = ld<uint8_t>( e, E_firing ) != 0;
    }
    {
        int32_t n = ld<int32_t>( b, W_stats_count );
        value.stats_count = n < 0 ? 0 : ( n > 80 ? 80 : n );
    }
    for ( int i = 0; i < 80; ++i )
    {
        const uint8_t * e = b + W_stats + i * CStat;
        value.stats[i].stat_id = ld<uint32_t>( e, S_stat_id );
        value.stats[i].delta = ld<int32_t>( e, S_delta );
    }
    {
        const uint8_t * u = b + W_game_event;
        value.game_event.type = (MixedEventType) ld<uint8_t>( u, 0 );
        switch ( value.game_event.type )
        {
            case MixedEventType::Hit:
                value.game_event.hit.target_id = ld<uint32_t>( u + 1, H_target );
                value.game_event.hit.damage = ld<int32_t>( u + 1, H_damage );
                value.game_event.hit.hit_kind = ld<int32_t>( u + 1, H_kind );
                value.game_event.hit.crit = ld<uint8_t>( u + 1, H_crit ) != 0;
                break;
            case MixedEventType::Chat:
                value.game_event.chat.channel = ld<int32_t>( u + 1, C_channel );
                value.game_event.chat.speaker = ld<uint32_t>( u + 1, C_speaker );
                break;
            case MixedEventType::Pickup:
                value.game_event.pickup.item_id = ld<uint32_t>( u + 1, P_item );
                value.game_event.pickup.amount = ld<int32_t>( u + 1, P_amount );
                break;
            default: break;
        }
    }
    std::memcpy( value.loadout, b + W_loadout, 4 );
    {
        int32_t n = ld<int32_t>( b, W_name_len );
        n = n < 0 ? 0 : ( n > 15 ? 15 : n );
        value.player_name_length = n;
        std::memcpy( value.player_name, b + W_name, 15 );
        value.player_name[n] = 0;
    }
    {
        int32_t n = ld<int32_t>( b, W_payload_len );
        n = n < 0 ? 0 : ( n > 16 ? 16 : n );
        value.payload_length = n;
        std::memcpy( value.payload, b + W_payload, 16 );
    }
    value.aim_x = ld<float>( b, W_aim_x ); value.aim_y = ld<float>( b, W_aim_y ); value.aim_z = ld<float>( b, W_aim_z );
    value.recoil = ld<float>( b, W_recoil );
    value.drift = ld<double>( b, W_drift );
    std::memcpy( &value.wide_key, b + W_wide_key, 16 );
    std::memcpy( &value.flux, b + W_flux, 16 );
    value.ping = ld<uint16_t>( b, W_ping );
    value.crc_hint = ld<uint32_t>( b, W_crc_hint );
    value.has_extra = ld<uint8_t>( b, W_has_extra ) != 0;
    value.extra = ld<int32_t>( b, W_extra );
    value.idle_ticks = ld<int32_t>( b, W_idle_ticks );
}

// ---------------------------------------------------------------------------
// READ A: THE ONE PLAN-DRIVEN PATH.
//
// A plan is a flat array of entries. An entry is a source offset, a size, a
// destination offset and an op. The identity plan carries COPY entries with
// adjacent runs coalesced, and the four ops below are the whole set this unit
// needs; a compiled plan is the same array with the same loop over it.
// ---------------------------------------------------------------------------

enum PlanOp : uint8_t { OpCopy = 0, OpCount = 1, OpText = 2, OpUnion = 3 };

// A RUN COPY. Every run is a known, small number of bytes and the widths a
// record's runs take are few, so the copy is written as overlapping unaligned
// word moves rather than as a call into libc for each entry.
static inline void copy_run( uint8_t * d, const uint8_t * s, uint32_t n )
{
    if ( n <= 16 )
    {
        if ( n >= 8 )
        {
            uint64_t a, b;
            std::memcpy( &a, s, 8 ); std::memcpy( &b, s + n - 8, 8 );
            std::memcpy( d, &a, 8 ); std::memcpy( d + n - 8, &b, 8 );
        }
        else if ( n >= 4 )
        {
            uint32_t a, b;
            std::memcpy( &a, s, 4 ); std::memcpy( &b, s + n - 4, 4 );
            std::memcpy( d, &a, 4 ); std::memcpy( d + n - 4, &b, 4 );
        }
        else if ( n )
        {
            d[0] = s[0]; d[n >> 1] = s[n >> 1]; d[n - 1] = s[n - 1];
        }
        return;
    }
    if ( n <= 64 )
    {
        uint64_t w[4];
        std::memcpy( w, s, 16 ); std::memcpy( d, w, 16 );
        std::memcpy( w, s + 16, 16 ); std::memcpy( d + 16, w, 16 );
        std::memcpy( w, s + n - 32, 32 ); std::memcpy( d + n - 32, w, 32 );
        return;
    }
    std::memcpy( d, s, n );
}

struct PlanEntry
{
    uint32_t src;
    uint32_t dst;
    uint32_t size;   // COPY: bytes.  COUNT/TEXT: the bound.  UNION: sub-plan base
    uint32_t aux;    // TEXT: the buffer's destination.  UNION: arm count
    uint8_t  op;
};

struct Plan
{
    const PlanEntry * entries;
    uint32_t count;
};

static PlanEntry g_root[128];
static uint32_t  g_root_count = 0;
static PlanEntry g_arms[3][8];
static uint32_t  g_arm_count[3] = { 0, 0, 0 };
static uint32_t  g_arm_dst[3] = { 0, 0, 0 };

static inline void run_plan( const PlanEntry * e, uint32_t n, const uint8_t * src, uint8_t * dst )
{
    for ( uint32_t i = 0; i < n; ++i )
    {
        const PlanEntry & p = e[i];
        switch ( p.op )
        {
            case OpCopy:
                copy_run( dst + p.dst, src + p.src, p.size );
                break;
            case OpCount:
            {
                int32_t v; std::memcpy( &v, src + p.src, 4 );
                v = v < 0 ? 0 : ( v > (int32_t) p.size ? (int32_t) p.size : v );
                std::memcpy( dst + p.dst, &v, 4 );
                break;
            }
            case OpText:
            {
                int32_t v; std::memcpy( &v, src + p.src, 4 );
                v = v < 0 ? 0 : ( v > (int32_t) p.size ? (int32_t) p.size : v );
                std::memcpy( dst + p.dst, &v, 4 );
                copy_run( dst + p.aux, src + p.src + 4, p.size );
                dst[ p.aux + (uint32_t) v ] = 0;
                break;
            }
            case OpUnion:
            {
                const uint8_t tag = src[ p.src ];
                std::memcpy( dst + p.dst, &tag, 1 );
                if ( tag >= 1 && tag <= p.aux )
                {
                    const uint32_t arm = tag - 1u;
                    run_plan( g_arms[arm], g_arm_count[arm], src + p.src + 1, dst + g_arm_dst[arm] );
                }
                break;
            }
            default: break;
        }
    }
}

static void f3_read_plan( const uint8_t * rec, BenchMixed & value )
{
    run_plan( g_root, g_root_count, rec + 8, (uint8_t *) &value );
}

// PATH A AS SHIPPED: the generated identity plan, run by the generated loop.
// This is the reader the ruling is about; the interpreter above is kept beside
// it only so the harness can check the two agree.
static TableReport g_report;
// the same plan in RUNTIME storage: the shipped one is a constexpr array, and
// this is how the harness tells "the runtime's code" apart from "what the
// compiler does when it can see the plan".
static std::vector<TableFixedEntry> g_runtime_plan;
static inline void f3_read_shipped( const uint8_t * rec, FixedTable & value )
{
    TableFixedRun( FixedTableFixedPlan, FixedTableFixedPlanCount, FixedTableFixedPlanGuarded, rec + 8, (uint8_t *) &value, (uint32_t) sizeof( value ), &g_report );
}

// --- building the identity plan, once, exactly as the generator would bake it

struct Leaf { uint32_t src, dst, size; };
static std::vector<Leaf> g_leaves;
static void leaf( uint32_t s, uint32_t d, uint32_t n ) { g_leaves.push_back( Leaf{ s, d, n } ); }

#define OFF( f ) ( (uint32_t) __builtin_offsetof( BenchMixed, f ) )
#define EOFF( f ) ( (uint32_t) __builtin_offsetof( MixedEntity, f ) )
#define SOFF( f ) ( (uint32_t) __builtin_offsetof( MixedStat, f ) )

static void emit_copy_run_list()
{
    // COALESCE adjacent leaves whose source and destination both advance
    // together — the only optimization the plan compiler performs.
    for ( size_t i = 0; i < g_leaves.size(); ++i )
    {
        if ( g_root_count && g_root[g_root_count-1].op == OpCopy && i > 0 &&
             g_root[g_root_count-1].src + g_root[g_root_count-1].size == g_leaves[i].src &&
             g_root[g_root_count-1].dst + g_root[g_root_count-1].size == g_leaves[i].dst )
        {
            g_root[g_root_count-1].size += g_leaves[i].size;
            continue;
        }
        g_root[g_root_count++] = PlanEntry{ g_leaves[i].src, g_leaves[i].dst, g_leaves[i].size, 0, OpCopy };
    }
    g_leaves.clear();
}

static void flush_and( PlanEntry e )
{
    emit_copy_run_list();
    g_root[g_root_count++] = e;
}

static void build_identity_plan()
{
    // fields 1..9: scalars
    leaf( W_sequence, OFF( sequence ), 4 );
    leaf( W_ack_sequence, OFF( ack_sequence ), 4 );
    leaf( W_ack_bits, OFF( ack_bits ), 4 );
    leaf( W_session_id, OFF( session_id ), 8 );
    leaf( W_client_id, OFF( client_id ), 4 );
    leaf( W_nonce, OFF( nonce ), 8 );
    leaf( W_world_time, OFF( world_time ), 8 );
    leaf( W_frame_tick, OFF( frame_tick ), 8 );
    leaf( W_server_time, OFF( server_time ), 4 );
    // entities: 8 elements, each a nested fixed table inline
    for ( uint32_t i = 0; i < 8; ++i )
    {
        const uint32_t s = W_entities + i * CEntity;
        const uint32_t d = OFF( entities ) + i * (uint32_t) sizeof( MixedEntity );
        leaf( s + E_entity_id, d + EOFF( entity_id ), 4 );
        leaf( s + E_pos_x, d + EOFF( pos_x ), 4 );
        leaf( s + E_pos_y, d + EOFF( pos_y ), 4 );
        leaf( s + E_pos_z, d + EOFF( pos_z ), 4 );
        leaf( s + E_yaw, d + EOFF( yaw ), 4 );
        leaf( s + E_pitch, d + EOFF( pitch ), 4 );
        leaf( s + E_vel_x, d + EOFF( vel_x ), 4 );
        leaf( s + E_vel_y, d + EOFF( vel_y ), 4 );
        leaf( s + E_vel_z, d + EOFF( vel_z ), 4 );
        leaf( s + E_health, d + EOFF( health ), 4 );
        leaf( s + E_weapon, d + EOFF( weapon ), 1 );
        leaf( s + E_damage, d + EOFF( damage ), 8 );
        leaf( s + E_moving, d + EOFF( moving ), 1 );
        leaf( s + E_firing, d + EOFF( firing ), 1 );
    }
    flush_and( PlanEntry{ W_entities_count, OFF( entities_count ), 8, 0, OpCount } );
    for ( uint32_t i = 0; i < 80; ++i )
    {
        const uint32_t s = W_stats + i * CStat;
        const uint32_t d = OFF( stats ) + i * (uint32_t) sizeof( MixedStat );
        leaf( s + S_stat_id, d + SOFF( stat_id ), 4 );
        leaf( s + S_delta, d + SOFF( delta ), 4 );
    }
    flush_and( PlanEntry{ W_stats_count, OFF( stats_count ), 80, 0, OpCount } );
    // the union
    g_arm_dst[0] = (uint32_t) __builtin_offsetof( MixedEvent, hit );
    g_arm_dst[1] = (uint32_t) __builtin_offsetof( MixedEvent, chat );
    g_arm_dst[2] = (uint32_t) __builtin_offsetof( MixedEvent, pickup );
    g_arm_dst[0] += OFF( game_event ); g_arm_dst[1] += OFF( game_event ); g_arm_dst[2] += OFF( game_event );
    g_arms[0][0] = PlanEntry{ H_target, (uint32_t) __builtin_offsetof( MixedHitEvent, target_id ), 12, 0, OpCopy };
    g_arms[0][1] = PlanEntry{ H_crit, (uint32_t) __builtin_offsetof( MixedHitEvent, crit ), 1, 0, OpCopy };
    g_arm_count[0] = 2;
    g_arms[1][0] = PlanEntry{ C_channel, (uint32_t) __builtin_offsetof( MixedChatEvent, channel ), 8, 0, OpCopy };
    g_arm_count[1] = 1;
    g_arms[2][0] = PlanEntry{ P_item, (uint32_t) __builtin_offsetof( MixedPickupEvent, item_id ), 8, 0, OpCopy };
    g_arm_count[2] = 1;
    flush_and( PlanEntry{ W_game_event, OFF( game_event ) + (uint32_t) __builtin_offsetof( MixedEvent, type ), 0, 3, OpUnion } );
    leaf( W_loadout, OFF( loadout ), 4 );
    flush_and( PlanEntry{ W_name_len, OFF( player_name_length ), 15, OFF( player_name ), OpText } );
    flush_and( PlanEntry{ W_payload_len, OFF( payload_length ), 16, OFF( payload ), OpText } );
    leaf( W_aim_x, OFF( aim_x ), 4 );
    leaf( W_aim_y, OFF( aim_y ), 4 );
    leaf( W_aim_z, OFF( aim_z ), 4 );
    leaf( W_recoil, OFF( recoil ), 4 );
    leaf( W_drift, OFF( drift ), 8 );
    leaf( W_wide_key, OFF( wide_key ), 16 );
    leaf( W_flux, OFF( flux ), 16 );
    leaf( W_ping, OFF( ping ), 2 );
    leaf( W_crc_hint, OFF( crc_hint ), 4 );
    leaf( W_has_extra, OFF( has_extra ), 1 );
    leaf( W_extra, OFF( extra ), 4 );
    leaf( W_idle_ticks, OFF( idle_ticks ), 4 );
    emit_copy_run_list();
}

// ---------------------------------------------------------------------------

static bool read_file( const char * path, std::vector<uint8_t> & data )
{
    FILE * f = std::fopen( path, "rb" );
    if ( !f ) return false;
    uint8_t b[65536]; size_t n;
    while ( ( n = std::fread( b, 1, sizeof( b ), f ) ) != 0 ) data.insert( data.end(), b, b + n );
    const bool ok = !std::ferror( f );
    std::fclose( f );
    return ok;
}

static bool same( const BenchMixed & a, const BenchMixed & b )
{
    return std::memcmp( &a, &b, sizeof( BenchMixed ) ) == 0;
}

typedef std::chrono::steady_clock clk;

int main( int argc, char ** argv )
{
    const int reps = argc > 1 ? std::atoi( argv[1] ) : 2000;
    const int best_of = argc > 2 ? std::atoi( argv[2] ) : 9;

    std::vector<uint8_t> packet;
    if ( !read_file( "bench/corpus/variants/bench_mixed.variants.bin", packet ) || packet.size() % 438 )
    { std::fprintf( stderr, "no corpus\n" ); return 1; }
    const size_t stride = 438, count = packet.size() / stride;
    if ( count != 64 ) { std::fprintf( stderr, "want 64 records, have %zu\n", count ); return 1; }

    std::vector<BenchMixed> records( count );
    for ( size_t k = 0; k < count; ++k )
    {
        alignas( 8 ) uint8_t src[512] = {};
        std::memcpy( src, packet.data() + k * stride, stride );
        serialize::ReadStream rs( src, (int) stride );
        if ( !ReadBenchMixed( rs, records[k] ) ) { std::fprintf( stderr, "packet %zu\n", k ); return 1; }
    }

    build_identity_plan();

    // the constant bytes of the type: the hash, and zero everywhere a value lands
    g_hash = 0x0123456789abcdefull;
    std::memset( g_template, 0, sizeof( g_template ) );
    std::memcpy( g_template, &g_hash, 8 );

    // ---- THE HAND LAYOUT AND THE EMITTER MUST AGREE, BYTE FOR BYTE ----
    //
    // The layout above is written by hand from §3.4's constant-size table, and
    // the generated writer is written by the emitter from the same table. If
    // they disagree, one of the two is wrong and the measurement below is
    // measuring nothing.
    {
        std::vector<FixedTable> wrapped( count );
        for ( size_t k = 0; k < count; ++k ) wrapped[k].value = records[k];
        std::vector<uint8_t> file( (size_t) FixedTableFixedMeasure( (int64_t) count ) );
        if ( FixedTableFixedSave( wrapped.data(), (int64_t) count, file.data(), (int64_t) file.size() ) != (int64_t) file.size() )
        { std::fprintf( stderr, "the emitter's save refused\n" ); return 1; }
        if ( (int64_t) RecordBytes != FixedTableFixedRecordBytes || (int64_t) CMixed != FixedTableFixedBodyBytes )
        { std::fprintf( stderr, "hand layout %d/%d, emitter %lld/%lld\n", (int) RecordBytes, (int) CMixed,
                        (long long) FixedTableFixedRecordBytes, (long long) FixedTableFixedBodyBytes ); return 1; }
        g_hash = FixedTableFixedHash;
        std::memset( g_template, 0, sizeof( g_template ) );
        std::memcpy( g_template, &g_hash, 8 );
        const uint8_t * first = file.data() + kTableFixedHeaderBytes + 4 + FixedTableFixedLayoutBytes;
        for ( size_t k = 0; k < count; ++k )
        {
            alignas( 8 ) uint8_t mine[RecordBytes];
            f3_write( records[k], mine );
            if ( std::memcmp( mine, first + k * RecordBytes, RecordBytes ) )
            {
                for ( int q = 0; q < RecordBytes; ++q )
                    if ( mine[q] != first[k * RecordBytes + q] )
                    { std::fprintf( stderr, "record %zu: hand layout and emitter differ at body offset %d\n", k, q - 8 ); break; }
                return 1;
            }
        }
        std::printf( "hand layout == emitter, %zu records, %d bytes each\n", count, (int) RecordBytes );
    }

    // ---- correctness first: the two readers must agree, and round trip ----
    std::vector<uint8_t> wire( count * RecordBytes );
    for ( size_t k = 0; k < count; ++k ) f3_write( records[k], wire.data() + k * RecordBytes );
    for ( size_t k = 0; k < count; ++k )
    {
        // The two readers agree, and each round trips, judged on the WIRE —
        // a struct's padding is not a value and memcmp over it is not a test.
        BenchMixed a, b;
        BenchMixedReset( a ); BenchMixedReset( b );
        f3_read_straight( wire.data() + k * RecordBytes, a );
        f3_read_plan( wire.data() + k * RecordBytes, b );
        alignas( 8 ) uint8_t ra[RecordBytes], rb[RecordBytes];
        f3_write( a, ra ); f3_write( b, rb );
        if ( std::memcmp( ra, rb, RecordBytes ) ) { for ( int q = 0; q < RecordBytes; ++q ) if ( ra[q] != rb[q] ) { std::fprintf( stderr, "record %zu: readers disagree at body offset %d (%02x vs %02x)\n", k, q - 8, ra[q], rb[q] ); break; } return 1; }
        if ( std::memcmp( ra, wire.data() + k * RecordBytes, RecordBytes ) ) { std::fprintf( stderr, "record %zu: form 3 round trip lost the value\n", k ); return 1; }
    }
    (void) same;

    std::printf( "plan entries (identity, root): %u\n", g_root_count );
    std::printf( "record bytes: %d  (8 hash + %d body); packet 438\n", (int) RecordBytes, (int) CMixed );

    // ---- the timings ----
    volatile uint64_t sink = 0;
    double read_plan = 1e30, read_straight = 1e30, write_template = 1e30, write_form1 = 1e30, read_form1 = 1e30, read_interp = 1e30, read_runtime = 1e30;
    g_runtime_plan.assign( FixedTableFixedPlan, FixedTableFixedPlan + FixedTableFixedPlanCount );
    // today's form-1 wire for the same records, for the reference row
    std::vector<uint8_t> f1( count * 4096 );
    std::vector<int64_t> f1len( count );
    for ( size_t k = 0; k < count; ++k )
    { f1len[k] = BenchMixedSave( records[k], f1.data() + k * 4096, 4096 ); if ( f1len[k] <= 0 ) return 1; }
    std::vector<BenchMixed> out( count );
    std::vector<uint8_t> scratch( count * 4096 );

    for ( int t = 0; t < best_of; ++t )
    {
        {   // A. plan-driven, the SHIPPED identity plan and the SHIPPED loop
            auto t0 = clk::now();
            for ( int r = 0; r < reps; ++r )
                for ( size_t k = 0; k < count; ++k )
                    f3_read_shipped( wire.data() + k * RecordBytes, *(FixedTable *) &out[k] );
            auto t1 = clk::now();
            sink += out[0].session_id;
            double ns = std::chrono::duration<double, std::nano>( t1 - t0 ).count() / ( reps * (double) count );
            if ( ns < read_plan ) read_plan = ns;
        }
        {   // A3. the SHIPPED loop over a RUNTIME copy of the same plan
            auto t0 = clk::now();
            for ( int r = 0; r < reps; ++r )
                for ( size_t k = 0; k < count; ++k )
                    TableFixedRun( g_runtime_plan.data(), (int32_t) g_runtime_plan.size(), FixedTableFixedPlanGuarded,
                                   wire.data() + k * RecordBytes + 8, (uint8_t *) &out[k], (uint32_t) sizeof( out[k] ), &g_report );
            auto t1 = clk::now();
            sink += out[0].session_id;
            double ns = std::chrono::duration<double, std::nano>( t1 - t0 ).count() / ( reps * (double) count );
            if ( ns < read_runtime ) read_runtime = ns;
        }
        {   // A2. the same plan through this file's own interpreter, which is
            // where the harness and the shipped runtime can be told apart
            auto t0 = clk::now();
            for ( int r = 0; r < reps; ++r )
                for ( size_t k = 0; k < count; ++k )
                    f3_read_plan( wire.data() + k * RecordBytes, out[k] );
            auto t1 = clk::now();
            sink += out[0].session_id;
            double ns = std::chrono::duration<double, std::nano>( t1 - t0 ).count() / ( reps * (double) count );
            if ( ns < read_interp ) read_interp = ns;
        }
        {   // B. straight-line constant-offset loads
            auto t0 = clk::now();
            for ( int r = 0; r < reps; ++r )
                for ( size_t k = 0; k < count; ++k )
                    f3_read_straight( wire.data() + k * RecordBytes, out[k] );
            auto t1 = clk::now();
            sink += out[0].session_id;
            double ns = std::chrono::duration<double, std::nano>( t1 - t0 ).count() / ( reps * (double) count );
            if ( ns < read_straight ) read_straight = ns;
        }
        {   // C. the template write
            auto t0 = clk::now();
            for ( int r = 0; r < reps; ++r )
                for ( size_t k = 0; k < count; ++k )
                    f3_write( records[k], scratch.data() + k * RecordBytes );
            auto t1 = clk::now();
            sink += scratch[0];
            double ns = std::chrono::duration<double, std::nano>( t1 - t0 ).count() / ( reps * (double) count );
            if ( ns < write_template ) write_template = ns;
        }
        {   // D. today's form-1 write
            auto t0 = clk::now();
            for ( int r = 0; r < reps; ++r )
                for ( size_t k = 0; k < count; ++k )
                    sink += (uint64_t) BenchMixedSave( records[k], scratch.data() + k * 4096, 4096 );
            auto t1 = clk::now();
            double ns = std::chrono::duration<double, std::nano>( t1 - t0 ).count() / ( reps * (double) count );
            if ( ns < write_form1 ) write_form1 = ns;
        }
    }

    for ( int t = 0; t < best_of; ++t )
    {   // reference: today's form-1 read
        auto t0 = clk::now();
        for ( int r = 0; r < reps; ++r )
            for ( size_t k = 0; k < count; ++k )
            { TableReport rep; BenchMixedLoad( out[k], f1.data() + k * 4096, f1len[k], &rep ); sink += rep.unknown; }
        auto t1 = clk::now();
        double ns = std::chrono::duration<double, std::nano>( t1 - t0 ).count() / ( reps * (double) count );
        if ( ns < read_form1 ) read_form1 = ns;
    }

    std::printf( "\nREAD   A plan-driven (identity)   %8.1f ns/record\n", read_plan );
    std::printf( "READ   A3 shipped loop, runtime plan %8.1f ns/record\n", read_runtime );
    std::printf( "READ   A2 the harness's interpreter %8.1f ns/record\n", read_interp );
    std::printf( "READ   B straight-line             %8.1f ns/record\n", read_straight );
    std::printf( "       ratio A/B                   %8.3fx\n", read_plan / read_straight );
    std::printf( "READ   ref: form 1 (today)         %8.1f ns/record\n", read_form1 );
    std::printf( "\nWRITE  C template + stores        %8.1f ns/record\n", write_template );
    std::printf( "WRITE  D form 1 (today)            %8.1f ns/record\n", write_form1 );
    std::printf( "       ratio D/C                   %8.3fx\n", write_form1 / write_template );
    std::printf( "\n(sink %llu)\n", (unsigned long long) sink );
    return 0;
}
