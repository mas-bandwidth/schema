/* the tblann4096 unit (test/tables/announce4096), one of the pair at the edge of
   the 4096-entry announcement cap (docs/SPEC-TABLES.md §3.3). Only its
   announcement is asked (README, `announce`). */
#include "announce4096/AnnounceTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE announce4096
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER ANNOUNCE4096
#include "unit.h"

SCHEMA_CONFORMANCE_ANNOUNCE_1122
static const ConformanceUnit1122 unit1122 = { "tblann4096", schema_conformance_announce_measure_, schema_conformance_announce_write_,
                                              schema_conformance_announce_read_, NULL, NULL };
const ConformanceUnit1122 * conformance_unit1122_tblann4096( void ) { return &unit1122; }
