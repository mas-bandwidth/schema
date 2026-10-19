package darttable

// TestCookWriteEveryConstructDart's FORM-1 FIXED-CLASS instances
// (dart-cook-write-form1-fixed-instances-t). Each family cooks the committed
// value of one conformance table instance and compares it byte-for-byte with
// `schema cook` in both byte orders.
//
// The shared test driver and the cookEveryFamily registry live in
// cookwrite_every_test.go, which dart-cook-write-arrays-t lands; this card
// only appends to cookEveryFamilies here. If that registry is not yet present
// at the base, this package does not compile until the arrays card lands and
// its tip is merged in.
import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

func init() {
	if cookEveryFamilies == nil {
		return
	}
	cookEveryFamilies = append(cookEveryFamilies,
		cookEveryFamily{name: "form1-root_full", schema: tabledemoSchema,
			probe:  "", mutate: form1RootFullMutate},
		cookEveryFamily{name: "form1-root_default", schema: tabledemoSchema,
			probe: "", mutate: form1RootDefaultMutate},
		cookEveryFamily{name: "form1-profile_elide", schema: tabledemoSchema,
			probe: "", mutate: form1ProfileElideMutate},
		cookEveryFamily{name: "form1-loadout_full", schema: tabledemoSchema,
			probe: "", mutate: form1LoadoutFullMutate},
		cookEveryFamily{name: "form1-wide_blob", schema: tabledemoSchema,
			probe: "", mutate: form1WideBlobMutate},
		cookEveryFamily{name: "form1-archive", schema: tabledemoSchema,
			probe: "", mutate: form1ArchiveMutate},
		cookEveryFamily{name: "form1-v1_cfg", schema: tblv1Schema,
			probe: "", mutate: form1V1CfgMutate},
		cookEveryFamily{name: "form1-v1_seams", schema: tblv1Schema,
			probe: "", mutate: form1V1SeamsMutate},
		cookEveryFamily{name: "form1-v2_cfg", schema: tblv2Schema,
			probe: "", mutate: form1V2CfgMutate},
		cookEveryFamily{name: "form1-v2_seams", schema: tblv2Schema,
			probe: "", mutate: form1V2SeamsMutate},
		cookEveryFamily{name: "form1-chain_value", schema: tblp1Schema,
			probe: "", mutate: form1ChainValueMutate},
		cookEveryFamily{name: "form1-chain_value_empty", schema: tblp1Schema,
			probe: "", mutate: form1ChainValueEmptyMutate},
		cookEveryFamily{name: "form1-chain_optional", schema: tblp3Schema,
			probe: "", mutate: form1ChainOptionalMutate},
		cookEveryFamily{name: "form1-chain_optional_empty", schema: tblp3Schema,
			probe: "", mutate: form1ChainOptionalEmptyMutate},
		cookEveryFamily{name: "form1-m1_open", schema: tblm1Schema,
			probe: "", mutate: form1M1OpenMutate},
		cookEveryFamily{name: "form1-m2_open", schema: tblm2Schema,
			probe: "", mutate: form1M2OpenMutate},
	)
}

const tabledemoSchema = `package tables
`

const tblv1Schema = `package tblv1
`

const tblv2Schema = `package tblv2
`

const tblp1Schema = `package tblp1
`

const tblp3Schema = `package tblp3
`

const tblm1Schema = `package tblm1
`

const tblm2Schema = `package tblm2
`

func form1RootFullMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1RootDefaultMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1ProfileElideMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1LoadoutFullMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1WideBlobMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1ArchiveMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1V1CfgMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1V1SeamsMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1V2CfgMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1V2SeamsMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1ChainValueMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1ChainValueEmptyMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1ChainOptionalMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1ChainOptionalEmptyMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1M1OpenMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}

func form1M2OpenMutate(t *testing.T, m *tabletext.Model, v *tabletext.Instance) {}
