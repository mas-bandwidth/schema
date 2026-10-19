package ci

// The SHARED HOSTILE-INPUT CORPUS (schema-shared-hostile-corpus).
//
// One corpus of adversarial inputs for schema, shared by every port. Each
// input's expected verdict — accept with the digest of the decoded value, or
// refuse with the engine's named reason — is taken from the GO ENGINE
// (internal/tablewire, internal/tabletext), never from a generated reader.
//
// THE CORPUS IS COMMITTED IN THIS FILE. The lander admits only the card's
// PATHS, and this test file is the one new non-make, non-workflow path it
// names, so the raw inputs and the line-oriented manifest live here rather
// than in a sibling directory. The format is unchanged and still needs no JSON
// dependency: the manifest is one tab-separated line per case and the payloads
// are raw bytes (base64 only to survive a Go source file), so a port reads
// them with a line reader and its own decoder. See docs/HOSTILE-CORPUS.md.
//
// The manifest fields are:
//
//	name  carrier  schema  table  input  provenance  verdict  reason  digest
//
// TestSchemaSharedHostileCorpus is the drift gate: it rebuilds every verdict
// from the engine and fails on any difference from the committed manifest.
// TestSchemaSharedHostileCorpusNegativeControl plants a changed verdict and
// requires that same gate to go red, so the gate is proved to have a blade.

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

const (
	hostileAccept   = "accept"
	hostileRefuse   = "refuse"
	hostileNoReason = "-"

	hostileReasonDamaged            = "damaged"
	hostileReasonNewerForm          = "newer_form"
	hostileReasonNoVocabulary       = "no_vocabulary"
	hostileReasonSecondAnnouncement = "second_announcement"
	hostileReasonVocabularyTooLarge = "vocabulary_too_large"
	hostileReasonMessageFormAsFile  = "message_form_as_file"
	hostileReasonBatchTooLarge      = "batch_too_large"
	hostileReasonCountRefusal       = "count_refusal"
	hostileReasonEngineError        = "engine_error"

	// hostileProbeSchemaPath is the manifest's name for the one schema that is
	// not in the checkout: a variable root whose collection count can pass the
	// int32 storage cap and name internal/tablewire's CountRefusal.
	hostileProbeSchemaPath = "schemas/probe.schema"
)

