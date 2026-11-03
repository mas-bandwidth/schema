/* the listdemo unit's strict reads (tables/lists). Its codec rows live in
   test/c-tables beside the collections differential; the strict rows live
   here, one translation unit per package as unit.h requires. */
#include "lists/HoldersTable.h"
#include "lists/MigrateTable.h"
#include "lists/ReportTable.h"
#include "lists/SaveTable.h"
#include "lists/SharedTable.h"
#define SCHEMA_CONFORMANCE_PACKAGE listdemo
#define SCHEMA_CONFORMANCE_PACKAGE_UPPER LISTDEMO
#include "unit.h"

SCHEMA_CONFORMANCE_GRAPH_STRICT( Save, save )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Mixed, mixed )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Album, album )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Sheet, sheet )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Army, army )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Unbounded, unbounded )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Ints, ints )
SCHEMA_CONFORMANCE_GRAPH_STRICT( Squad, squad )
SCHEMA_CONFORMANCE_STRICT( Bounded, bounded )

static const ConformanceStrict strict_rows[] = {
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Save ),
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Mixed ),
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Album ),
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Sheet ),
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Army ),
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Unbounded ),
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Ints ),
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Squad ),
    SCHEMA_CONFORMANCE_STRICT_ROW( listdemo, Bounded )
};

const ConformanceStrict * conformance_stricts_listdemo( int * count )
{
    *count = (int) ( sizeof( strict_rows ) / sizeof( strict_rows[0] ) );
    return strict_rows;
}
