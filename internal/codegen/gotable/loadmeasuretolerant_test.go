package gotable

import "testing"

// docs/SPEC-TABLES.md section 4: LoadMeasureReason refuses damaged framing;
// LoadMeasure still sizes the tolerant Load, which keeps the prefix.
func TestGoLoadMeasureSizesTheTolerantLoadOfDamagedFraming(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
 table Root { alpha int64 }
 `, `package probe
 import("errors";"testing";"unsafe")
 func TestTolerant(t *testing.T){
  var b RootBuilder;if !b.Init(){t.Fatal("init")};defer b.Shutdown()
  root:=b.GetRoot();wire:=make([]byte,RootMeasure(root,&b.Arena));RootSave(root,wire,&b.Arena)
  cut:=wire[:len(wire)-1]
  if n,err:=RootLoadMeasureReason(cut);n!= -1||!errors.Is(err,TableRefuseWireDamaged){t.Fatalf("reason on the cut wire: %d %v",n,err)}
  n:=RootLoadMeasure(cut);if n<0{t.Fatalf("tolerant load measure %d on the cut wire",n)}
  raw:=make([]byte,n+63);off:=(-uintptr(unsafe.Pointer(&raw[0])))&63;region:=raw[off:off+uintptr(n)]
  var r TableReport;if v:=RootLoadTolerant(region,cut,&r);v!=nil||!r.Malformed{t.Fatalf("tolerant load of the cut wire must discard the whole load: nil=%t report %+v",v==nil,r)}
  var pr TableReport;if v:=RootLoad(region,cut,&pr);v!=nil||!errors.Is(pr.Err(),TableRefuseMalformed){t.Fatalf("strict plain load of the cut wire: nil=%t err %v",v==nil,pr.Err())}
 }
 `)
}

// A schema table may take a name the generated runtime does not reserve: the one
// measure body adds no package-scope name (regionMeasureFraming was one, unregistered,
// so a table by that name passed the checker and the generated package did not build).
func TestGoTableNamedRegionMeasureFramingStillBuilds(t *testing.T) {
	t.Parallel()
	runGenerated(t, `package probe
 table regionMeasureFraming { alpha int64 }
 `, `package probe
 import "testing"
 func TestBuilds(t *testing.T){}
 `)
}