// HOSTILE-MANIFEST-BEGIN
const hostileManifest = `# schema shared hostile-input corpus — line-oriented manifest
# name	carrier	schema	table	input	provenance	verdict	reason	digest
announce-bytes-too-large	announce_bytes	tables/examples	PackConfig	inputs/announcement.bin	bound: the vocabulary field one above a receiver's byte bound (MaxBytes=8)	refuse	vocabulary_too_large	-
announce-entries-too-large	announce_entries	tables/examples	PackConfig	inputs/announcement.bin	bound: the vocabulary entry count above a receiver's capacity (MaxEntries=1)	refuse	vocabulary_too_large	-
announce-newer-form	announce	tables/examples	PackConfig	inputs/announcement-newer-form.bin	bound: a form byte above the one this reader carries, on the announcement path	refuse	newer_form	-
announce-second	announce_second	tables/examples	PackConfig	inputs/announcement.bin	edge: a second announcement on one connection	refuse	second_announcement	-
conformance-arm-pointer-null	text	tables/stream	Feed	inputs/conformance-arm-pointer-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/arm-pointer-null	accept	-	5b245b9db93e60dd2f95abcd25c582e677281c775f5b16e4acb9ce10bcb85fd9
conformance-arm-scalar-null	text	tables/messages	ToolMessage	inputs/conformance-arm-scalar-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/arm-scalar-null	accept	-	a49eaa69a47eae140375c24669c0dca3b71f971a387880b8fac0a8b6ebd0031c
conformance-arm-scalar-string	text	tables/messages	ToolMessage	inputs/conformance-arm-scalar-string.json	conformance hostile vector testdata/conformance/tables/json-hostile/arm-scalar-string	accept	-	6e5b5e663b7b2e8905e2662c588632c89521b9e65790e37ff307881efeb3996b
conformance-arm-void-not-null	text	tables/messages	ToolMessage	inputs/conformance-arm-void-not-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/arm-void-not-null	accept	-	c687e44c9be734ae353a4965a06da266033ae4efad6f7f1063139f2805582730
conformance-array-past-bound	text	tables/examples	RootConfig	inputs/conformance-array-past-bound.json	conformance hostile vector testdata/conformance/tables/json-hostile/array-past-bound	accept	-	5076f7d4995f94577af1a2831c25130c9056b98bd5f9d65034f48fed942a9c6e
conformance-base64-empty	text	tables/examples	RootConfig	inputs/conformance-base64-empty.json	conformance hostile vector testdata/conformance/tables/json-hostile/base64-empty	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-base64-unpadded	text	tables/examples	RootConfig	inputs/conformance-base64-unpadded.json	conformance hostile vector testdata/conformance/tables/json-hostile/base64-unpadded	accept	-	f5c68b1fef83aabe41354cd53c760bc680cad56ea4b43c730609c0a6a13a8298
conformance-bits-at-max	text	tables/examples	RootConfig	inputs/conformance-bits-at-max.json	conformance hostile vector testdata/conformance/tables/json-hostile/bits-at-max	accept	-	1b1a314b1e8d61e7e8d623dee62d36a1063c6ae8e82f75ad09f16faabcae13cc
conformance-bits-over-range	text	tables/examples	RootConfig	inputs/conformance-bits-over-range.json	conformance hostile vector testdata/conformance/tables/json-hostile/bits-over-range	accept	-	1b1a314b1e8d61e7e8d623dee62d36a1063c6ae8e82f75ad09f16faabcae13cc
conformance-blob-empty-string	text	tables/blobs	Catalog	inputs/conformance-blob-empty-string.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-empty-string	accept	-	3ede27215e620976db484c89cbc8ffd9b62036f1c5ca63fed0f5f1b483805da8
conformance-blob-label-in-skipped-value	text	tables/blobs	Catalog	inputs/conformance-blob-label-in-skipped-value.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-label-in-skipped-value	accept	-	7a62172c4b7d0078f5bcc6f7d6209f1a91636adba0a5411cef9f2cfe259f25e9
conformance-blob-not-base64	text	tables/blobs	Catalog	inputs/conformance-blob-not-base64.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-not-base64	accept	-	5641370757752e3c130f1b08cea377910a3c45874e62335948b05a37741d03fe
conformance-blob-null	text	tables/blobs	Catalog	inputs/conformance-blob-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-null	accept	-	cf52a824ad3d8834b955322d11aa83b35ff2f320ba7948ab539a8fc8afe74bfc
conformance-blob-wrong-shape	text	tables/blobs	Catalog	inputs/conformance-blob-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/blob-wrong-shape	accept	-	a4b80b1e396ac2297864ee638cb8b34fdb1e672713cd87a5e98eb2f9fd54e1b9
conformance-bytes-not-base64	text	tables/examples	RootConfig	inputs/conformance-bytes-not-base64.json	conformance hostile vector testdata/conformance/tables/json-hostile/bytes-not-base64	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-bytes-overlong	text	tables/examples	RootConfig	inputs/conformance-bytes-overlong.json	conformance hostile vector testdata/conformance/tables/json-hostile/bytes-overlong	accept	-	2ea61cae985a16c81d022a6f38eb9d63f2ef660a2bd66f07b36daa75dd321348
conformance-comment-block-between	text	tables/examples	RootConfig	inputs/conformance-comment-block-between.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-block-between	accept	-	9883833f1c1bb68cd1fa0947a8b51e7d8020fd81ba2bd5069bc14fbe8e476c4c
conformance-comment-block-first	text	tables/examples	RootConfig	inputs/conformance-comment-block-first.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-block-first	accept	-	9883833f1c1bb68cd1fa0947a8b51e7d8020fd81ba2bd5069bc14fbe8e476c4c
conformance-comment-inside-string	text	tables/examples	RootConfig	inputs/conformance-comment-inside-string.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-inside-string	accept	-	6bab345e235be1cfa43a206744824e9f15089818899b5fc66bfab8e671421e17
conformance-comment-last-line	text	tables/examples	RootConfig	inputs/conformance-comment-last-line.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-last-line	accept	-	9883833f1c1bb68cd1fa0947a8b51e7d8020fd81ba2bd5069bc14fbe8e476c4c
conformance-comment-line-first	text	tables/examples	RootConfig	inputs/conformance-comment-line-first.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-line-first	accept	-	9883833f1c1bb68cd1fa0947a8b51e7d8020fd81ba2bd5069bc14fbe8e476c4c
conformance-comment-slash-star-slash	text	tables/examples	RootConfig	inputs/conformance-comment-slash-star-slash.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-slash-star-slash	refuse	damaged	-
conformance-comment-unclosed	text	tables/examples	RootConfig	inputs/conformance-comment-unclosed.json	conformance hostile vector testdata/conformance/tables/json-hostile/comment-unclosed	refuse	damaged	-
conformance-dup-key	text	tables/examples	RootConfig	inputs/conformance-dup-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key	accept	-	039f49f40c7d3f8315a1b6a5e5c710a211be356b50411a494cca2940d55f75c2
conformance-dup-key-array	text	tables/examples	RootConfig	inputs/conformance-dup-key-array.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-array	accept	-	600d6be039ed2f2b8dd8e541a6dc0a628ae966fb38a2cc94be232093f45c2794
conformance-dup-key-placeable	text	tables/examples	RootConfig	inputs/conformance-dup-key-placeable.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-placeable	accept	-	0f357623522df02608a9fe9168674f1beafaf04ff5d0fc8da5e4490aec86c0f4
conformance-dup-key-rejected-element	text	tables/examples	RootConfig	inputs/conformance-dup-key-rejected-element.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-element	accept	-	14da9db93fde92383d8a521d369f4744d8ba71208f2fe7dba29d2a0a2a50f497
conformance-dup-key-rejected-float	text	tables/examples	RootConfig	inputs/conformance-dup-key-rejected-float.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-float	accept	-	db8f17169b5721a71d7e018a56a400df4b45defc74dc5925ff8c4db38555c25f
conformance-dup-key-rejected-keyed	text	tables/examples	PackConfig	inputs/conformance-dup-key-rejected-keyed.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-keyed	accept	-	ab804aaa9cb043a17e23359d2d10ffa5241672f2cf0be45698cf5b56196d6445
conformance-dup-key-rejected-nested	text	tables/examples	RootConfig	inputs/conformance-dup-key-rejected-nested.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-nested	accept	-	a6dea03ae39f3f87b278fd4dd32ad9dd866fc14877dfc8c0dec110c5ac315c46
conformance-dup-key-rejected-scalar	text	tables/examples	RootConfig	inputs/conformance-dup-key-rejected-scalar.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-rejected-scalar	accept	-	c3dc90169dea9dad57a5854aa138c83aa2dd9fbbcecdc2661c19d9792994eddf
conformance-dup-key-table	text	tables/examples	RootConfig	inputs/conformance-dup-key-table.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-table	accept	-	efe469058330f1bb3b6620b1dbe766545636a3ce6749d4bcc2da3be027d57466
conformance-dup-key-union	text	tables/examples	RootConfig	inputs/conformance-dup-key-union.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-union	accept	-	331ce77862ec6012d08c9a147ac40266903fd02942809495305986508aa4e405
conformance-dup-key-wrong-shape	text	tables/examples	RootConfig	inputs/conformance-dup-key-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/dup-key-wrong-shape	accept	-	c3dc90169dea9dad57a5854aa138c83aa2dd9fbbcecdc2661c19d9792994eddf
conformance-enum-unknown-name	text	tables/examples	RootConfig	inputs/conformance-enum-unknown-name.json	conformance hostile vector testdata/conformance/tables/json-hostile/enum-unknown-name	accept	-	c3fe203b8318c5eecddff82a7b5f4a278878f3bb54ce608272e1e2ebb64f042d
conformance-fixed-array-elements	text	tables/scalars	SimState	inputs/conformance-fixed-array-elements.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-array-elements	accept	-	3bdd3ca8116e22fc6f6870a6525540d2b5f3c807b3bd40fe207e633fc69a7273
conformance-fixed-at-bound	text	tables/scalars	SimState	inputs/conformance-fixed-at-bound.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-at-bound	accept	-	75aa5d7bfd7f8381bded8c7c2cdb0175321635aabff4abf04f840910fa6991e7
conformance-fixed-below-resolution	text	tables/scalars	SimState	inputs/conformance-fixed-below-resolution.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-below-resolution	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-fixed-exact-fraction	text	tables/scalars	SimState	inputs/conformance-fixed-exact-fraction.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-exact-fraction	accept	-	8e13e228d50fe4f9e37d92e019984e2e99e82efe723de26137dc19c06951fbf0
conformance-fixed-fraction-too-fine	text	tables/scalars	SimState	inputs/conformance-fixed-fraction-too-fine.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-fraction-too-fine	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-fixed-huge-exponent	text	tables/scalars	SimState	inputs/conformance-fixed-huge-exponent.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-huge-exponent	accept	-	7049074b6bc35e23eebe80fb1bd6076b4026e0276c5b81e93794869f2d9e1ee5
conformance-fixed-integral-token	text	tables/scalars	SimState	inputs/conformance-fixed-integral-token.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-integral-token	accept	-	ff0c6b8a7553e3becfd0afe63d2c57f9f7add83bd858f28df6d197b09e97f009
conformance-fixed-negative-zero	text	tables/scalars	SimState	inputs/conformance-fixed-negative-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-negative-zero	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-fixed-nested-type	text	tables/scalars	SimState	inputs/conformance-fixed-nested-type.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-nested-type	accept	-	357b3cfe004daef4d52da5a3639d46521aa9d02ff1c215a2fb1226450da6b40a
conformance-fixed-over-range	text	tables/scalars	SimState	inputs/conformance-fixed-over-range.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-over-range	accept	-	aaab3f8b161052ba6bde88528ed9bc656dc96db058c4769d63c720646dfa790b
conformance-fixed-wrong-shape	text	tables/scalars	SimState	inputs/conformance-fixed-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/fixed-wrong-shape	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-flags-unknown-name	text	tables/examples	RootConfig	inputs/conformance-flags-unknown-name.json	conformance hostile vector testdata/conformance/tables/json-hostile/flags-unknown-name	accept	-	ad2386fd1b8739e85550cc300c5461b87a33b61206dd72832260e2e47132a023
conformance-guard-key-after-fields	text	tables/examples	RootConfig	inputs/conformance-guard-key-after-fields.json	conformance hostile vector testdata/conformance/tables/json-hostile/guard-key-after-fields	accept	-	5e90ed45c2121c6f83d3097e86c127d86594ca26087a3422d25cb5b8094c011c
conformance-int128-beyond-int64	text	tables/scalars	SimState	inputs/conformance-int128-beyond-int64.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-beyond-int64	accept	-	06454ea4171e56286c1e3e6191fa4b58bed884e42aeb5618d4692dcbaa361a7c
conformance-int128-fractional	text	tables/scalars	SimState	inputs/conformance-int128-fractional.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-fractional	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-int128-integral-forms	text	tables/scalars	SimState	inputs/conformance-int128-integral-forms.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-integral-forms	accept	-	b0df95ceed833887c01576f2236432fdde7470fd09c851a7ed001a0ab257e9b2
conformance-int128-over-range	text	tables/scalars	SimState	inputs/conformance-int128-over-range.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-over-range	accept	-	5e3658d85122e72560929a4258c1fb20a87821af4ad5997abd72a6ea4dd970e4
conformance-int128-saturated-below	text	tables/scalars	SimState	inputs/conformance-int128-saturated-below.json	conformance hostile vector testdata/conformance/tables/json-hostile/int128-saturated-below	accept	-	5900a53ef10f059e41e84694f134577980d95341a4b8d7f8cf96c47c346c5c70
conformance-json-key-declared-name	text	tables/examples	PackConfig	inputs/conformance-json-key-declared-name.json	conformance hostile vector testdata/conformance/tables/json-hostile/json-key-declared-name	accept	-	4abc240da90c15e9807b5bcdd70deaa2c750ec3d63f66f19d9a2ce4db65d69fd
conformance-keyed-duplicate-mixed	text	tables/examples	PackConfig	inputs/conformance-keyed-duplicate-mixed.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-duplicate-mixed	accept	-	0507e6067ce4493ec31d644c40f28a6d0f45bb295d0a46d877fc1e547733536f
conformance-keyed-duplicate-none	text	tables/examples	PackConfig	inputs/conformance-keyed-duplicate-none.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-duplicate-none	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-keyed-duplicate-slot	text	tables/examples	PackConfig	inputs/conformance-keyed-duplicate-slot.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-duplicate-slot	accept	-	1f683f615669fe3ed7f16f1a159fa3d38e372fcfb3e12854ba1ef60568a22587
conformance-keyed-duplicate-unknown	text	tables/examples	PackConfig	inputs/conformance-keyed-duplicate-unknown.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-duplicate-unknown	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-keyed-none-key	text	tables/examples	PackConfig	inputs/conformance-keyed-none-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-none-key	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-keyed-unknown-key	text	tables/examples	PackConfig	inputs/conformance-keyed-unknown-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-unknown-key	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-keyed-wrong-shape	text	tables/examples	PackConfig	inputs/conformance-keyed-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/keyed-wrong-shape	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-label-before-definition	text	tables/pointers	Scene	inputs/conformance-label-before-definition.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-before-definition	refuse	damaged	-
conformance-label-defined-twice	text	tables/pointers	Scene	inputs/conformance-label-defined-twice.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-defined-twice	refuse	damaged	-
conformance-label-dropped-definition	text	tables/pointers	Scene	inputs/conformance-label-dropped-definition.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-dropped-definition	accept	-	a5e822faf7cd51dc8e70dc83b6d051cf5e5ddd9e8b400816b461b06a5ddcd002
conformance-label-in-by-value-nesting	text	tables/pointers	Scene	inputs/conformance-label-in-by-value-nesting.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-in-by-value-nesting	refuse	damaged	-
conformance-label-in-unknown-key	text	tables/pointers	Scene	inputs/conformance-label-in-unknown-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-in-unknown-key	accept	-	a7c244812df3edb7736d5b6fa0acfee3285cf854c5ddcc454cf572f1ccee7d37
conformance-label-leading-zero	text	tables/pointers	Scene	inputs/conformance-label-leading-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-leading-zero	refuse	damaged	-
conformance-label-negative	text	tables/pointers	Scene	inputs/conformance-label-negative.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-negative	refuse	damaged	-
conformance-label-not-an-integer	text	tables/pointers	Scene	inputs/conformance-label-not-an-integer.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-not-an-integer	refuse	damaged	-
conformance-label-not-first-key	text	tables/pointers	Scene	inputs/conformance-label-not-first-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-not-first-key	refuse	damaged	-
conformance-label-on-the-root	text	tables/pointers	Scene	inputs/conformance-label-on-the-root.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-on-the-root	refuse	damaged	-
conformance-label-overflow	text	tables/pointers	Scene	inputs/conformance-label-overflow.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-overflow	refuse	damaged	-
conformance-label-self-reference	text	tables/pointers	Scene	inputs/conformance-label-self-reference.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-self-reference	refuse	damaged	-
conformance-label-type-mismatch	text	tables/pointers	Scene	inputs/conformance-label-type-mismatch.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-type-mismatch	accept	-	29426bf9b050a0c592df7ca6188a51cd5c336c5c65568c378729d16fc5a91eee
conformance-label-undefined	text	tables/pointers	Scene	inputs/conformance-label-undefined.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-undefined	refuse	damaged	-
conformance-label-zero	text	tables/pointers	Scene	inputs/conformance-label-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/label-zero	refuse	damaged	-
conformance-nesting-past-depth-cap	text	tables/examples	RootConfig	inputs/conformance-nesting-past-depth-cap.json	conformance hostile vector testdata/conformance/tables/json-hostile/nesting-past-depth-cap	refuse	damaged	-
conformance-null-optional	text	tables/examples	PackConfig	inputs/conformance-null-optional.json	conformance hostile vector testdata/conformance/tables/json-hostile/null-optional	accept	-	8221f3f1ffa440d064ef574d3b936d89423293b0e837275506b3021420a17562
conformance-null-scalar	text	tables/examples	PackConfig	inputs/conformance-null-scalar.json	conformance hostile vector testdata/conformance/tables/json-hostile/null-scalar	accept	-	aa8eda454003c5e5b206b7184f9c70922466a2660b19fab4191c99215f131621
conformance-num-bare-minus	text	tables/examples	RootConfig	inputs/conformance-num-bare-minus.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-bare-minus	refuse	damaged	-
conformance-num-double-dot	text	tables/examples	RootConfig	inputs/conformance-num-double-dot.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-double-dot	refuse	damaged	-
conformance-num-double-minus	text	tables/examples	RootConfig	inputs/conformance-num-double-minus.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-double-minus	refuse	damaged	-
conformance-num-float32-midpoint	text	tables/examples	RootConfig	inputs/conformance-num-float32-midpoint.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float32-midpoint	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-float32-overflow	text	tables/examples	RootConfig	inputs/conformance-num-float32-overflow.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float32-overflow	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-float32-subnormal	text	tables/examples	RootConfig	inputs/conformance-num-float32-subnormal.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float32-subnormal	accept	-	cc4de3259bc0ad6729a30acd14264c3f20e640d765cf72ebc0701f8144286bef
conformance-num-float32-upper-band	text	tables/examples	RootConfig	inputs/conformance-num-float32-upper-band.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float32-upper-band	accept	-	ac8069499b9b765a014048fd36a5ca8be46b973661d7a56ff8f95d474ac01709
conformance-num-float64-overflow	text	tables/examples	RootConfig	inputs/conformance-num-float64-overflow.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-float64-overflow	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-fraction	text	tables/examples	RootConfig	inputs/conformance-num-fraction.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-fraction	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-int-overflow	text	tables/examples	RootConfig	inputs/conformance-num-int-overflow.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-int-overflow	accept	-	b38ab599ce4d34e61c63ea12cb1441121175d77ce93f18a8a1953e5c3ec61651
conformance-num-int64-min	text	tables/examples	RootConfig	inputs/conformance-num-int64-min.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-int64-min	accept	-	c8592dc53adcfe75b4753d200d82ebdcb84fb3718e4a43ce15ed56301facecd7
conformance-num-integral-forms	text	tables/examples	RootConfig	inputs/conformance-num-integral-forms.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-integral-forms	accept	-	d823692a928482f793564231d877b659434570354e8034a411d4c4282e47e5fc
conformance-num-leading-plus	text	tables/examples	RootConfig	inputs/conformance-num-leading-plus.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-leading-plus	refuse	damaged	-
conformance-num-leading-zero	text	tables/examples	RootConfig	inputs/conformance-num-leading-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-leading-zero	refuse	damaged	-
conformance-num-negative-unsigned	text	tables/examples	RootConfig	inputs/conformance-num-negative-unsigned.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-negative-unsigned	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-negative-zero	text	tables/examples	RootConfig	inputs/conformance-num-negative-zero.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-negative-zero	accept	-	363355c9aae20f740477abef8d614faaf34167cd01b02f075990c7edad8f644f
conformance-num-no-exp-digits	text	tables/examples	RootConfig	inputs/conformance-num-no-exp-digits.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-no-exp-digits	refuse	damaged	-
conformance-num-no-int	text	tables/examples	RootConfig	inputs/conformance-num-no-int.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-no-int	refuse	damaged	-
conformance-num-three-part	text	tables/examples	RootConfig	inputs/conformance-num-three-part.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-three-part	refuse	damaged	-
conformance-num-trailing-dot	text	tables/examples	RootConfig	inputs/conformance-num-trailing-dot.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-trailing-dot	refuse	damaged	-
conformance-num-trailing-plus	text	tables/examples	RootConfig	inputs/conformance-num-trailing-plus.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-trailing-plus	refuse	damaged	-
conformance-num-two-exponents	text	tables/examples	RootConfig	inputs/conformance-num-two-exponents.json	conformance hostile vector testdata/conformance/tables/json-hostile/num-two-exponents	refuse	damaged	-
conformance-optional-array-element-shape	text	tables/messages	ToolMessage	inputs/conformance-optional-array-element-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-array-element-shape	accept	-	9554dd4729de859c86b913ee9a010a5cc640f0129d19d690bf5b2fd302e69789
conformance-optional-array-null	text	tables/messages	ToolMessage	inputs/conformance-optional-array-null.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-array-null	accept	-	c88bfa21718fcd215630914fe8b22bcf354a2ebd6059efe685052755e24e612f
conformance-optional-array-shape	text	tables/messages	ToolMessage	inputs/conformance-optional-array-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-array-shape	accept	-	f2032ed93aa3969e30c0f86f6b0141068f2b1487e539fd81f5bedf65c5fc2c86
conformance-optional-present-empty	text	tables/examples	PackConfig	inputs/conformance-optional-present-empty.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-present-empty	accept	-	8a4546b8021ffdb54bcd57fe1250bde141e864e9d74a630916feb9ea027cebb6
conformance-optional-wrong-shape	text	tables/examples	PackConfig	inputs/conformance-optional-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/optional-wrong-shape	accept	-	b679a018dcb48b55559210d214d0cd2bac7770216fc82bfe67d23ebd901570a4
conformance-pointer-array-element-shape	text	tables/stream	Feed	inputs/conformance-pointer-array-element-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/pointer-array-element-shape	accept	-	aacdd729070f33ce0ef7fff5d10a742fda2be8746c9e40cfc6845e4e5e0f1207
conformance-pointer-array-shared-label	text	tables/stream	Feed	inputs/conformance-pointer-array-shared-label.json	conformance hostile vector testdata/conformance/tables/json-hostile/pointer-array-shared-label	accept	-	d44255051bbc610bebe28e4ebd078c70c556d878054517ca0ca5eae8d6f20465
conformance-reserved-ampersand-in-skipped-value	text	tables/examples	RootConfig	inputs/conformance-reserved-ampersand-in-skipped-value.json	conformance hostile vector testdata/conformance/tables/json-hostile/reserved-ampersand-in-skipped-value	accept	-	be1ec856da2aae75b3516fe73c5eb922f2042132cd1c5570f8936b07d6d211f9
conformance-reserved-ampersand-key	text	tables/examples	RootConfig	inputs/conformance-reserved-ampersand-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/reserved-ampersand-key	refuse	damaged	-
conformance-shared-node-by-label	text	tables/pointers	Scene	inputs/conformance-shared-node-by-label.json	conformance hostile vector testdata/conformance/tables/json-hostile/shared-node-by-label	accept	-	ac4e86c548c97e92d2b3ec2287b81da6062849fc5d304e69bf68b9a0e8b82015
conformance-str-clamp-boundary	text	tables/examples	RootConfig	inputs/conformance-str-clamp-boundary.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-clamp-boundary	accept	-	5b49596ca7851df717fa0abf5bf57cb240d18c90d8c968bcfcd5aa34aa3b4c61
conformance-str-illformed-at-bound	text	tables/examples	RootConfig	inputs/conformance-str-illformed-at-bound.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-illformed-at-bound	accept	-	a78a7791d6ab20a76f4543412e9119b70befa760acf33f967690c10529f20f03
conformance-str-lone-lead-surrogate	text	tables/examples	RootConfig	inputs/conformance-str-lone-lead-surrogate.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-lone-lead-surrogate	accept	-	a97cb775edd55072097790f1f3b5a3889bb773c40aaaed4a55eb83625a85c4ee
conformance-str-lone-trail-surrogate	text	tables/examples	RootConfig	inputs/conformance-str-lone-trail-surrogate.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-lone-trail-surrogate	accept	-	a97cb775edd55072097790f1f3b5a3889bb773c40aaaed4a55eb83625a85c4ee
conformance-str-surrogate-pair	text	tables/examples	RootConfig	inputs/conformance-str-surrogate-pair.json	conformance hostile vector testdata/conformance/tables/json-hostile/str-surrogate-pair	accept	-	1667969d0b15bff7ece62c5bdb24ea0fead94b2504b3b18edae9d1c664b700eb
conformance-trailing-commas	text	tables/examples	RootConfig	inputs/conformance-trailing-commas.json	conformance hostile vector testdata/conformance/tables/json-hostile/trailing-commas	accept	-	434a7d677d4de44c3280acbeb79b5d0c6659b657c0d8fd6870e5331474a4a4ec
conformance-ufixed-negative	text	tables/scalars	SimState	inputs/conformance-ufixed-negative.json	conformance hostile vector testdata/conformance/tables/json-hostile/ufixed-negative	accept	-	24c37af78e1e7f50240bdee0def68cc34541bada46ad3c8607b21bdcbe3e813c
conformance-uint128-max	text	tables/scalars	SimState	inputs/conformance-uint128-max.json	conformance hostile vector testdata/conformance/tables/json-hostile/uint128-max	accept	-	1616f769e7d383a2d4b51ead1a8ce492e653ce9e213305cfe9b98ae335cd2c23
conformance-uint128-negative	text	tables/scalars	SimState	inputs/conformance-uint128-negative.json	conformance hostile vector testdata/conformance/tables/json-hostile/uint128-negative	accept	-	a142f50a4d07c072605bab0321d0a1fb8b4abef75f9e5d20e3f1cc69c407e06d
conformance-uint128-over	text	tables/scalars	SimState	inputs/conformance-uint128-over.json	conformance hostile vector testdata/conformance/tables/json-hostile/uint128-over	accept	-	72fe645150a4f1a5523f1803968cb51e773baaa3a261eb419767ab3450617242
conformance-union-array-element-shape	text	tables/messages	ToolMessage	inputs/conformance-union-array-element-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-array-element-shape	accept	-	5f514a69c11325eb77eb2b0768b70b2a153014a3c5e79b573f0de6d848448c1e
conformance-union-array-element-two-keys	text	tables/messages	ToolMessage	inputs/conformance-union-array-element-two-keys.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-array-element-two-keys	refuse	damaged	-
conformance-union-array-element-unknown	text	tables/messages	ToolMessage	inputs/conformance-union-array-element-unknown.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-array-element-unknown	accept	-	5f514a69c11325eb77eb2b0768b70b2a153014a3c5e79b573f0de6d848448c1e
conformance-union-table-arm-nested-unknown	text	tables/messages	ToolMessage	inputs/conformance-union-table-arm-nested-unknown.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-table-arm-nested-unknown	accept	-	7977b97232182ff36bdf5093bc8675b7bd5e70bfd08603f715f46c9b37ffc09c
conformance-union-table-arm-two-keys	text	tables/messages	ToolMessage	inputs/conformance-union-table-arm-two-keys.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-table-arm-two-keys	refuse	damaged	-
conformance-union-table-arm-unknown	text	tables/messages	ToolMessage	inputs/conformance-union-table-arm-unknown.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-table-arm-unknown	accept	-	4994661b9f705d9b0db9ef2439f0f527f69a8f0da1663d5468a192d34cbaf168
conformance-union-table-arm-wrong-shape	text	tables/messages	ToolMessage	inputs/conformance-union-table-arm-wrong-shape.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-table-arm-wrong-shape	accept	-	4994661b9f705d9b0db9ef2439f0f527f69a8f0da1663d5468a192d34cbaf168
conformance-union-two-keys	text	tables/examples	RootConfig	inputs/conformance-union-two-keys.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-two-keys	refuse	damaged	-
conformance-union-unknown-arm	text	tables/examples	RootConfig	inputs/conformance-union-unknown-arm.json	conformance hostile vector testdata/conformance/tables/json-hostile/union-unknown-arm	accept	-	f0f988748fa372cfbfd031b42a9164e937488a18c7aa98b1220a41f71f1fe01f
conformance-unknown-key	text	tables/examples	RootConfig	inputs/conformance-unknown-key.json	conformance hostile vector testdata/conformance/tables/json-hostile/unknown-key	accept	-	be1ec856da2aae75b3516fe73c5eb922f2042132cd1c5570f8936b07d6d211f9
fuzz-text-bare_minus	text	tables/examples	GlobalSettings	inputs/fuzz-text-bare_minus.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/bare_minus	refuse	damaged	-
fuzz-text-comment_refused	text	tables/examples	GlobalSettings	inputs/fuzz-text-comment_refused.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/comment_refused	accept	-	6d5749286871d32eee4f135b31450b74ae38eba5f44cb8e27c822af15fcb8c60
fuzz-text-deep_unknown	text	tables/examples	GlobalSettings	inputs/fuzz-text-deep_unknown.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/deep_unknown	accept	-	eed037befe6af41282eb5bdbd159d684825ff5ea339705b4a575748b5834f748
fuzz-text-illformed_byte_at_bound	text	tables/examples	GlobalSettings	inputs/fuzz-text-illformed_byte_at_bound.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/illformed_byte_at_bound	accept	-	e549756c736d18ce40ceae4f4a51eb0b953b2a1ca8f5c9c983474b8ea733f642
fuzz-text-illformed_bytes_in_string	text	tables/examples	GlobalSettings	inputs/fuzz-text-illformed_bytes_in_string.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/illformed_bytes_in_string	accept	-	3026b6e7bd2c38db6d7bc5da6e5ca1cce1f1fd8ed532b35c2191f761b1ab5036
fuzz-text-integer_overflow	text	tables/examples	GlobalSettings	inputs/fuzz-text-integer_overflow.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/integer_overflow	accept	-	6aa4a7bec05532fbee489233fceb38b8ed795db26f96a1d570093d47f3a87490
fuzz-text-invalid_utf8_tail	text	tables/examples	GlobalSettings	inputs/fuzz-text-invalid_utf8_tail.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/invalid_utf8_tail	accept	-	cf054b987774b0c443f4838b823693d0c7210a3a2d2bee34c58ba917f5212bf9
fuzz-text-leading_zeros	text	tables/examples	GlobalSettings	inputs/fuzz-text-leading_zeros.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/leading_zeros	refuse	damaged	-
fuzz-text-lone_lead_surrogate	text	tables/examples	GlobalSettings	inputs/fuzz-text-lone_lead_surrogate.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/lone_lead_surrogate	accept	-	cf054b987774b0c443f4838b823693d0c7210a3a2d2bee34c58ba917f5212bf9
fuzz-text-null_scalar	text	tables/examples	GlobalSettings	inputs/fuzz-text-null_scalar.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/null_scalar	accept	-	eed037befe6af41282eb5bdbd159d684825ff5ea339705b4a575748b5834f748
fuzz-text-surrogate_pair	text	tables/examples	GlobalSettings	inputs/fuzz-text-surrogate_pair.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/surrogate_pair	accept	-	46a33da966c2a1394bcd0b6a24e7da064258a6f02b27929c3f8cfcabc9605718
fuzz-text-trailing_comma_nested	text	tables/examples	GlobalSettings	inputs/fuzz-text-trailing_comma_nested.txt	go fuzz corpus internal/tabletext/testdata/fuzz/FuzzReadText/trailing_comma_nested	accept	-	251e478472c95a4ab76e17e515dd516e712ee2805d2a3d0a76e099756a4d2633
message-batch-too-large	message	tables/messages	ToolMessage	inputs/message-batch-too-large.bin	bound: a batch of 2 against storage of 1 (one above the caller's capacity)	refuse	batch_too_large	-
message-no-vocabulary	message_novocab	tables/messages	ToolMessage	inputs/message-no-vocabulary.bin	edge: a form-2 body before any announcement	refuse	no_vocabulary	-
message-valid	message	tables/messages	ToolMessage	inputs/message-valid.bin	hand-built message accept case from the engine's own encoder	accept	-	c88bfa21718fcd215630914fe8b22bcf354a2ebd6059efe685052755e24e612f
text-valid	text	tables/examples	ShipEntry	inputs/text-valid.json	hand-built text accept case	accept	-	644e47689a50819987e8efad23f7e361052f2d05d83642293634e4e28bb1f712
wire-count-refusal	wire	schemas/probe.schema	Root	inputs/wire-count-refusal.bin	bound: a collection count past the int32 storage cap (internal/tablewire/decodenodes.go CountRefusal)	refuse	count_refusal	-
wire-empty	wire	tables/examples	ShipEntry	inputs/wire-empty.bin	bounds: zero bytes is below the smallest readable wire	refuse	damaged	-
wire-form-02	wire	tables/examples	ShipEntry	inputs/wire-form-02.bin	bound: the message form offered where a file was expected	refuse	message_form_as_file	-
wire-form-ff	wire	tables/examples	ShipEntry	inputs/wire-form-ff.bin	bound: a form byte past the message form (2), one above the highest form the reader carries	refuse	newer_form	-
wire-truncated-tail	wire	tables/examples	ShipEntry	inputs/wire-truncated-tail.bin	bounds: the wire body cut short of its declared length	refuse	damaged	-
wire-valid	wire	tables/examples	ShipEntry	inputs/wire-valid.bin	hand-built accept case from the engine's own encode	accept	-	0bfa420b0f4335197fa96598a7d246852cee1559275a6463f1f2f0f2532aec2e
`
// HOSTILE-MANIFEST-END

