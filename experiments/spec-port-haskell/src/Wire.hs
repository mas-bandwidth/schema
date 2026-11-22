{-# LANGUAGE LambdaCase #-}

-- | Form 1, the variable form: the one read and the canonical write.
-- docs/SPEC-TABLES.md §3, §3.1, §3.2, §4, §6.5, §22.1–§22.5.
module Wire
  ( Node (..)
  , Doc (..)
  , load
  , save
  , checkValue
  , sortMapEntries
  , cmpKey
  , overTotal
  ) where

import Control.Monad (forM, forM_, unless, when, zipWithM)
import Control.Monad.State.Strict
import Data.Bits (shiftL, shiftR, testBit, xor, (.&.), (.|.))
import qualified Data.ByteString as B
import qualified Data.ByteString.Builder as BB
import qualified Data.ByteString.Lazy as BL
import qualified Data.IntMap.Strict as IM
import qualified Data.IntSet as IS
import Data.List (sortBy)
import qualified Data.Map.Strict as M
import Data.Maybe (fromMaybe, isJust)
import qualified Data.Set as S
import Data.Word (Word32, Word64, Word8)
import FloatBits
import Schema
import Utf8 (validText)

data Node = NRec String Val | NBlob Bool B.ByteString
  deriving (Eq, Show)

-- | A loaded or built value: the root and its pointer graph.
data Doc = Doc {docRoot :: Val, docNodes :: IM.IntMap Node}
  deriving (Show)

-- ======================================================================
-- the generic framing tree (phase one of the read)

data GF = GF {gfId :: Word64, gfKind :: Int, gfPay :: GP}

data GP
  = GPFix B.ByteString
  | GPRef (Maybe Word64)
  | GPIdx Integer
  | GPText B.ByteString
  | GPOpaque B.ByteString
  | GPBody [GF]
  | GPArr Int [GP]
  | GPKeyed Int [(Word64, GP)]
  | GPUnion (Maybe (Word64, Int, GP))
  | GPNodes [(Word64, NB)]

data NB = NBRaw B.ByteString | NBFields [GF]

data FS = FS {fsIds :: IM.IntMap Word64, fsCount :: Int, fsMax :: Int}

type FP a = StateT FS (Either String) a

damage :: String -> FP a
damage why = lift (Left ("wire_damaged: " ++ why))

-- | Canonical unsigned LEB128, at most ten bytes, 64-bit capability.
leb :: B.ByteString -> FP (Integer, B.ByteString)
leb = go 0 0
  where
    go :: Int -> Integer -> B.ByteString -> FP (Integer, B.ByteString)
    go i acc s = case B.uncons s of
      Nothing -> damage "LEB128 runs past its body"
      Just (b, r) -> do
        when (i == 9 && b > 1) (damage "LEB128 past 64 bits")
        let acc' = acc .|. (toInteger (b .&. 0x7F) `shiftL` (7 * i))
        if testBit b 7
          then if i >= 9 then damage "LEB128 past ten bytes" else go (i + 1) acc' r
          else do
            when (i > 0 && b == 0) (damage "non-minimal LEB128")
            pure (acc', r)

-- | A reference: 0 names nothing; k resolves to entry k, held to first-use order.
ref :: B.ByteString -> FP (Maybe Word64, B.ByteString)
ref s = do
  (k, r) <- leb s
  if k == 0
    then pure (Nothing, r)
    else do
      st <- get
      when (k > toInteger (fsCount st)) (damage "reference past the id table")
      let ki = fromInteger k
      when (ki > fsMax st + 1) (damage "id table not in first-use order")
      when (ki == fsMax st + 1) (put st {fsMax = ki})
      pure (Just (fsIds st IM.! ki), r)

takeN :: Integer -> B.ByteString -> FP (B.ByteString, B.ByteString)
takeN n s
  | n > toInteger (B.length s) = damage "length runs past its body"
  | otherwise = let k = fromInteger n in pure (B.take k s, B.drop k s)

fixedWidth :: Int -> Maybe Int
fixedWidth k
  | k == 1 || k == 2 || k == 6 = Just 1
  | k == 3 || k == 7 = Just 2
  | k == 4 || k == 8 || k == 10 = Just 4
  | k == 5 || k == 9 || k == 11 || k == 36 = Just 8
  | k == 18 || k == 19 = Just 16
  | k >= 20 && k <= 29 = Just ([1, 2, 4, 8, 16] !! ((k - 20) `mod` 5))
  | otherwise = Nothing

arrayElemKinds, keyedElemKinds :: [Int]
arrayElemKinds = [1 .. 13] ++ [15, 17] ++ [18 .. 30]
keyedElemKinds = [1 .. 11] ++ [13] ++ [18 .. 30]

maxDepth :: Int
maxDepth = 128

reservedIds :: [Word64]
reservedIds = [0xFFFFFFFFFFFFFFFE, 0xFFFFFFFFFFFFFFFD]

fixedScalar :: Int -> B.ByteString -> FP GP
fixedScalar k bs = do
  when (k == 1 && B.head bs > 1) (damage "a bool byte other than 0 or 1")
  when (k == 10 && not (f32IsFinite (le32 bs))) (lift (Left "non_finite_float"))
  when (k == 11 && not (f64IsFinite (le64 bs))) (lift (Left "non_finite_float"))
  pure (GPFix bs)

-- | A body: fields until the zero reference, which must end the input exactly.
body :: Bool -> Int -> B.ByteString -> FP [GF]
body isRoot d s0 = do
  when (d > maxDepth) (damage "nesting past 128")
  go s0 S.empty []
  where
    go s seen acc = do
      (mr, r) <- ref s
      case mr of
        Nothing -> do
          unless (B.null r) (damage "bytes after a body's terminator")
          pure (reverse acc)
        Just fid -> do
          when (S.member fid seen) (damage "the same field id twice in one body")
          when (fid `elem` reservedIds) (damage "a reserved id out of place")
          case B.uncons r of
            Nothing -> damage "a field header cut short"
            Just (kb, r2) -> do
              let k = fromIntegral kb
              if fid == nodeTableId
                then do
                  unless isRoot (damage "a node table in a nested body")
                  unless (k == 12) (damage "a node table under another kind")
                  (l, r3) <- leb r2
                  (pl, r4) <- takeN l r3
                  nodes <- nodeTable pl
                  -- the node table is the root body's last field
                  (mt, r5) <- ref r4
                  when (isJust mt) (lift (Left "refused: node table not the root body's last field"))
                  unless (B.null r5) (damage "bytes after a body's terminator")
                  pure (reverse (GF fid k (GPNodes nodes) : acc))
                else do
                  (p, r3) <- payload d k r2
                  go r3 (S.insert fid seen) (GF fid k p : acc)

nodeTable :: B.ByteString -> FP [(Word64, NB)]
nodeTable s = do
  (n, r) <- leb s
  when (n == 0) (lift (Left "refused: an empty node table"))
  recs <- scan r []
  when (toInteger (length recs) /= n) (damage "node_count disagrees with the records")
  pure recs
  where
    scan r acc
      | B.null r = pure (reverse acc)
      | otherwise = do
          (mt, r2) <- ref r
          tid <- maybe (damage "a node record's type id is 0") pure mt
          (l, r3) <- leb r2
          (b, r4) <- takeN l r3
          -- a record's body is framed here, in its place, so its references
          -- take their turn in first-use order; a blob's bytes are its own
          nb <-
            if isBlobId tid
              then do
                when (tid == blobId True && not (validText b)) (damage "ill-formed text blob")
                pure (NBRaw b)
              else NBFields <$> body False 1 b
          scan r4 ((tid, nb) : acc)

-- | A field payload of kind k at nesting depth d (the body holding it).
payload :: Int -> Int -> B.ByteString -> FP (GP, B.ByteString)
payload d k s = case k of
  _ | Just w <- fixedWidth k -> do
        (bs, r) <- takeN (toInteger w) s
        g <- fixedScalar k bs
        pure (g, r)
  12 -> do
    (l, r) <- leb s
    (bs, r2) <- takeN l r
    unless (validText bs) (damage "ill-formed text")
    pure (GPText bs, r2)
  13 -> do
    (l, r) <- leb s
    (bs, r2) <- takeN l r
    fs <- body False (d + 1) bs
    pure (GPBody fs, r2)
  14 -> do
    (l, r) <- leb s
    (bs, r2) <- takeN l r
    g <- arrayBody (d + 1) bs
    pure (g, r2)
  15 -> do
    (g, r) <- unionPayload d s
    pure (g, r)
  16 -> do
    (l, r) <- leb s
    (bs, r2) <- takeN l r
    g <- keyedBody (d + 1) bs
    pure (g, r2)
  17 -> do
    (i, r) <- leb s
    pure (GPIdx i, r)
  30 -> do
    (m, r) <- ref s
    pure (GPRef m, r)
  31 -> do
    (l, r) <- leb s
    (bs, r2) <- takeN l r
    pure (GPOpaque bs, r2)
  32 -> do
    (l, r) <- leb s
    when (l /= 0) (damage "kind 32 with a nonzero length")
    pure (GPOpaque B.empty, r)
  _ -> damage ("a kind no reader can skip: " ++ show k)

-- | A union payload in its place: arm reference, then kind, L, arm payload.
unionPayload :: Int -> B.ByteString -> FP (GP, B.ByteString)
unionPayload d s = do
  (ma, r) <- ref s
  case ma of
    Nothing -> pure (GPUnion Nothing, r)
    Just aid -> case B.uncons r of
      Nothing -> damage "an arm header cut short"
      Just (kb, r2) -> do
        let k = fromIntegral kb
        (l, r3) <- leb r2
        (bs, r4) <- takeN l r3
        p <- armPayload d k bs
        pure (GPUnion (Just (aid, k, p)), r4)

-- | The arm payload is the arm's field type under the arm's L.
-- SPEC-GAP 8: a structural arm's body is one level; the union adds none.
armPayload :: Int -> Int -> B.ByteString -> FP GP
armPayload d k bs = case k of
  _ | Just w <- fixedWidth k -> do
        unless (B.length bs == w) (damage "an arm whose L does not frame its kind")
        fixedScalar k bs
  12 -> do
    unless (validText bs) (damage "ill-formed text")
    pure (GPText bs)
  13 -> GPBody <$> body False (d + 1) bs
  14 -> arrayBody (d + 1) bs
  16 -> keyedBody (d + 1) bs
  15 -> do
    (g, r) <- unionPayload d bs
    unless (B.null r) (damage "bytes after a nested union inside its arm")
    pure g
  17 -> do
    (i, r) <- leb bs
    unless (B.null r) (damage "an arm whose L does not frame its kind")
    pure (GPIdx i)
  30 -> do
    (m, r) <- ref bs
    unless (B.null r) (damage "an arm whose L does not frame its kind")
    pure (GPRef m)
  31 -> pure (GPOpaque bs)
  32 -> do
    unless (B.null bs) (damage "kind 32 with a nonzero length")
    pure (GPOpaque B.empty)
  _ -> damage ("a kind no reader can skip: " ++ show k)

arrayBody :: Int -> B.ByteString -> FP GP
arrayBody d bs = do
  when (d > maxDepth) (damage "nesting past 128")
  when (B.length bs < 2) (damage "an array body too short for its header")
  let ek = fromIntegral (B.head bs)
  unless (ek `elem` arrayElemKinds) (damage "an element kind off the closed list")
  (n, r) <- leb (B.drop 1 bs)
  els <- case fixedWidth ek of
    Just w -> do
      when (n * toInteger w /= toInteger (B.length r)) (damage "an array count its body cannot cover exactly")
      forM [0 .. fromInteger n - 1] $ \i -> fixedScalar ek (B.take w (B.drop (i * w) r))
    Nothing -> do
      when (n > toInteger (B.length r)) (damage "a count past what its bytes can hold")
      elems ek n r
  pure (GPArr ek els)
  where
    elems ek n r = go n r []
      where
        go 0 rest acc = do
          unless (B.null rest) (damage "bytes left inside an array after its elements")
          pure (reverse acc)
        go i rest acc = do
          (g, rest') <- element ek rest
          go (i - 1) rest' (g : acc)
    element ek r = case ek of
      12 -> payload d 12 r
      13 -> do
        (l, r2) <- leb r
        (b, r3) <- takeN l r2
        fs <- body False (d + 1) b
        pure (GPBody fs, r3)
      15 -> unionPayload d r
      17 -> do
        (i, r2) <- leb r
        pure (GPIdx i, r2)
      30 -> do
        (m, r2) <- ref r
        pure (GPRef m, r2)
      _ -> damage "an element kind off the closed list"

-- SPEC-GAP 6: the count header is read inside the field's own L, as for kind 14.
keyedBody :: Int -> B.ByteString -> FP GP
keyedBody d bs = do
  when (d > maxDepth) (damage "nesting past 128")
  when (B.length bs < 2) (damage "a keyed body too short for its header")
  let ek = fromIntegral (B.head bs)
  unless (ek `elem` keyedElemKinds) (damage "an element kind off the closed list")
  (n, r) <- leb (B.drop 1 bs)
  when (n > toInteger (B.length r)) (damage "a count past what its bytes can hold")
  go ek n r S.empty []
  where
    go ek 0 rest _ acc = do
      unless (B.null rest) (damage "bytes left inside a keyed body")
      pure (GPKeyed ek (reverse acc))
    go ek i rest seen acc = do
      (mk, r) <- ref rest
      key <- maybe (damage "a keyed slot's key is 0") pure mk
      when (S.member key seen) (damage "the same keyed slot twice")
      (l, r2) <- leb r
      (eb, r3) <- takeN l r2
      g <- case ek of
        13 -> GPBody <$> body False (d + 1) eb
        30 -> do
          (m, rr) <- ref eb
          unless (B.null rr) (damage "a keyed element whose L does not frame its kind")
          pure (GPRef m)
        _ -> case fixedWidth ek of
          Just w -> do
            unless (B.length eb == w) (damage "a keyed element whose L does not frame its kind")
            fixedScalar ek eb
          Nothing -> damage "an element kind off the closed list"
      go ek (i - 1) r3 (S.insert key seen) ((key, g) : acc)

-- ======================================================================
-- the read

-- | The one read: the whole value, or a refusal (the reason is diagnostic).
load :: Unit -> Rec -> B.ByteString -> Either String Doc
load u root input0 = do
  when (B.null input0) (Left "wire_damaged: an empty buffer")
  let form = B.head input0
  unless (form == 1) (Left ("unknown_form " ++ show form))
  input <-
    if rIntegrity root
      then do
        when (B.length input0 < 5) (Left "integrity_mismatch")
        let n = B.length input0 - 4
            (pre, tr) = B.splitAt n input0
        unless (crc32c pre == le32 tr) (Left "integrity_mismatch")
        pure pre
      else pure input0
  let len = B.length input
  when (len < 9) (Left "wire_damaged: no room for the id table")
  let count = le64 (B.drop (len - 8) input)
  when (count > 4096) (Left "wire_damaged: id table past 4096 entries")
  let cnt = fromIntegral count :: Int
  when (8 * cnt + 8 > len - 1) (Left "wire_damaged: id table runs past the front of the file")
  let entriesStart = len - 8 - 8 * cnt
      ents = [le64 (B.drop (entriesStart + 8 * i) input) | i <- [0 .. cnt - 1]]
  when (S.size (S.fromList ents) /= cnt) (Left "wire_damaged: one id twice in the table")
  let rootBytes = B.take (entriesStart - 1) (B.drop 1 input)
      st0 = FS (IM.fromList (zip [1 ..] ents)) cnt 0
  (fields, st) <- runStateT (body True 0 rootBytes) st0
  unless (fsMax st == cnt) (Left "refused: an id-table entry no reference names")
  decodeDoc u root fields

data RS = RS
  { rsNodes :: IM.IntMap (Word64, NB) -- wire index -> record
  , rsDecoded :: IM.IntMap Node
  }

type TR a = StateT RS (Either String) a

refuse :: String -> TR a
refuse = lift . Left

decodeDoc :: Unit -> Rec -> [GF] -> Either String Doc
decodeDoc u root fields = do
  let nodeRecs = concat [ns | GF _ _ (GPNodes ns) <- fields]
      idx = IM.fromList (zip [2 ..] nodeRecs)
      plain = [f | f@(GF _ _ p) <- fields, not (isNodes p)]
  -- every index anywhere on the wire, named or not
  allIdx <- collectIdx u nodeRecs plain
  let maxIdx = length nodeRecs + 1
  forM_ allIdx $ \i -> when (i > toInteger maxIdx) (Left "wire_damaged: a node index past the node table")
  let counts = IM.fromListWith (+) [(fromInteger i, 1 :: Int) | i <- allIdx, i >= 2]
  forM_ (IM.toList idx) $ \(i, (tid, _)) -> do
    unless (IM.member i counts) (Left "wire_damaged: a node record no pointer reaches")
    when (isBlobId tid && IM.findWithDefault 0 i counts > 1) (Left "blob_shared")
  when (any (== 1) allIdx) (Left "wire_damaged: a pointer names the root")
  (rootVal, rs) <- runStateT (decRec u root plain) (RS idx IM.empty)
  -- decode every record this reader can name, reached or not
  rs' <- execStateT (mapM_ (decodeNode u) (IM.keys idx)) rs
  let doc = Doc rootVal (rsDecoded rs')
  acyclic u root doc
  pure doc
  where
    isNodes (GPNodes _) = True
    isNodes _ = False

isBlobId :: Word64 -> Bool
isBlobId t = t == blobId True || t == blobId False

-- | Every node index the wire carries, in every body and every record.
-- SPEC-GAP 7: a record is "reached" when any kind-17 index anywhere names it.
collectIdx :: Unit -> [(Word64, NB)] -> [GF] -> Either String [Integer]
collectIdx _ recs root =
  pure (concatMap gfIdx root ++ concat [concatMap gfIdx fs | (_, NBFields fs) <- recs])

gfIdx :: GF -> [Integer]
gfIdx (GF _ _ p) = gpIdx p

gpIdx :: GP -> [Integer]
gpIdx = \case
  GPIdx i -> [i]
  GPBody fs -> concatMap gfIdx fs
  GPArr _ es -> concatMap gpIdx es
  GPKeyed _ es -> concatMap (gpIdx . snd) es
  GPUnion (Just (_, _, p)) -> gpIdx p
  _ -> []

decodeNode :: Unit -> Int -> TR ()
decodeNode u i = do
  rs <- get
  unless (IM.member i (rsDecoded rs)) $ do
    let (tid, _) = rsNodes rs IM.! i
    if isBlobId tid
      then do
        _ <- nodeAs u (Left (tid == blobId True)) i
        pure ()
      else case M.lookup tid (uRecById u) of
        Just n -> do
          _ <- nodeAs u (Right n) i
          pure ()
        Nothing -> pure ()

-- | Resolve a node index for a slot. Left blob kind or Right table name.
nodeAs :: Unit -> Either Bool String -> Int -> TR Int
nodeAs u want i = do
  rs <- get
  case IM.lookup i (rsNodes rs) of
    Nothing -> refuse "wire_damaged: a node index past the node table"
    Just (tid, bytes) -> case want of
      Left isStr -> do
        unless (tid == blobId isStr) (refuse "wire_damaged: a wrong-type pointer target")
        raw <- case bytes of
          NBRaw x -> pure x
          NBFields _ -> refuse "wire_damaged: a wrong-type pointer target"
        modify (\s -> s {rsDecoded = IM.insert i (NBlob isStr raw) (rsDecoded s)})
        pure i
      Right n -> do
        let r = lookupRec u n
        if tid == rId r
          then do
            case IM.lookup i (rsDecoded rs) of
              Just _ -> pure i
              Nothing -> do
                -- mark as decoding, then decode its body
                modify (\s -> s {rsDecoded = IM.insert i (NRec n VUnit) (rsDecoded s)})
                fs <- case bytes of
                  NBFields x -> pure x
                  NBRaw _ -> refuse "wire_damaged: a record framed as a blob"
                v <- decRec u r fs
                modify (\s -> s {rsDecoded = IM.insert i (NRec n v) (rsDecoded s)})
                pure i
          else
            if isBlobId tid || M.member tid (uRecById u)
              then refuse "wire_damaged: a wrong-type pointer target"
              else pure 0 -- a type this reader does not declare: the pointer reads null


decRec :: Unit -> Rec -> [GF] -> TR Val
decRec u r fs = do
  let byId = M.fromList [(gfId g, g) | g <- fs]
      flds = rFields r
  vals <- forM flds $ \f -> case M.lookup (fId f) byId of
    Nothing -> pure Nothing
    Just g -> Just <$> decField u r f (gfKind g) (gfPay g)
  let vs = zipWith (\f mv -> fromMaybe (fieldDefault u f) mv) flds vals
      guardOn f = all (\(gi, want) -> vs !! gi == VBool want) (fGuards f)
  forM_ (zip flds vals) $ \(f, mv) ->
    when (fRequired f && not (isJust mv) && guardOn f) (refuse ("missing_required: " ++ fName f))
  pure (VRec vs)

kindAccepts :: Int -> Int -> Bool
kindAccepts want got
  | want == got = True
  | otherwise = ladder [2, 3, 4, 5, 18] || ladder [6, 7, 8, 9, 19] || (want == 11 && got == 10)
  where
    ladder l = case (lookup want (zip l [0 :: Int ..]), lookup got (zip l [0 ..])) of
      (Just a, Just b) -> b < a
      _ -> False

decField :: Unit -> Rec -> Field -> Int -> GP -> TR Val
decField u r f k p = do
  v <- decTy u (fTy f) k p
  when (overTotal f v) (refuse "length_over_bound: past the text list's total")
  case (fTy f, v) of
    (TEnum e, VEnum o) | o > 0 -> do
      let (_, _, dep) = eVariants (uEnums u M.! e) !! (o - 1)
      pure (if dep then fieldDefault u f else v)
    _ -> pure v
  where
    _ = r

decTy :: Unit -> Ty -> Int -> GP -> TR Val
decTy u ty k p = do
  let want = kindOf u ty
  unless (kindAccepts want k) (refuse "kind_mismatch")
  decAs u ty k p

-- | Decode a payload already known to be of an acceptable kind.
decAs :: Unit -> Ty -> Int -> GP -> TR Val
decAs u ty k p = case (ty, p) of
  (TOpt t, _) -> VOpt . Just <$> decAs u t k p
  (TBool, GPFix bs) -> pure (VBool (B.head bs == 1))
  (TInt _ _ _, GPFix bs) -> do
    let v = intFromWire k bs
    inRange ty v
  (TBits _, GPFix bs) -> inRange ty (intFromWire k bs)
  (TFixedPt {}, GPFix bs) -> inRange ty (intFromWire k bs)
  (TTime, GPFix bs) -> pure (VI (signed 8 (leN bs)))
  (TFlags _, GPFix bs) -> pure (VFlags (fromInteger (leN bs)))
  (TFloat32 c, GPFix bs) -> do
    let w = le32 bs
    case c of
      Nothing -> pure (VF32 w)
      Just (lo, hi, _) -> do
        let w' = if f32IsNegZero w then 0 else w
            x = f32ToRat w'
        when (x < lo || x > hi) (refuse "out_of_range")
        pure (VF32 w')
  (TFloat64, GPFix bs)
    | k == 10 -> pure (VF64 (fromMaybe 0 (ratToF64 (f32ToRat (le32 bs)))))
    | otherwise -> pure (VF64 (le64 bs))
  (TStr b, GPText bs) -> do
    when (maybe False (\n -> toInteger (B.length bs) > n) b) (refuse "length_over_bound")
    pure (VBytes bs)
  (TBytes n, GPArr ek es) -> do
    unless (ek == 6) (refuse "kind_mismatch")
    when (toInteger (length es) > n) (refuse "length_over_bound")
    pure (VBytes (B.concat [bs | GPFix bs <- es]))
  (TEnum e, GPRef m) -> pure $ case m of
    Nothing -> VEnum 0
    Just vid -> case [i | (i, (_, w, _)) <- zip [1 ..] (eVariants (uEnums u M.! e)), w == vid] of
      (i : _) -> VEnum i
      [] -> VEnum 0 -- unknown: reads None, counted
  (TRecT n, GPBody fs) -> decRec u (lookupRec u n) fs
  (TUnionT un, GPUnion m) -> case m of
    Nothing -> pure (VUnion 0 VUnit)
    Just (aid, ak, ap) -> do
      let arms = uArms (uUnions u M.! un)
      case [(i, a) | (i, a) <- zip [1 ..] arms, aId a == aid] of
        [] -> pure (VUnion 0 VUnit) -- unknown arm: None, counted
        ((i, a) : _) -> case aTy a of
          Nothing -> do
            unless (ak == 32) (refuse "kind_mismatch")
            pure (VUnion i VUnit)
          Just at -> do
            unless (kindAccepts (kindOf u at) ak) (refuse "kind_mismatch")
            VUnion i <$> decAs u at ak ap
  (TPtr n, GPIdx i) -> VPtr <$> ptrTo (Right n) i
  (TBlob s, GPIdx i) -> VPtr <$> ptrTo (Left s) i
  (TArr sh et, GPArr ek es) -> do
    let wantE = elemKindOf u et
    unless (kindAccepts wantE ek) (refuse "kind_mismatch")
    let n = toInteger (length es)
    case sh of
      SFixedN m -> when (n > m) (refuse "count_over_bound")
      SCount a b -> when (n > b || n < a) (refuse "count_over_bound")
      SUnbounded -> pure ()
    vs <- mapM (decElem et ek) es
    pure $ case sh of
      SFixedN m -> VArr (vs ++ replicate (fromInteger m - length vs) (defaultOf u et))
      _ -> VArr vs
  (TKeyed e et, GPKeyed ek slots) -> do
    unless (kindAccepts (elemKindOf u et) ek) (refuse "kind_mismatch")
    let vars = eVariants (uEnums u M.! e)
        base = replicate (length vars) (defaultOf u et)
    placed <- forM slots $ \(key, g) -> case [i | (i, (_, w, _)) <- zip [0 ..] vars, w == key] of
      (i : _) -> Just . (,) i <$> decAs u et ek g
      [] -> pure Nothing -- unknown key: dropped, counted
    let m = M.fromList [x | Just x <- placed]
    pure (VKeyed [M.findWithDefault d i m | (i, d) <- zip [0 ..] base])
  (TMap kt vt, GPArr ek es) -> do
    unless (ek == 13) (refuse "kind_mismatch")
    let er = entryRec kt vt
    ents <- forM es $ \case
      GPBody fs -> do
        rv <- decRec u er fs
        case rv of
          VRec [kv, vv] -> pure (kv, vv)
          _ -> refuse "internal"
      _ -> refuse "kind_mismatch"
    let ks = map fst ents
    unless (and (zipWith (\a b -> cmpKey a b == LT) ks (drop 1 ks))) (refuse "wire_damaged: map keys not ascending")
    pure (VMap ents)
  _ -> refuse "kind_mismatch"
  where
    decElem et ek g
      | isTextListOpt et = case g of
          GPBody fs -> do
            let tr = Rec "" 0 False True [Field "value" mapValueId "value" (case et of TOpt t -> t; t -> t) Nothing [] False Nothing] False False
            rv <- decRec u tr fs
            let v = case rv of VRec [x] -> x; _ -> VUnit
            pure (VOpt (if any ((== mapValueId) . gfId) fs then Just v else Nothing))
          _ -> refuse "kind_mismatch"
      | otherwise = decAs u et ek g
    ptrTo want i
      | i == 0 = pure 0
      | otherwise = nodeAs u want (fromInteger i)

-- | The generated `{ key, value }` entry of a map (SPEC-TABLES.md §2.8).
entryRec :: Ty -> Ty -> Rec
entryRec kt vt =
  Rec
    "entry"
    0
    False
    True
    [ Field "key" mapKeyId "key" kt Nothing [] False Nothing
    , Field "value" mapValueId "value" vt Nothing [] False Nothing
    ]
    False
    False

inRange :: Ty -> Integer -> TR Val
inRange ty v = case intRange ty of
  Just (lo, hi) | v < lo || v > hi -> refuse "out_of_range"
  _ -> pure (VI v)

intFromWire :: Int -> B.ByteString -> Integer
intFromWire k bs =
  let n = leN bs
   in if k `elem` [2, 3, 4, 5, 18] || (k >= 20 && k <= 24) then signed (B.length bs) n else n

signed :: Int -> Integer -> Integer
signed w n = if n >= 2 ^ (8 * w - 1) then n - 2 ^ (8 * w) else n

leN :: B.ByteString -> Integer
leN = B.foldr' (\b acc -> acc * 256 + toInteger b) 0

le32 :: B.ByteString -> Word32
le32 = fromInteger . leN . B.take 4

le64 :: B.ByteString -> Word64
le64 = fromInteger . leN . B.take 8

-- | The stored graph is acyclic (§3.1): linear, three marks, no recursion
-- over pointers.
acyclic :: Unit -> Rec -> Doc -> Either String ()
acyclic u root (Doc rv nodes) = go [(0, edges root rv)] (IM.fromList [(0, 1 :: Int)])
  where
    -- mark 1 = open, 2 = closed
    go [] _ = Right ()
    go ((n, []) : st) marks = go st (IM.insert n 2 marks)
    go ((n, e : es) : st) marks = case IM.lookup e marks of
      Just 1 -> Left "wire_damaged: a cycle"
      Just _ -> go ((n, es) : st) marks
      Nothing -> case IM.lookup e nodes of
        Just (NRec tn v) -> go ((e, edges (lookupRec u tn) v) : (n, es) : st) (IM.insert e 1 marks)
        _ -> go ((n, es) : st) (IM.insert e 2 marks)
    edges r v = filter (/= 0) (recEdges u r v)

-- | Every non-null table pointer slot a value's by-value closure holds,
-- guards ignored (a read stores what it reads).
recEdges :: Unit -> Rec -> Val -> [Int]
recEdges u r (VRec vs) = concat (zipWith (\f v -> tyEdges u (fTy f) v) (rFields r) vs)
recEdges _ _ _ = []

tyEdges :: Unit -> Ty -> Val -> [Int]
tyEdges u ty v = case (ty, v) of
  (TPtr _, VPtr i) -> [i]
  (TRecT n, _) -> recEdges u (lookupRec u n) v
  (TOpt t, VOpt (Just x)) -> tyEdges u t x
  (TArr _ t, VArr xs) -> concatMap (tyEdges u t) xs
  (TKeyed _ t, VKeyed xs) -> concatMap (tyEdges u t) xs
  (TMap _ t, VMap es) -> concatMap (tyEdges u t . snd) es
  (TUnionT un, VUnion i x) | i > 0 -> case aTy (uArms (uUnions u M.! un) !! (i - 1)) of
    Just at -> tyEdges u at x
    Nothing -> []
  _ -> []

-- ======================================================================
-- key order (SPEC-TABLES.md §22.2.1)

cmpKey :: Val -> Val -> Ordering
cmpKey (VI a) (VI b) = compare a b
cmpKey (VBytes a) (VBytes b) = compare a b -- ByteString Ord is memcmp then length
cmpKey a b = compare (show a) (show b)

sortMapEntries :: [(Val, Val)] -> [(Val, Val)]
sortMapEntries = sortBy (\(a, _) (b, _) -> cmpKey a b)

-- ======================================================================
-- the write

data Out = ORaw B.ByteString | ORef Word64 | ONum Integer | OLen [Out]

data WS = WS {wsIndex :: IM.IntMap Int, wsOpen :: IS.IntSet, wsNext :: Int, wsOrder :: [Int]}

type W a = StateT WS (Either String) a

-- | Save: the canonical bytes of a value, or a refusal (`invalid_value`,
-- `data_cycle`, `blob_shared`).
save :: Unit -> Rec -> Doc -> Either String B.ByteString
save u root doc = do
  checkValue u root doc
  ws <- execStateT (numRec u doc root (docRoot doc)) (WS IM.empty IS.empty 2 [])
  let ix = wsIndex ws
      order = reverse (wsOrder ws)
  rootFields <- encFields u doc ix root (docRoot doc)
  recs <- forM order $ \n -> case docNodes doc IM.! n of
    NRec tn v -> do
      let r = lookupRec u tn
      fs <- encFields u doc ix r v
      pure [ORef (rId r), OLen (fs ++ [ONum 0])]
    NBlob s bs -> pure [ORef (blobId s), OLen [ORaw bs]]
  let nodeField =
        if null order
          then []
          else [ORef nodeTableId, ORaw (B.singleton 12), OLen (ONum (toInteger (length order)) : concat recs)]
      outs = ORaw (B.singleton 1) : rootFields ++ nodeField ++ [ONum 0]
      ids = firstUse outs
      ixOf = M.fromList (zip ids [1 :: Integer ..])
      bodyBytes = render ixOf outs
      table = BL.toStrict . BB.toLazyByteString $ mconcat (map BB.word64LE ids) <> BB.word64LE (fromIntegral (length ids))
      file = bodyBytes <> table
  pure $
    if rIntegrity root
      then file <> BL.toStrict (BB.toLazyByteString (BB.word32LE (crc32c file)))
      else file

firstUse :: [Out] -> [Word64]
firstUse outs = reverse (fst (foldl go ([], S.empty) (flatten outs)))
  where
    flatten = concatMap (\case OLen xs -> flatten xs; o -> [o])
    go (acc, seen) (ORef i)
      | S.member i seen = (acc, seen)
      | otherwise = (i : acc, S.insert i seen)
    go st _ = st

render :: M.Map Word64 Integer -> [Out] -> B.ByteString
render ixOf = BL.toStrict . BB.toLazyByteString . mconcat . map one
  where
    one = \case
      ORaw b -> BB.byteString b
      ORef i -> lebB (ixOf M.! i)
      ONum n -> lebB n
      OLen xs -> let b = render ixOf xs in lebB (toInteger (B.length b)) <> BB.byteString b

lebB :: Integer -> BB.Builder
lebB n
  | n < 128 = BB.word8 (fromInteger n)
  | otherwise = BB.word8 (fromInteger (n .&. 0x7F) .|. 0x80) <> lebB (n `shiftR` 7)

-- numbering: depth-first pre-order from the root (§22.3)
numRec :: Unit -> Doc -> Rec -> Val -> W ()
numRec u doc r (VRec vs) =
  forM_ (zip (rFields r) vs) $ \(f, v) ->
    when (guardHolds f vs) (numTy u doc (fTy f) v)
numRec _ _ _ _ = pure ()

guardHolds :: Field -> [Val] -> Bool
guardHolds f vs = all (\(gi, want) -> vs !! gi == VBool want) (fGuards f)

numTy :: Unit -> Doc -> Ty -> Val -> W ()
numTy u doc ty v = case (ty, v) of
  (TPtr _, VPtr n) | n /= 0 -> visit n
  (TBlob _, VPtr n) | n /= 0 -> visit n
  (TRecT rn, _) -> numRec u doc (lookupRec u rn) v
  (TOpt t, VOpt (Just x)) -> numTy u doc t x
  (TArr _ t, VArr xs) -> mapM_ (numTy u doc t) xs
  (TKeyed _ t, VKeyed xs) -> mapM_ (numTy u doc t) xs
  (TMap _ t, VMap es) -> mapM_ (numTy u doc t . snd) (sortMapEntries es)
  (TUnionT un, VUnion i x) | i > 0 -> case aTy (uArms (uUnions u M.! un) !! (i - 1)) of
    Just at -> numTy u doc at x
    Nothing -> pure ()
  _ -> pure ()
  where
    visit n = do
      ws <- get
      case IM.lookup n (wsIndex ws) of
        Just _
          | IS.member n (wsOpen ws) -> lift (Left "data_cycle")
          | otherwise -> case IM.lookup n (docNodes doc) of
              Just (NBlob _ _) -> lift (Left "blob_shared")
              _ -> pure ()
        Nothing -> do
          put ws {wsIndex = IM.insert n (wsNext ws) (wsIndex ws), wsNext = wsNext ws + 1, wsOrder = n : wsOrder ws, wsOpen = IS.insert n (wsOpen ws)}
          case IM.lookup n (docNodes doc) of
            Just (NRec tn nv) -> numRec u doc (lookupRec u tn) nv
            Just (NBlob _ _) -> pure ()
            Nothing -> lift (Left "invalid_value: a pointer to no node")
          modify (\s -> s {wsOpen = IS.delete n (wsOpen s)})

-- | A body's fields (no terminator) by §22.2's elision table.
encFields :: Unit -> Doc -> IM.IntMap Int -> Rec -> Val -> Either String [Out]
encFields u doc ix r (VRec vs) = concat <$> zipWithM one (rFields r) vs
  where
    one f v
      | not (guardHolds f vs) = pure []
      | not (fRequired f) && elided u (fTy f) v (fieldDefault u f) = pure []
      | otherwise = do
          p <- fieldPayload u doc ix (fTy f) v
          pure (ORef (fId f) : ORaw (B.singleton (fromIntegral (kindOf u (fTy f)))) : p)
encFields _ _ _ _ _ = Left "invalid_value: not a record"

-- | Would this value, at this declaration, be elided (§22.2)?
elided :: Unit -> Ty -> Val -> Val -> Bool
elided u ty v d = case ty of
  TOpt _ -> v == VOpt Nothing
  TPtr _ -> v == VPtr 0
  TBlob _ -> v == VPtr 0
  TRecT n -> recElided u (lookupRec u n) v
  TArr sh et -> case v of
    VArr xs -> case sh of
      SFixedN _ | elemKindOf u et == 13 && not (isTextListOpt et) -> False
      SFixedN _ -> all (atDefault et) xs
      SCount a _ -> toInteger (length xs) == a && all (atDefault et) xs
      SUnbounded -> null xs
    _ -> False
  TKeyed _ et -> case v of
    VKeyed xs -> all (atDefault et) xs
    _ -> False
  TUnionT _ -> case v of
    VUnion 0 _ -> True
    _ -> False
  TMap _ _ -> v == VMap []
  TFloat32 (Just _) -> case (v, d) of
    (VF32 a, VF32 b) -> f32ToRat a == f32ToRat b
    _ -> False
  _ -> v == d
  where
    atDefault et x = elided u et x (defaultOf u et)

recElided :: Unit -> Rec -> Val -> Bool
recElided u r (VRec vs) =
  and
    [ not (guardHolds f vs) || (not (fRequired f) && elided u (fTy f) v (fieldDefault u f))
    | (f, v) <- zip (rFields r) vs
    ]
recElided _ _ _ = False

lengthKinds :: [Int]
lengthKinds = [12, 13, 14, 16]

fieldPayload :: Unit -> Doc -> IM.IntMap Int -> Ty -> Val -> Either String [Out]
fieldPayload u doc ix ty v = do
  c <- content u doc ix ty v
  pure (if kindOf u ty `elem` lengthKinds then [OLen c] else c)

-- | The payload without its own L, for the kinds that carry one.
content :: Unit -> Doc -> IM.IntMap Int -> Ty -> Val -> Either String [Out]
content u doc ix ty v = case (ty, v) of
  (TOpt t, VOpt (Just x)) -> content u doc ix t x
  (TBool, VBool b) -> raw [if b then 1 else 0]
  (TInt w _ _, VI n) -> raw (leBytes w n)
  (TBits n, VI x) -> raw (leBytes (bw n) x)
  (TFixedPt _ i f _, VI x) -> raw (leBytes ((i + f) `div` 8) x)
  (TTime, VI x) -> raw (leBytes 8 x)
  (TFlags _, VFlags x) -> raw (leBytes 8 (toInteger x))
  (TFloat32 c, VF32 w) -> raw (leBytes 4 (toInteger (if isJust c && f32IsNegZero w then 0 else w)))
  (TFloat64, VF64 w) -> raw (leBytes 8 (toInteger w))
  (TStr _, VBytes bs) -> pure [ORaw bs]
  (TBytes _, VBytes bs) -> pure [ORaw (B.singleton 6), ONum (toInteger (B.length bs)), ORaw bs]
  (TEnum e, VEnum o) ->
    if o == 0
      then pure [ONum 0]
      else let (_, w, _) = eVariants (uEnums u M.! e) !! (o - 1) in pure [ORef w]
  (TRecT n, _) -> do
    fs <- encFields u doc ix (lookupRec u n) v
    pure (fs ++ [ONum 0])
  (TUnionT un, VUnion i x) ->
    if i == 0
      then pure [ONum 0]
      else do
        let a = uArms (uUnions u M.! un) !! (i - 1)
        case aTy a of
          Nothing -> pure [ORef (aId a), ORaw (B.singleton 32), OLen []]
          Just at -> do
            c <- content u doc ix at x
            pure [ORef (aId a), ORaw (B.singleton (fromIntegral (kindOf u at))), OLen c]
  (TPtr _, VPtr n) -> pure [ONum (toInteger (if n == 0 then 0 else ix IM.! n))]
  (TBlob _, VPtr n) -> pure [ONum (toInteger (if n == 0 then 0 else ix IM.! n))]
  (TArr _ et, VArr xs) -> do
    es <- mapM (arrayElem et) xs
    pure (ORaw (B.singleton (fromIntegral (elemKindOf u et))) : ONum (toInteger (length xs)) : concat es)
  (TKeyed e et, VKeyed xs) -> do
    let vars = eVariants (uEnums u M.! e)
        present = [(w, x) | ((_, w, _), x) <- zip vars xs, not (elided u et x (defaultOf u et))]
    es <- forM present $ \(w, x) -> do
      c <- content u doc ix et x
      pure [ORef w, OLen c]
    pure (ORaw (B.singleton (fromIntegral (elemKindOf u et))) : ONum (toInteger (length present)) : concat es)
  (TMap kt vt, VMap es) -> do
    let er = entryRec kt vt
    bs <- forM (sortMapEntries es) $ \(k, x) -> do
      fs <- encFields u doc ix er (VRec [k, x])
      pure [OLen (fs ++ [ONum 0])]
    pure (ORaw (B.singleton 13) : ONum (toInteger (length es)) : concat bs)
  _ -> Left ("invalid_value: a value of the wrong shape for " ++ show ty)
  where
    raw = pure . (: []) . ORaw . B.pack
    bw n
      | n <= 8 = 1
      | n <= 16 = 2
      | n <= 32 = 4
      | otherwise = 8
    arrayElem et x
      | isTextListOpt et = case x of
          VOpt Nothing -> pure [OLen [ONum 0]]
          VOpt (Just s) -> do
            c <- content u doc ix (case et of TOpt t -> t; t -> t) s
            pure [OLen [ORef mapValueId, ORaw (B.singleton 12), OLen c, ONum 0]]
          _ -> Left "invalid_value"
      | otherwise = do
          c <- content u doc ix et x
          pure (if elemKindOf u et `elem` [12, 13] then [OLen c] else c)

leBytes :: Int -> Integer -> [Word8]
leBytes w n = let m = n `mod` (2 ^ (8 * w)) in [fromInteger ((m `shiftR` (8 * i)) .&. 0xFF) | i <- [0 .. w - 1]]

-- | The write contract's refusals (SPEC.md §5.1; SPEC-TABLES.md §22.2):
-- a value outside its field's domain is refused, never clamped.
checkValue :: Unit -> Rec -> Doc -> Either String ()
checkValue u root doc = do
  chkRec root (docRoot doc)
  forM_ (IM.elems (docNodes doc)) $ \case
    NRec tn v -> chkRec (lookupRec u tn) v
    NBlob s bs -> when (s && not (validText bs)) (bad "text")
  where
    bad what = Left ("invalid_value: " ++ what)
    chkRec r (VRec vs) = zipWithM_' (\f v -> when (overTotal f v) (bad "a text list past its total") >> chk (fTy f) v) (rFields r) vs
    chkRec _ _ = bad "record"
    zipWithM_' g as bs = sequence_ (zipWith g as bs)
    chk ty v = case (ty, v) of
      (TOpt t, VOpt (Just x)) -> chk t x
      (TOpt _, VOpt Nothing) -> pure ()
      (TInt {}, VI n) -> rng ty n
      (TBits _, VI n) -> rng ty n
      (TFixedPt {}, VI n) -> rng ty n
      (TTime, VI n) -> rng ty n
      (TFloat32 c, VF32 w) -> do
        unless (f32IsFinite w) (bad "non-finite float")
        case c of
          Just (lo, hi, _) -> let x = f32ToRat w in when (x < lo || x > hi) (bad "compressed float off its range")
          Nothing -> pure ()
      (TFloat64, VF64 w) -> unless (f64IsFinite w) (bad "non-finite float")
      (TStr b, VBytes bs) -> do
        unless (validText bs) (bad "text")
        when (maybe False (\n -> toInteger (B.length bs) > n) b) (bad "length past its bound")
      (TBytes n, VBytes bs) -> when (toInteger (B.length bs) > n) (bad "length past its bound")
      (TEnum e, VEnum o) -> do
        let vars = eVariants (uEnums u M.! e)
        when (o < 0 || o > length vars) (bad "enum value no variant names")
      (TRecT n, _) -> chkRec (lookupRec u n) v
      (TUnionT un, VUnion i x) -> do
        let arms = uArms (uUnions u M.! un)
        when (i < 0 || i > length arms) (bad "union tag")
        when (i > 0) $ maybe (pure ()) (\at -> chk at x) (aTy (arms !! (i - 1)))
      (TArr sh et, VArr xs) -> do
        let n = toInteger (length xs)
        case sh of
          SFixedN m -> when (n /= m) (bad "fixed array extent")
          SCount a b -> when (n < a || n > b) (bad "count past its bound")
          SUnbounded -> pure ()
        mapM_ (chk et) xs
      (TKeyed _ et, VKeyed xs) -> mapM_ (chk et) xs
      (TMap kt vt, VMap es) -> mapM_ (\(k, x) -> chk kt k >> chk vt x) es
      _ -> pure ()
    rng ty n = case intRange ty of
      Just (lo, hi) | n < lo || n > hi -> bad "integer off its range"
      _ -> pure ()

-- ======================================================================

crc32c :: B.ByteString -> Word32
crc32c = xor 0xFFFFFFFF . B.foldl' step 0xFFFFFFFF
  where
    step c b = iterate bit (c `xor` fromIntegral b) !! 8
    bit c = if testBit c 0 then (c `shiftR` 1) `xor` 0x82F63B78 else c `shiftR` 1

-- | A text list past its declared `total` (SPEC-TABLES.md §2.13): the sum of
-- its elements' byte lengths, an absent element counting zero.
overTotal :: Field -> Val -> Bool
overTotal f v = case (fTotal f, v) of
  (Just n, VArr xs) -> toInteger (sum (map len xs)) > n
  (Just n, VOpt (Just x)) -> overTotal f {fTotal = Just n} x
  _ -> False
  where
    len (VBytes b) = B.length b
    len (VOpt (Just (VBytes b))) = B.length b
    len _ = 0
