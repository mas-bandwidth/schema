package gotable

import "testing"

func TestUnitViewPacketStorage(t *testing.T) {
	runGenerated(t, `package probe
 enum Hidden { A, B }
 type Child { n int32 = 7 }
 union Choice { a Child
 b Child }
 type Shared { choice Choice
 after uint32 = 19 }
 type Packet { nested Shared
 hidden Hidden
 text string(8) = "ready" }
 table Root { shared Shared
 next *Root }
 `, `package probe
 import("testing";"unsafe")
 func TestPacketStorage(t *testing.T){
 var packet Packet;desc:=PacketTableType();desc.Reset(unsafe.Pointer(&packet));if packet.Nested.After!=19||string(packet.Text[:packet.TextLength])!="ready"{t.Fatalf("packet defaults: %+v",packet)}
 if desc.Fields[0].Offset!=uint32(unsafe.Offsetof(Packet{}.Nested)){t.Fatal("outside packet field offset")};if desc.Fields[0].Id!=0||desc.Fields[0].Json!=""||desc.SaveBody!=nil||desc.LoadBody!=nil||desc.SaveMessageBody!=nil{t.Fatal("outside packet gained wire identity or codec")}
 nested:=desc.Fields[0].Table();if nested==SharedTableType(){t.Fatal("packet field points at table Row descriptor")};if nested.Size!=uint32(unsafe.Sizeof(Shared{}))||nested.Fields[1].Offset!=uint32(unsafe.Offsetof(Shared{}.After)){t.Fatal("private packet offsets")}
 union:=nested.Fields[0].Arms();if union.TagSize!=uint32(unsafe.Sizeof(Choice{}.Type))||union.Arms[2].Offset!=uint32(unsafe.Offsetof(Choice{}.B)){t.Fatal("packet union layout")};packet.Nested.Choice.B.N=99;union.Arms[2].Reset(unsafe.Pointer(&packet.Nested.Choice));if packet.Nested.Choice.B.N!=7{t.Fatal("packet arm default")}
 if desc.Fields[1].VariantId(1)!=0||UnitView().Enums[0].Variants[1].Id!=0{t.Fatal("unreached vocabulary gained identity")}
 if n:=testing.AllocsPerRun(20,func(){UnitView();desc.Reset(unsafe.Pointer(&packet));desc.Fields[0].Table().Fields[0].Arms()});n!=0{t.Fatalf("packet view allocates %v",n)}
 }
 `)
}