// hostileProbeSchema is the variable root used only by the CountRefusal case.
const hostileProbeSchema = "// probe.schema — a variable root whose one collection is what the engine's\n// builder-path CountRefusal can name (internal/tablewire/decodenodes.go): a\n// count past the int32 storage cap refuses the partial value before an entry\n// is read. It exists only so the hostile corpus can carry that verdict.\npackage probe\n\ntable Root\n{\n    entries map[int32]int32\n}\n"

// hostileInputs is every raw payload, base64 so arbitrary bytes survive a Go
// source file. The key is the manifest's input path without the "inputs/"
// prefix; base64 is the only encoding and a port decodes it with any base64.
var hostileInputs = map[string]string{
	"announcement-newer-form.bin":                          "/w==",
	"announcement.bin":                                     "AQEJTgoQ2Q4PSTECDpcNBpQNxWfE8B/9VKMNdKJ5RI7i5bEEAQcA0TH+9hgWd2oEAQMA5rTnijXR+e4KAMaHXIA6YtyaCgBpabGifv4TgQQBBwDz6n1hm4VfpggBCAFwSm5MpzV/Rh5gnVTCHJLsFAwwcRIFsBNWrgkOAwMKAGpkASJmo1q3CgC8sAJGmnG/pgEYiIS5J0bEWwwYz6mLKLXUaX8KALEKe86iVzcfCgCMYIPCCyb4hBADDW36qKVIsK66EAMN/9iUVsLDCs4QAw2yD0AnC2uYAQ3UisR3KZuoMh7FmfeCdk4K2Q4ABB5JJdcjWvRvtA4DAx5K5ZZwhPUGSwkBAwA56YP3fxMxvw0kzIoR9fAo3g4CAg1BmiRAA6oB+Q4ACA036giYLMZiuwgAzklBtVc1tH8NRK3hE0lcSikQAw1xFU40CpTanBADBAEKACjCAdLMf3B3DgADDW8MbwMLeYBlAUAOIKCKSYEiCgC9z6pVfw5/JAFQUKIVwJq8twQBCgDURRi92FjHuAoA3XxY0br7+DsMCIYbY466rbzEDCDwkiBsxUz/2w4AEAYAabmAH1Jti6sIAMavmy7viFAeAgB1D8qQkYIosQMAAzT1jjOS3F0FABhVCf6Bqb0QBgCmP5OoDdssjAcApmtuGdGXlvwJAPcFvbr1J4QoC/mwDD2K6yG5DgQECgA8JY4UCl5voAGjtbuGdc5ZVw21LnC/ERUSSAIBCP8BfQ8JaDH+K5QCAQj/AdFdtQXRrILRAgEI/QGBKpHII+2d7wIBCP0BtGA3ok5XVbYDARD//wPeK3qNr7MX0gMBEP//AwB5EslwL2WQAwEQ/f8DWLFNNXydZWoDARD9/wPWkSxTiL2T5gQBIP////8PtPuZzyfoXgYEASD/////D+6GocLEIbHJBAEg/f///w968qxlpN9j0wQBIP3///8Pb53EAAfHSXUFAUD///////////8B23GXQRdmzhwFAUD///////////8Bl1ULYmD6VDsFAUD9//////////8BI13Ojrn8CNMFAUD9//////////8BfRm2hWveDMcOAAQDARD//wMhwDd/VZeIDwYBCAChogCCP1UXLgYBCAB9GzD5DcbloAYBCAFNhPWmc8CyHAYBCAFgCXmxUMGgTAcBEACyTLA2aBNS8gcBEAAMYecW8uE3TwcBEAEEM4+XBbZ20QcBEAHanEfnTZILIAgBIACwrLUxKi89pQgBIADS46Shjr5ZlwgBIAEOPC4I/BXFjQgBIAFzid/bEe/svgkBQAAn8v/gZq0XAQkBQADbiQa181q08gkBQAH3yut+ARI+cQkBQAHlUa5avv5Bww4ABAkBQADHjU21BwymCAYBCAC+l60SGXCV/wcBEABwlLMSGVyc/wgBIADnYasSGXCS/wkBQACKnq0SGXSV/wcBDAChNaUSGWiL/wkBMACU00aHcslnigwQtqt/uAHZy0EOAAgNZ2dDwB/mgYEOAAQNYBYOmnOtD/EQAwQBEQBdWr1mzdchLQwgcyuonODM0JUOAAQEAQQAqkTNwEi220ANdLZd1uKZ7M4EAQcAzwygx7HaoLwMEMB/s4q+CGN/CgBIPTRTab4s3AoA0rGsAlcFMxcKADLpE54wxTFPBAEEAKTtytWaPgGlBgEGAFk/LmC8h2WtAZRD8lE7yME2Dz1iy4/s/Pc5DPCiBOXptWPQqbjPDgDwogQGANzdSDtqyrHjDgDwogQHAJsfzRxsnj1IAFKN+DAdEvOfAHwoh3XYtcdYAJHFxKo5d5LEAIAl8ereGuXDALMYMg1+yRbEAPQsjOjLpmGuAB+8DULhJE0zANsjDtCjISNsAMKFUsHD9xHQAIrLVMWhaiGoAKXBGVdiPFd9AHwbrP4Z3vGfAC0+acGn0/PsABw/ldWP1wDPAHx2Lju+3lRYAOODRVouWSi1AHZS/6iuFtwEAMxp5OKbvrX/DdcvPeftxc0TDf//////////AORPHE9HwC4vAFj8r/rY4EtwAMfUeyawnSlfAMfDGybPez5BAFshzvax37H/AGQbQRVG4MtfAK83MyWyQmi3AJB4vd8wsWYwAM/uTenkOmPWAKHMjaKuDKqmAG4zFLFdgi3QAHsCKSUcLhgcAHinnYW3/P8ZAC6+KNoddXnsAPGL0rfEwB4DANdeT55OaVm/AIde9N72Q0hfACkaF9oRmRodADoR+xFdVfgcABWtshYMup1GAMOwxIYNeHcFAFFkNtNhaWqrAAD+//////////3/////////AgAAAAAAAAA=",
	"conformance-arm-pointer-null.json":                    "eyAiaWQiOiAxLCAiZnJhbWUiOiB7ICJsaW5rIjogbnVsbCB9LCAicGFydHMiOiBbXSB9Cg==",
	"conformance-arm-scalar-null.json":                     "eyAic2VxdWVuY2UiOiAyLCAiYm9keSI6IHsgInRyYW5zYWN0IjogeyAiZWRpdHMiOiBbIHsgInJldmlzaW9uIjogMSwgImJvZHkiOiB7ICJ0YWxseSI6IG51bGwgfSB9IF0gfSB9IH0K",
	"conformance-arm-scalar-string.json":                   "eyAic2VxdWVuY2UiOiAxLCAiYm9keSI6IHsgInRyYW5zYWN0IjogeyAiZWRpdHMiOiBbIHsgInJldmlzaW9uIjogMSwgImJvZHkiOiB7ICJ0YWxseSI6ICI3IiB9IH0gXSB9IH0gfQo=",
	"conformance-arm-void-not-null.json":                   "eyAic2VxdWVuY2UiOiAzLCAiYm9keSI6IHsgImFjayI6IDUgfSB9Cg==",
	"conformance-array-past-bound.json":                    "eyAicHJvZmlsZXMiOiBbIHsgInJhdGluZ3MiOiBbIDEuMCwgMi4wLCAzLjAsIDQuMCwgNS4wLCA2LjAgXSB9IF0gfQo=",
	"conformance-base64-empty.json":                        "eyAicHJvZmlsZXMiOiBbIHsgImljb24iOiAiIiB9IF0gfQo=",
	"conformance-base64-unpadded.json":                     "eyAicHJvZmlsZXMiOiBbIHsgImljb24iOiAiQVFJREJBIiB9IF0gfQo=",
	"conformance-bits-at-max.json":                         "eyAid2VhcG9ucyI6IFsgeyAiY2hhbm5lbCI6IDYzIH0gXSB9Cg==",
	"conformance-bits-over-range.json":                     "eyAid2VhcG9ucyI6IFsgeyAiY2hhbm5lbCI6IDY0IH0gXSB9Cg==",
	"conformance-blob-empty-string.json":                   "eyAidGh1bWIiOiAiIiwgIm5vdGUiOiAiIiB9Cg==",
	"conformance-blob-label-in-skipped-value.json":         "eyAibmFtZSI6ICJvYmoiLCAidGh1bWIiOiB7ICImbm9kZSI6IDEsICJ4IjogMSB9LCAiaGVhZCI6IHsgImRhdGEiOiB7ICImbm9kZSI6IDEgfSB9IH0K",
	"conformance-blob-not-base64.json":                     "eyAibmFtZSI6ICJiYWQiLCAidGh1bWIiOiAibm90KmJhc2U2NCEiIH0K",
	"conformance-blob-null.json":                           "eyAidGh1bWIiOiBudWxsLCAibm90ZSI6IG51bGwsICJoZWFkIjogbnVsbCB9Cg==",
	"conformance-blob-wrong-shape.json":                    "eyAibmFtZSI6ICJudW0iLCAidGh1bWIiOiA0MiwgIm5vdGUiOiBbIDEgXSB9Cg==",
	"conformance-bytes-not-base64.json":                    "eyAicHJvZmlsZXMiOiBbIHsgImljb24iOiAibm90IGJhc2U2NCEiIH0gXSB9Cg==",
	"conformance-bytes-overlong.json":                      "eyAicHJvZmlsZXMiOiBbIHsgImljb24iOiAiQUFFQ0F3UUZCZ2NJQ1FvTERBME9EeEFSRWhNVUZSWVgiIH0gXSB9Cg==",
	"conformance-comment-block-between.json":               "eyAidmVyc2lvbl9ub3RlIjogLyogYmV0d2VlbiBhIGtleSBhbmQgaXRzIHZhbHVlICovICJhIiwgLy8gYWZ0ZXIgYSB2YWx1ZQp9Cg==",
	"conformance-comment-block-first.json":                 "eyAvKiBhIG5vdGUgYmVmb3JlIHRoZSBmaXJzdCBrZXkgKi8gInZlcnNpb25fbm90ZSI6ICJhIiB9Cg==",
	"conformance-comment-inside-string.json":               "eyAidmVyc2lvbl9ub3RlIjogIi8vIG5vdCAvKiIgfQo=",
	"conformance-comment-last-line.json":                   "eyAidmVyc2lvbl9ub3RlIjogImEiIH0KLy8gdGhlIGxhc3QgbGluZSwgd2l0aCBubyB0cmFpbGluZyBuZXdsaW5l",
	"conformance-comment-line-first.json":                  "Ly8gYSBub3RlIGJlZm9yZSB0aGUgZmlyc3Qga2V5CnsgInZlcnNpb25fbm90ZSI6ICJhIiB9Cg==",
	"conformance-comment-slash-star-slash.json":            "eyAvKi8gInZlcnNpb25fbm90ZSI6ICJhIiB9Cg==",
	"conformance-comment-unclosed.json":                    "eyAidmVyc2lvbl9ub3RlIjogImEiIC8qIG9wZW4K",
	"conformance-dup-key-array.json":                       "eyAicHJvZmlsZXMiOiBbIHsgInJhdGluZ3MiOiBbIDEuMCwgMi4wLCAzLjAsIDQuMCBdLCAicmF0aW5ncyI6IFsgOS4wIF0gfSBdIH0K",
	"conformance-dup-key-placeable.json":                   "eyAicHJvZmlsZXMiOiBbIHsgInRpbHQiOiAxMDAsICJ0aWx0IjogNyB9IF0gfQo=",
	"conformance-dup-key-rejected-element.json":            "eyAicHJvZmlsZXMiOiBbIHsgImhhc19sb2Fkb3V0IjogdHJ1ZSwgImxvYWRvdXQiOiB7ICJhdHRhY2htZW50cyI6IFsgeyAic2xvdCI6IDIsICJzbG90IjogMS41IH0gXSB9IH0gXSB9Cg==",
	"conformance-dup-key-rejected-float.json":              "eyAicHJvZmlsZXMiOiBbIHsgInByZWNpc2lvbiI6IDEuNSwgInByZWNpc2lvbiI6IDFlNDAwIH0gXSB9Cg==",
	"conformance-dup-key-rejected-keyed.json":              "eyAidGhyZXNob2xkcyI6IHsgIkhhcmQiOiA2NCwgIkhhcmQiOiAwLjEgfSB9Cg==",
	"conformance-dup-key-rejected-nested.json":             "eyAicHJvZmlsZXMiOiBbIHsgImhhc19sb2Fkb3V0IjogdHJ1ZSwgImxvYWRvdXQiOiB7ICJwcmltYXJ5IjogeyAiZGFtYWdlIjogMy4wLCAiZGFtYWdlIjogMWU0MDAgfSB9IH0gXSB9Cg==",
	"conformance-dup-key-rejected-scalar.json":             "eyAicHJvZmlsZXMiOiBbIHsgInRpbHQiOiAxMDAsICJ0aWx0IjogNWUtMzI0IH0gXSB9Cg==",
	"conformance-dup-key-table.json":                       "eyAicHJvZmlsZXMiOiBbIHsgImhhc19sb2Fkb3V0IjogdHJ1ZSwgImxvYWRvdXQiOiB7ICJncmFkZSI6ICJHb2xkIiwgInBlcmtzIjogWyAiVHVyYm8iIF0gfSwgImxvYWRvdXQiOiB7ICJncmFkZSI6ICJCcm9uemUiIH0gfSBdIH0K",
	"conformance-dup-key-union.json":                       "eyAid2VhcG9ucyI6IFsgeyAiZWZmZWN0IjogeyAiYnVmZiI6IHsgIm11bHRpcGxpZXIiOiAzLjAgfSB9LCAiZWZmZWN0IjogeyAiZGVidWZmIjogeyAiYW1vdW50IjogNCB9IH0gfSBdIH0K",
	"conformance-dup-key-wrong-shape.json":                 "eyAicHJvZmlsZXMiOiBbIHsgInRpbHQiOiAxMDAsICJ0aWx0IjogIngiIH0gXSB9Cg==",
	"conformance-dup-key.json":                             "eyAidmVyc2lvbl9ub3RlIjogImEiLCAidmVyc2lvbl9ub3RlIjogImIiIH0K",
	"conformance-enum-unknown-name.json":                   "eyAicHJvZmlsZXMiOiBbIHsgImhhc19sb2Fkb3V0IjogdHJ1ZSwgImxvYWRvdXQiOiB7ICJncmFkZSI6ICJQbGF0aW51bSIgfSB9IF0gfQo=",
	"conformance-fixed-array-elements.json":                "eyAic2FtcGxlcyI6IFsgMC4yNSwgOS4wLCAwLjEgXSwgIndlaWdodHMiOiBbIDEwMC41LCAwLjAwMzkwNjI1IF0sICJheGVzIjogeyAiWSI6IDFlLTMgfSB9Cg==",
	"conformance-fixed-at-bound.json":                      "eyAidGlsdCI6IC04LjAsICJyYXRpbyI6IDE1LjAsICJhbmdsZSI6IDE4MC4wLCAicmVhY2giOiAtMTAwMDAwMC4wLCAic3BhbiI6IDI4MTQ3NDk3NjcxMDY1NSB9Cg==",
	"conformance-fixed-below-resolution.json":              "eyAiYW5nbGUiOiAxZS05OTk5OTk5OTksICJzY2FsZSI6IDAuMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDEgfQo=",
	"conformance-fixed-exact-fraction.json":                "eyAiYW5nbGUiOiAtMC4wMDAwMTUyNTg3ODkwNjI1LCAic2NhbGUiOiA3Ljk5OTk4NDc0MTIxMDkzNzUwLCAidGlsdCI6IDAuMDYyNSB9Cg==",
	"conformance-fixed-fraction-too-fine.json":             "eyAiYW5nbGUiOiAwLjEgfQo=",
	"conformance-fixed-huge-exponent.json":                 "eyAiYW5nbGUiOiAxZTk5OTk5OTk5OSwgInNwZWVkIjogLTFlNDAwIH0K",
	"conformance-fixed-integral-token.json":                "eyAiYW5nbGUiOiAyLCAicG9zaXRpb24iOiAyLjAsICJzcGVlZCI6IDE1ZS0xLCAidGlja3MiOiAxZTMgfQo=",
	"conformance-fixed-negative-zero.json":                 "eyAiYW5nbGUiOiAtMC4wLCAic3BlZWQiOiAtMCB9Cg==",
	"conformance-fixed-nested-type.json":                   "eyAicG9zZSI6IHsgIngiOiAtMC41LCAiaGVhZGluZyI6IDM2MC41IH0sICJzcGF3biI6IHsgInkiOiAxZS0yIH0gfQo=",
	"conformance-fixed-over-range.json":                    "eyAiYW5nbGUiOiAxODAuNSwgInBvc2l0aW9uIjogLTMwMDAwLjI1LCAic3BlZWQiOiAxMDAwLjAwMDAxNTI1ODc4OTA2MjUgfQo=",
	"conformance-fixed-wrong-shape.json":                   "eyAiYW5nbGUiOiAiMS41IiwgImVudGl0eV9pZCI6IHRydWUgfQo=",
	"conformance-flags-unknown-name.json":                  "eyAicHJvZmlsZXMiOiBbIHsgImhhc19sb2Fkb3V0IjogdHJ1ZSwgImxvYWRvdXQiOiB7ICJwZXJrcyI6IFsgIlNoaWVsZGVkIiwgIk5vcGUiIF0gfSB9IF0gfQo=",
	"conformance-guard-key-after-fields.json":              "eyAicHJvZmlsZXMiOiBbIHsgImxvYWRvdXQiOiB7ICJncmFkZSI6ICJHb2xkIiB9LCAiaGFzX2xvYWRvdXQiOiB0cnVlIH0gXSB9Cg==",
	"conformance-int128-beyond-int64.json":                 "eyAiZmx1eCI6IDEwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAsICJlbmVyZ3kiOiAtNTAwMDAwMDAwMCB9Cg==",
	"conformance-int128-fractional.json":                   "eyAiZmx1eCI6IDEuNSB9Cg==",
	"conformance-int128-integral-forms.json":               "eyAiZmx1eCI6IDIuMCwgImVuZXJneSI6IC0xZTksICJlbnRpdHlfaWQiOiAzZTM4IH0K",
	"conformance-int128-over-range.json":                   "eyAiZmx1eCI6IDEyNjc2NTA2MDAyMjgyMjk0MDE0OTY3MDMyMDUzNzcsICJlbmVyZ3kiOiA1MDAwMDAwMDAxIH0K",
	"conformance-int128-saturated-below.json":              "eyAiZmx1eCI6IC0xZTQwIH0K",
	"conformance-json-key-declared-name.json":              "eyAic2hpcHMiOiB7ICJGaWdodGVyIjogeyAiZGlzcGxheV9uYW1lIjogIngiLCAibmFtZSI6ICJvayIgfSB9IH0K",
	"conformance-keyed-duplicate-mixed.json":               "eyAic2hpcHMiOiB7ICJTY291dCI6IHsgIm5hbWUiOiAiQSIgfSwgIkZyaWdhdGUiOiB7IH0sICJTY291dCI6IHsgIm5hbWUiOiAiQiIgfSwgIkZyaWdhdGUiOiB7IH0gfSB9Cg==",
	"conformance-keyed-duplicate-none.json":                "eyAic2hpcHMiOiB7ICJOb25lIjogeyAibmFtZSI6ICJBIiB9LCAiTm9uZSI6IHsgIm5hbWUiOiAiQiIgfSB9IH0K",
	"conformance-keyed-duplicate-slot.json":                "eyAidGhyZXNob2xkcyI6IHsgIkhhcmQiOiAxMDAsICJIYXJkIjogMzAwIH0gfQo=",
	"conformance-keyed-duplicate-unknown.json":             "eyAic2hpcHMiOiB7ICJGcmlnYXRlIjogeyAibmFtZSI6ICJBIiB9LCAiRnJpZ2F0ZSI6IHsgIm5hbWUiOiAiQiIgfSB9IH0K",
	"conformance-keyed-none-key.json":                      "eyAic2hpcHMiOiB7ICJOb25lIjogeyAibmFtZSI6ICJOIiB9IH0gfQo=",
	"conformance-keyed-unknown-key.json":                   "eyAic2hpcHMiOiB7ICJGcmlnYXRlIjogeyAibmFtZSI6ICJGIiB9IH0gfQo=",
	"conformance-keyed-wrong-shape.json":                   "eyAic2hpcHMiOiBbIF0gfQo=",
	"conformance-label-before-definition.json":             "eyAiaGVhZCI6IHsgIiZub2RlIjogMSB9LCAiYWxpYXMiOiB7ICImbm9kZSI6IDEsICJ2YWx1ZSI6IDEgfSB9Cg==",
	"conformance-label-defined-twice.json":                 "eyAiaGVhZCI6IHsgIiZub2RlIjogMSwgInZhbHVlIjogMSB9LCAiYWxpYXMiOiB7ICImbm9kZSI6IDEsICJ2YWx1ZSI6IDIgfSB9Cg==",
	"conformance-label-dropped-definition.json":            "eyAibGF5ZXJzIjogWyB7fSwge30sIHt9LCB7fSwgeyAiaGVhZCI6IHsgIiZub2RlIjogMSwgInZhbHVlIjogMSB9IH0gXSwgImFsaWFzIjogeyAiJm5vZGUiOiAxIH0gfQo=",
	"conformance-label-in-by-value-nesting.json":           "eyAiZ3JvdW5kIjogeyAiJm5vZGUiOiAxIH0gfQo=",
	"conformance-label-in-unknown-key.json":                "eyAibXlzdGVyeSI6IHsgIiZub2RlIjogOSwgInZhbHVlIjogMSB9LCAiaGVhZCI6IHsgIiZub2RlIjogOSB9IH0K",
	"conformance-label-leading-zero.json":                  "eyAiaGVhZCI6IHsgIiZub2RlIjogMDEsICJ2YWx1ZSI6IDEgfSB9Cg==",
	"conformance-label-negative.json":                      "eyAiaGVhZCI6IHsgIiZub2RlIjogLTEsICJ2YWx1ZSI6IDEgfSB9Cg==",
	"conformance-label-not-an-integer.json":                "eyAiaGVhZCI6IHsgIiZub2RlIjogMS4wIH0gfQo=",
	"conformance-label-not-first-key.json":                 "eyAiaGVhZCI6IHsgInZhbHVlIjogMSwgIiZub2RlIjogMSB9IH0K",
	"conformance-label-on-the-root.json":                   "eyAiJm5vZGUiOiAxLCAibmFtZSI6ICJyb290IiB9Cg==",
	"conformance-label-overflow.json":                      "eyAiaGVhZCI6IHsgIiZub2RlIjogMTg0NDY3NDQwNzM3MDk1NTE2MTYsICJ2YWx1ZSI6IDEgfSB9Cg==",
	"conformance-label-self-reference.json":                "eyAiaGVhZCI6IHsgIiZub2RlIjogMSwgInZhbHVlIjogMSwgIm5leHQiOiB7ICImbm9kZSI6IDEgfSB9IH0K",
	"conformance-label-type-mismatch.json":                 "eyAic2V0dGluZ3MiOiB7ICImbm9kZSI6IDEsICJxdWFsaXR5IjogMSB9LCAiaGVhZCI6IHsgIiZub2RlIjogMSB9IH0K",
	"conformance-label-undefined.json":                     "eyAiaGVhZCI6IHsgIiZub2RlIjogMSB9IH0K",
	"conformance-label-zero.json":                          "eyAiaGVhZCI6IHsgIiZub2RlIjogMCB9IH0K",
	"conformance-nesting-past-depth-cap.json":              "eyAibXlzdGVyeSI6IFtbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbW1tbXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV1dXV0gfQo=",
	"conformance-null-optional.json":                       "eyAic2hpcHMiOiB7ICJGaWdodGVyIjogeyAibmFtZSI6ICJGIiwgImd1bm5lciI6IG51bGwgfSB9IH0K",
	"conformance-null-scalar.json":                         "eyAidmVyc2lvbiI6IG51bGwgfQo=",
	"conformance-num-bare-minus.json":                      "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAxLTIgfSBdIH0K",
	"conformance-num-double-dot.json":                      "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAxLi4yIH0gXSB9Cg==",
	"conformance-num-double-minus.json":                    "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAtLTMgfSBdIH0K",
	"conformance-num-float32-midpoint.json":                "eyAicHJvZmlsZXMiOiBbIHsgInJhdGluZ3MiOiBbIDM0MDI4MjM1Njc3OTczMzY2MTYzNzUzOTM5NTQ1ODE0MjU2ODQ0OCwgMC4wLCAwLjAsIDAuMCBdIH0gXSB9Cg==",
	"conformance-num-float32-overflow.json":                "eyAicHJvZmlsZXMiOiBbIHsgInJhdGluZ3MiOiBbIDFlMzksIDAuMCwgMC4wLCAwLjAgXSB9IF0gfQo=",
	"conformance-num-float32-subnormal.json":               "eyAicHJvZmlsZXMiOiBbIHsgInJhdGluZ3MiOiBbIDcuMDA2NDkyMzIxNjI0MDg1MzU0N2UtNDYsIC03LjAwNjQ5MjMyMTYyNDA4NTM1NDdlLTQ2LCAxLjQwMTI5ODQ2NDNlLTQ1LCAwLjAgXSB9IF0gfQo=",
	"conformance-num-float32-upper-band.json":              "eyAicHJvZmlsZXMiOiBbIHsgInJhdGluZ3MiOiBbIDM0MDI4MjM1Njc3OTczMzY2MTYzNzUzOTM5NTQ1ODE0MjU2ODQ0NywgLTM0MDI4MjM1Njc3OTczMzY2MTYzNzUzOTM5NTQ1ODE0MjU2ODQ0MCwgMzQwMjgyMzU2Nzc5NzMzNjYxNjM3NTM5Mzk1NDU4MDAwMDAwMDAwLCAwLjAgXSB9IF0gfQo=",
	"conformance-num-float64-overflow.json":                "eyAicHJvZmlsZXMiOiBbIHsgInByZWNpc2lvbiI6IDFlNDAwIH0gXSB9Cg==",
	"conformance-num-fraction.json":                        "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAxLjUgfSBdIH0K",
	"conformance-num-int-overflow.json":                    "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiA5OTk5OTk5OTk5OTk5OTk5OTk5OTk5OTk5OSB9IF0gfQo=",
	"conformance-num-int64-min.json":                       "eyAicHJvZmlsZXMiOiBbIHsgInRpbWVzdGFtcCI6IC05MjIzMzcyMDM2ODU0Nzc1ODA4IH0gXSB9Cg==",
	"conformance-num-integral-forms.json":                  "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAxZTMsICJ0aWx0IjogMi4wLCAiaGVhZGluZyI6IDFFMiwgInBvcnQiOiAxZSsyLCAiZXBvY2giOiAwZTAsICJwcmVjaXNpb24iOiAxLjBlMSB9IF0gfQo=",
	"conformance-num-leading-plus.json":                    "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiArNyB9IF0gfQo=",
	"conformance-num-leading-zero.json":                    "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAwMDcgfSBdIH0K",
	"conformance-num-negative-unsigned.json":               "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAtMSB9IF0gfQo=",
	"conformance-num-negative-zero.json":                   "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAtMCB9IF0gfQo=",
	"conformance-num-no-exp-digits.json":                   "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAxZSB9IF0gfQo=",
	"conformance-num-no-int.json":                          "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAuNSB9IF0gfQo=",
	"conformance-num-three-part.json":                      "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAxLjIuMyB9IF0gfQo=",
	"conformance-num-trailing-dot.json":                    "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAzLiB9IF0gfQo=",
	"conformance-num-trailing-plus.json":                   "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiA1KyB9IF0gfQo=",
	"conformance-num-two-exponents.json":                   "eyAicHJvZmlsZXMiOiBbIHsgImV4cGVyaWVuY2UiOiAxZTJlMyB9IF0gfQo=",
	"conformance-optional-array-element-shape.json":        "eyAidHJhY2UiOiBbIDUgXSB9Cg==",
	"conformance-optional-array-null.json":                 "eyAidHJhY2UiOiBudWxsIH0K",
	"conformance-optional-array-shape.json":                "eyAidHJhY2UiOiA1IH0K",
	"conformance-optional-present-empty.json":              "eyAic2hpcHMiOiB7ICJTY291dCI6IHsgImd1bm5lciI6IHt9IH0gfSB9Cg==",
	"conformance-optional-wrong-shape.json":                "eyAic2hpcHMiOiB7ICJGaWdodGVyIjogeyAibmFtZSI6ICJGIiwgImd1bm5lciI6IDUgfSB9IH0K",
	"conformance-pointer-array-element-shape.json":         "eyAiaWQiOiAxLCAiZnJhbWUiOiB7ICJoZWFkZXIiOiB7ICJuYW1lIjogImgiIH0gfSwgInBhcnRzIjogWyA1LCBudWxsIF0gfQo=",
	"conformance-pointer-array-shared-label.json":          "eyAiaWQiOiAyLCAiZnJhbWUiOiB7fSwgInBhcnRzIjogWyB7ICImbm9kZSI6IDEsICJkYXRhIjogIkFRPT0iLCAibmV4dCI6IG51bGwgfSwgbnVsbCwgeyAiJm5vZGUiOiAxIH0gXSwgInBhaXIiOiBbIHsgIiZub2RlIjogMSB9LCBudWxsIF0gfQo=",
	"conformance-reserved-ampersand-in-skipped-value.json": "eyAibXlzdGVyeSI6IHsgIiZub2RlIjogMSB9IH0K",
	"conformance-reserved-ampersand-key.json":              "eyAidmVyc2lvbl9ub3RlIjogIngiLCAiJm5vZGUiOiAxIH0K",
	"conformance-shared-node-by-label.json":                "eyAiaGVhZCI6IHsgIiZub2RlIjogMSwgInZhbHVlIjogNywgIm5hbWUiOiAib25lIiB9LCAiYWxpYXMiOiB7ICImbm9kZSI6IDEgfSwgImdyb3VuZCI6IHsgImRlcHRoIjogMSwgImhlYWQiOiB7ICImbm9kZSI6IDEgfSB9IH0K",
	"conformance-str-clamp-boundary.json":                  "eyAicHJvZmlsZXMiOiBbIHsgIm5hbWUiOiAiw6nDqcOpw6nDqcOpw6nDqcOpw6nDqcOpw6nDqcOpw6nDqcOpw6nDqcOpIiB9IF0gfQo=",
	"conformance-str-illformed-at-bound.json":              "eyAicHJvZmlsZXMiOiBbIHsgIm5hbWUiOiAiYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYf8iIH0gXSB9Cg==",
	"conformance-str-lone-lead-surrogate.json":             "eyAicHJvZmlsZXMiOiBbIHsgIm5hbWUiOiAiXHVEODAweCIgfSBdIH0K",
	"conformance-str-lone-trail-surrogate.json":            "eyAicHJvZmlsZXMiOiBbIHsgIm5hbWUiOiAiXHVEQzAweCIgfSBdIH0K",
	"conformance-str-surrogate-pair.json":                  "eyAicHJvZmlsZXMiOiBbIHsgIm5hbWUiOiAi8J+YgCIgfSBdIH0K",
	"conformance-trailing-commas.json":                     "ewogICJ2ZXJzaW9uX25vdGUiOiAiYSIsCiAgIndlYXBvbnMiOiBbIHsgImRhbWFnZSI6IDEuMCwgfSwgXSwKfQo=",
	"conformance-ufixed-negative.json":                     "eyAic3BlZWQiOiAtMC41LCAicmF0aW8iOiAtMSB9Cg==",
	"conformance-uint128-max.json":                         "eyAiZW50aXR5X2lkIjogMzQwMjgyMzY2OTIwOTM4NDYzNDYzMzc0NjA3NDMxNzY4MjExNDU1LCAic2VlZHMiOiBbIDM0MDI4MjM2NjkyMDkzODQ2MzQ2MzM3NDYwNzQzMTc2ODIxMTQ1NSwgMCBdIH0K",
	"conformance-uint128-negative.json":                    "eyAiZW50aXR5X2lkIjogLTEsICJzZWVkcyI6IFsgLTAgXSB9Cg==",
	"conformance-uint128-over.json":                        "eyAiZW50aXR5X2lkIjogMzQwMjgyMzY2OTIwOTM4NDYzNDYzMzc0NjA3NDMxNzY4MjExNDU2LCAic2VlZHMiOiBbIDFlNDAgXSB9Cg==",
	"conformance-union-array-element-shape.json":           "eyAiaGlzdG9yeSI6IFsgNSBdIH0K",
	"conformance-union-array-element-two-keys.json":        "eyAiaGlzdG9yeSI6IFsgeyAib3BlbiI6IHsgInBhdGgiOiAiYS5tZCIgfSwgInNhdmUiOiB7ICJwYXRoIjogImIubWQiIH0gfSBdIH0K",
	"conformance-union-array-element-unknown.json":         "eyAiaGlzdG9yeSI6IFsgeyAicmVuYW1lIjogeyAicGF0aCI6ICJhLm1kIiB9IH0gXSB9Cg==",
	"conformance-union-table-arm-nested-unknown.json":      "eyAic2VxdWVuY2UiOiAxLCAiYm9keSI6IHsgInRyYW5zYWN0IjogeyAicmVhc29uIjogIngiLCAiZWRpdHMiOiBbIHsgInJldmlzaW9uIjogMSwgImJvZHkiOiB7ICJpbnNlcnQiOiB7ICJ0ZXh0IjogInQiLCAib3JpZ2luIjogeyAicm9ib3QiOiB7ICJpZCI6IDQgfSB9IH0gfSB9IF0gfSB9IH0K",
	"conformance-union-table-arm-two-keys.json":            "eyAic2VxdWVuY2UiOiAxLCAiYm9keSI6IHsgIm9wZW4iOiB7ICJwYXRoIjogImEubWQiIH0sICJzYXZlIjogeyAicGF0aCI6ICJiLm1kIiB9IH0gfQo=",
	"conformance-union-table-arm-unknown.json":             "eyAic2VxdWVuY2UiOiAxLCAiYm9keSI6IHsgInJlbmFtZSI6IHsgInBhdGgiOiAiYS5tZCIgfSB9IH0K",
	"conformance-union-table-arm-wrong-shape.json":         "eyAic2VxdWVuY2UiOiAxLCAiYm9keSI6IHsgIm9wZW4iOiA1IH0gfQo=",
	"conformance-union-two-keys.json":                      "eyAid2VhcG9ucyI6IFsgeyAiZWZmZWN0IjogeyAiYnVmZiI6IHt9LCAiZGVidWZmIjoge30gfSB9IF0gfQo=",
	"conformance-union-unknown-arm.json":                   "eyAid2VhcG9ucyI6IFsgeyAiZWZmZWN0IjogeyAibm9wZSI6IHt9IH0gfSBdIH0K",
	"conformance-unknown-key.json":                         "eyAibXlzdGVyeSI6IFsgMSwgMiwgMyBdIH0K",
	"fuzz-text-bare_minus.txt":                             "eyAidGlja19yYXRlIjogMS0yIH0=",
	"fuzz-text-comment_refused.txt":                        "eyAvKiBubyAqLyAidGlja19yYXRlIjogMSB9",
	"fuzz-text-deep_unknown.txt":                           "eyAieCI6IFtbW1tbW1tbW1sxXV1dXV1dXV1dXSB9",
	"fuzz-text-illformed_byte_at_bound.txt":                "eyAiYnVpbGRfbm90ZSI6ICJhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYWFhYf8iIH0=",
	"fuzz-text-illformed_bytes_in_string.txt":              "eyJidWlsZF9ub3RlIjoiMDAw9PuJMLLHMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwMDAwIn0=",
	"fuzz-text-integer_overflow.txt":                       "eyAidGlja19yYXRlIjogOTk5OTk5OTk5OTk5OTk5OTk5OTk5OTk5IH0=",
	"fuzz-text-invalid_utf8_tail.txt":                      "eyAiYnVpbGRfbm90ZSI6ICL/IiB9",
	"fuzz-text-leading_zeros.txt":                          "eyAidGlja19yYXRlIjogMDA3IH0=",
	"fuzz-text-lone_lead_surrogate.txt":                    "eyAiYnVpbGRfbm90ZSI6ICJcdUQ4MDAiIH0=",
	"fuzz-text-null_scalar.txt":                            "eyAidGlja19yYXRlIjogbnVsbCB9",
	"fuzz-text-surrogate_pair.txt":                         "eyAiYnVpbGRfbm90ZSI6ICJcdUQ4M0RcdURFMDAiIH0=",
	"fuzz-text-trailing_comma_nested.txt":                  "eyAic3Bhd25fZGVsYXlzIjogWyAxLjAsIDIuMCwgXSwgfQ==",
	"message-batch-too-large.bin":                          "AgEAAA==",
	"message-no-vocabulary.bin":                            "AgA=",
	"message-valid.bin":                                    "AgAA",
	"text-valid.json":                                      "eyAibmFtZSI6ICJBdXJvcmEiLCAiaGFyZHBvaW50cyI6IFsxLCAyXSB9",
	"wire-count-refusal.bin":                               "AQEOCA2AgICACAEAAFOiRQgsp7LFAQAAAAAAAAA=",
	"wire-empty.bin":                                       "",
	"wire-form-02.bin":                                     "Ag==",
	"wire-form-ff.bin":                                     "/w==",
	"wire-truncated-tail.bin":                              "AQAAAAAAAAAA",
	"wire-valid.bin":                                       "AQAAAAAAAAAAAA==",
}

