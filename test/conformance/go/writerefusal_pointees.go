//go:build !writebounds_only

package main

import (
	"blobdemo"
	"errors"
	"graphdemo"
	"reflect"
	tblsurf "surfaces"
)

func isBlobSharedErr(err error) bool {
	return errors.Is(err, blobdemo.TableRefuseBlobShared)
}

// writeRefusalGoPointees names the record type a region pointer slot points
// at: reflection sees the slot as an int64 (a signed self-relative delta,
// §6.3), and the generated comment that names its type is not reflected. A
// `point` row walking through a slot this table does not name fails loudly.
var writeRefusalGoPointees = map[reflect.Type]map[string]reflect.Type{
	reflect.TypeFor[graphdemo.SceneRow]():    {"Head": reflect.TypeFor[graphdemo.ListNodeRow](), "Alias": reflect.TypeFor[graphdemo.ListNodeRow]()},
	reflect.TypeFor[graphdemo.ListNodeRow](): {"Next": reflect.TypeFor[graphdemo.ListNodeRow]()},
	reflect.TypeFor[tblsurf.ChainRow]():      {"Head": reflect.TypeFor[tblsurf.LinkRow]()},
	reflect.TypeFor[tblsurf.LinkRow]():       {"Next": reflect.TypeFor[tblsurf.LinkRow]()},
	reflect.TypeFor[tblsurf.HolderRow]():     {"Must": reflect.TypeFor[tblsurf.LeafRow]()},
}
