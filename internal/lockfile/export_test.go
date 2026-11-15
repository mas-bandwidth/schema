package lockfile

// AppendLineage is the test seam onto the one place a lineage entry joins a
// table's history, so a test can hand it two entries with one wire hash without
// searching for a real collision.
var AppendLineage = appendLineage