// hostileCase is one manifest line.
type hostileCase struct {
	Name       string
	Carrier    string
	Schema     string
	Table      string
	Input      string
	Provenance string
	Verdict    string
	Reason     string
	Digest     string
}

// hostileParse reads manifest text without asking the engine anything.
func hostileParse(text string) ([]hostileCase, error) {
	var out []hostileCase
	for i, line := range strings.Split(text, "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 9 {
			return nil, fmt.Errorf("manifest line %d: %d fields, want 9", i+1, len(f))
		}
		out = append(out, hostileCase{
			Name: f[0], Carrier: f[1], Schema: f[2], Table: f[3], Input: f[4],
			Provenance: f[5], Verdict: f[6], Reason: f[7], Digest: f[8],
		})
	}
	return out, nil
}

// hostileFormat renders a manifest deterministically, in the exact committed
// shape (header comments, then rows sorted by name).
func hostileFormat(cases []hostileCase) string {
	var b strings.Builder
	b.WriteString("# schema shared hostile-input corpus — line-oriented manifest\n")
	b.WriteString("# name\tcarrier\tschema\ttable\tinput\tprovenance\tverdict\treason\tdigest\n")
	rows := append([]hostileCase(nil), cases...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })
	for _, c := range rows {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			c.Name, c.Carrier, c.Schema, c.Table, c.Input, c.Provenance, c.Verdict, c.Reason, c.Digest)
	}
	return b.String()
}

// hostileEngineReasons is every refusal reason the engine can name, read off
// internal/tablewire's own constants. A corpus that names a reason outside this
// set, or omits one, is a corpus whose coverage a reader cannot trust.
func hostileEngineReasons() []string {
	return []string{
		tablewire.ReasonNewerForm.String(),
		tablewire.ReasonNoVocabulary.String(),
		tablewire.ReasonSecondAnnouncement.String(),
		tablewire.ReasonVocabularyTooLarge.String(),
		tablewire.ReasonMessageFormAsFile.String(),
		tablewire.ReasonBatchTooLarge.String(),
		hostileReasonCountRefusal,
		hostileReasonDamaged,
	}
}

// hostilePayload decodes one embedded input.
func hostilePayload(input string) ([]byte, error) {
	key := strings.TrimPrefix(input, "inputs/")
	text, ok := hostileInputs[key]
	if !ok {
		return nil, fmt.Errorf("input %q is not in the committed corpus", input)
	}
	payload, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, fmt.Errorf("input %q: %w", input, err)
	}
	return payload, nil
}

// hostileLoadModel loads one schema. The probe schema is written to the test's
// temporary directory; every other schema is the checkout's own.
func hostileLoadModel(root, tmp, schema string) (*tabletext.Model, error) {
	path := filepath.Join(root, filepath.FromSlash(schema))
	if schema == hostileProbeSchemaPath {
		path = filepath.Join(tmp, "probe.schema")
		if err := os.WriteFile(path, []byte(hostileProbeSchema), 0o644); err != nil {
			return nil, err
		}
	}
	paths, err := compiler.GatherPaths([]string{path})
	if err != nil {
		return nil, err
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		return nil, err
	}
	return tabletext.NewModel(u), nil
}

// hostileGenerate rebuilds every verdict from the engine, using the supplied
// metadata only for each case's identity.
func hostileGenerate(root, tmp string, meta []hostileCase) ([]hostileCase, error) {
	models := map[string]*tabletext.Model{}
	out := make([]hostileCase, 0, len(meta))
	for _, c := range meta {
		payload, err := hostilePayload(c.Input)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		m, ok := models[c.Schema]
		if !ok {
			m, err = hostileLoadModel(root, tmp, c.Schema)
			if err != nil {
				return nil, fmt.Errorf("%s: load schema %s: %w", c.Name, c.Schema, err)
			}
			models[c.Schema] = m
		}
		verdict, reason, digest, err := hostileEngineVerdict(m, c, payload)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", c.Name, err)
		}
		c.Verdict, c.Reason, c.Digest = verdict, reason, digest
		out = append(out, c)
	}
	return out, nil
}

// hostileVerify regenerates the manifest from the engine and fails on any
// difference from the committed bytes. This is the drift gate.
func hostileVerify(root, tmp, manifest string) error {
	meta, err := hostileParse(manifest)
	if err != nil {
		return err
	}
	got, err := hostileGenerate(root, tmp, meta)
	if err != nil {
		return err
	}
	want := hostileFormat(got)
	if want == manifest {
		return nil
	}
	for _, d := range hostileDiff(manifest, want) {
		fmt.Fprintln(os.Stderr, d)
	}
	return errors.New("hostile corpus drift: the committed manifest differs from the engine's verdicts (regenerate with make schema-shared-hostile-corpus-generate)")
}

func hostileDiff(have, want string) []string {
	h, w := strings.Split(have, "\n"), strings.Split(want, "\n")
	var out []string
	for i := 0; i < len(h) || i < len(w); i++ {
		var a, b string
		if i < len(h) {
			a = h[i]
		}
		if i < len(w) {
			b = w[i]
		}
		if a != b {
			out = append(out, fmt.Sprintf("manifest line %d:\n  committed: %s\n  engine:    %s", i+1, a, b))
		}
	}
	return out
}

func hostileEngineVerdict(m *tabletext.Model, c hostileCase, payload []byte) (verdict, reason, digest string, err error) {
	st := m.Lookup(c.Table)
	if st == nil {
		return "", "", "", fmt.Errorf("table %q is not in %s", c.Table, c.Schema)
	}
	fail := func(r string) (string, string, string, error) {
		return hostileRefuse, r, hostileNoReason, nil
	}
	accept := func(inst *tabletext.Instance) (string, string, string, error) {
		text, err := m.Write(inst)
		if err != nil {
			return "", "", "", fmt.Errorf("digest: %w", err)
		}
		sum := sha256.Sum256(text)
		return hostileAccept, hostileNoReason, hex.EncodeToString(sum[:]), nil
	}
	switch c.Carrier {
	case "wire":
		inst := m.New(st)
		var r tabletext.Report
		ok, err := tablewire.Decode(m, inst, payload, &r)
		if err != nil {
			return fail(hostileRefusalReason(err))
		}
		if !ok {
			return fail(hostileReasonDamaged)
		}
		return accept(inst)
	case "text":
		inst := m.New(st)
		var r tabletext.Report
		if !m.Read(inst, payload, &r) {
			return fail(hostileReasonDamaged)
		}
		return accept(inst)
	case "announce":
		v := &tablewire.Vocabulary{}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err != nil {
			return fail(hostileRefusalReason(err))
		}
		if r.Malformed {
			return fail(hostileReasonDamaged)
		}
		return hostileAccept, hostileNoReason, hostileNoReason, nil
	case "announce_second":
		v := &tablewire.Vocabulary{}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err != nil {
			return "", "", "", fmt.Errorf("first announcement: %w", err)
		}
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("second announcement was accepted")
		} else {
			return fail(hostileRefusalReason(err))
		}
	case "announce_bytes":
		v := &tablewire.Vocabulary{MaxBytes: 8}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("announcement over the byte bound was accepted")
		} else {
			return fail(hostileRefusalReason(err))
		}
	case "announce_entries":
		v := &tablewire.Vocabulary{MaxEntries: 1}
		var r tabletext.Report
		if err := v.AnnounceRead(payload, &r); err == nil {
			return "", "", "", errors.New("announcement over the entry bound was accepted")
		} else {
			return fail(hostileRefusalReason(err))
		}
	case "message", "message_novocab":
		var v *tablewire.Vocabulary
		if c.Carrier == "message" {
			v = &tablewire.Vocabulary{}
			var ar tabletext.Report
			if err := v.AnnounceRead(tablewire.Announce(m.Unit), &ar); err != nil {
				return "", "", "", fmt.Errorf("announce: %w", err)
			}
		}
		inst := m.New(st)
		var r tabletext.Report
		n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{inst}, payload, v, &r)
		if err != nil {
			return fail(hostileRefusalReason(err))
		}
		if !ok || n != 1 {
			return fail(hostileReasonDamaged)
		}
		return accept(inst)
	default:
		return "", "", "", fmt.Errorf("unknown carrier %q", c.Carrier)
	}
}

func hostileRefusalReason(err error) string {
	var mr *tablewire.MessageRefusal
	if errors.As(err, &mr) {
		return mr.Reason.String()
	}
	var fr *tablewire.FormRefusal
	if errors.As(err, &fr) {
		return hostileReasonNewerForm
	}
	var cr *tablewire.CountRefusal
	if errors.As(err, &cr) {
		return hostileReasonCountRefusal
	}
	return hostileReasonEngineError
}

// TestSchemaSharedHostileCorpus is the drift gate.
func TestSchemaSharedHostileCorpus(t *testing.T) {
	t.Parallel()
	if err := hostileVerify(repoRoot(t), t.TempDir(), hostileManifest); err != nil {
		t.Fatal(err)
	}
}

// TestSchemaSharedHostileCorpusReasons proves the corpus names every refusal
// reason the engine can produce and no reason it cannot, and that every accept
// carries a digest and every refuse names its reason.
func TestSchemaSharedHostileCorpusReasons(t *testing.T) {
	t.Parallel()
	cases, err := hostileParse(hostileManifest)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, c := range cases {
		if c.Verdict == hostileAccept {
			if c.Reason != hostileNoReason {
				t.Errorf("%s: accept carries reason %q", c.Name, c.Reason)
			}
			if c.Digest == hostileNoReason {
				t.Errorf("%s: accept carries no decoded-value digest", c.Name)
			}
			continue
		}
		if c.Verdict != hostileRefuse {
			t.Errorf("%s: verdict %q is neither accept nor refuse", c.Name, c.Verdict)
		}
		if c.Reason == hostileNoReason {
			t.Errorf("%s: refuse carries no reason", c.Name)
		}
		if c.Digest != hostileNoReason {
			t.Errorf("%s: refuse carries a decoded-value digest", c.Name)
		}
		have[c.Reason] = true
	}
	want := map[string]bool{}
	for _, r := range hostileEngineReasons() {
		want[r] = true
		if !have[r] {
			t.Errorf("engine reason %q has no input in the corpus (STEP 2: prove none is missing)", r)
		}
	}
	for r := range have {
		if !want[r] {
			t.Errorf("corpus names reason %q, which is not an engine reason", r)
		}
	}
}

// TestSchemaSharedHostileCorpusNegativeControl plants a changed verdict in the
// committed manifest (in memory), requires the same drift gate to go red, and
// requires the unplanted manifest to be green. A plant that stops being caught
// is the gate going blind.
func TestSchemaSharedHostileCorpusNegativeControl(t *testing.T) {
	t.Parallel()
	root, tmp := repoRoot(t), t.TempDir()
	if err := hostileVerify(root, tmp, hostileManifest); err != nil {
		t.Fatalf("unplanted corpus must be green: %v", err)
	}
	lines := strings.Split(hostileManifest, "\n")
	planted := false
	for i, line := range lines {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if f[6] == hostileAccept {
			f[6], f[7], f[8] = hostileRefuse, hostileReasonDamaged, hostileNoReason
		} else {
			f[6], f[7], f[8] = hostileAccept, hostileNoReason, strings.Repeat("0", 64)
		}
		lines[i] = strings.Join(f, "\t")
		planted = true
		break
	}
	if !planted {
		t.Fatal("no manifest row to plant")
	}
	if err := hostileVerify(root, tmp, strings.Join(lines, "\n")); err == nil {
		t.Fatal("planted bad verdict was NOT caught: the drift gate has no blade")
	}
}

// TestSchemaSharedHostileCorpusSources requires every gathered input to name a
// source that still exists in the checkout, so provenance cannot rot into a
// claim the repository does not hold.
func TestSchemaSharedHostileCorpusSources(t *testing.T) {
	t.Parallel()
	cases, err := hostileParse(hostileManifest)
	if err != nil {
		t.Fatal(err)
	}
	root := repoRoot(t)
	for _, c := range cases {
		for _, prefix := range []string{
			"internal/tabletext/testdata/fuzz/FuzzReadText/",
			"testdata/conformance/tables/json-hostile/",
		} {
			if i := strings.Index(c.Provenance, prefix); i >= 0 {
				rest := strings.Fields(c.Provenance[i+len(prefix):])[0]
				p := filepath.Join(root, filepath.FromSlash(prefix+rest))
				if _, err := os.Stat(p); err != nil {
					t.Errorf("%s: source %s does not exist: %v", c.Name, p, err)
				}
			}
		}
	}
}

// TestSchemaSharedHostileCorpusGenerate is the generator: it rebuilds the
// manifest from the engine. Without HOSTILE_CORPUS_UPDATE it is the same drift
// check; with it, it rewrites the committed manifest block in this file.
func TestSchemaSharedHostileCorpusGenerate(t *testing.T) {
	t.Parallel()
	root, tmp := repoRoot(t), t.TempDir()
	meta, err := hostileParse(hostileManifest)
	if err != nil {
		t.Fatal(err)
	}
	got, err := hostileGenerate(root, tmp, meta)
	if err != nil {
		t.Fatal(err)
	}
	want := hostileFormat(got)
	if os.Getenv("HOSTILE_CORPUS_UPDATE") == "1" {
		if err := hostileRewriteManifest(want); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote the committed hostile corpus manifest from the engine")
		return
	}
	if want != hostileManifest {
		for _, d := range hostileDiff(hostileManifest, want) {
			t.Error(d)
		}
		t.Fatal("the committed manifest is stale; run make schema-shared-hostile-corpus-generate")
	}
}

// hostileRewriteManifest replaces the manifest between the two marker lines of
// this test file. The markers keep the source rewrite honest and narrow.
func hostileRewriteManifest(manifest string) error {
	const path = "schema_shared_hostile_corpus_test.go"
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	const begin = "// HOSTILE-MANIFEST-BEGIN\n"
	const end = "// HOSTILE-MANIFEST-END"
	text := string(data)
	i := strings.Index(text, begin)
	j := strings.Index(text, end)
	if i < 0 || j < i {
		return errors.New("manifest markers not found")
	}
	block := begin + "const hostileManifest = `" + manifest + "`\n" + end
	out := text[:i] + block + text[j+len(end):]
	return os.WriteFile(path, []byte(out), 0o644)
}
