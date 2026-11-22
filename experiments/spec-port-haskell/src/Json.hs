{-# LANGUAGE LambdaCase #-}

-- | The text form: one JSON text in, one canonical JSON text out.
-- docs/SPEC-TABLES.md §16.1, §16.2, §16.3, §16.7, §16.10, §16.14.
module Json (fromJson, toJson, parseJ, J (..)) where

import Control.Monad (foldM, forM, forM_, unless, when)
import Control.Monad.State.Strict
import Data.Bits (shiftL, shiftR, testBit, (.&.), (.|.))
import qualified Data.ByteString as B
import qualified Data.ByteString.Builder as BB
import qualified Data.ByteString.Char8 as BC
import qualified Data.ByteString.Lazy as BL
import Data.Char (chr, isDigit, isHexDigit, digitToInt, ord)
import qualified Data.IntMap.Strict as IM
import qualified Data.IntSet as IS
import Data.List (sortBy)
import qualified Data.Map.Strict as M
import Data.Maybe (fromMaybe, isJust)
import Data.Ratio (denominator, numerator, (%))
import qualified Data.Set as S
import Data.Word (Word32, Word64, Word8)
import FloatBits
import Schema
import Utf8
import Wire (Doc (..), Node (..), checkValue, cmpKey, overTotal, sortMapEntries)

-- ======================================================================
-- the JSON layer

data J
  = JNull
  | JBool Bool
  | JNum String -- the token, already held to RFC 8259's number grammar
  | JStr B.ByteString -- decoded, well-formed UTF-8
  | JArr [J]
  | JObj [(B.ByteString, J)]
  deriving (Show)

malformed :: String -> Either String a
malformed why = Left ("malformed: " ++ why)

maxDepth :: Int
maxDepth = 128

-- | The whole text is one value (§16.1); a BOM is skipped, comments and
-- trailing commas are accepted, a repeated key is malformed (§16.2).
parseJ :: B.ByteString -> Either String J
parseJ input0 = do
  let input = if B.take 3 input0 == B.pack [0xEF, 0xBB, 0xBF] then B.drop 3 input0 else input0
  s1 <- ws input
  (v, s2) <- value 0 s1
  s3 <- ws s2
  unless (B.null s3) (malformed "data after the root value")
  pure v

ws :: B.ByteString -> Either String B.ByteString
ws s = case B.uncons s of
  Just (c, r)
    | c == 0x20 || c == 0x09 || c == 0x0A || c == 0x0D -> ws r
    | c == 0x2F -> case B.uncons r of
        Just (0x2F, r2) -> do
          let (body, rest) = B.break (== 0x0A) r2
          commentText body
          ws rest
        Just (0x2A, r2) -> case B.breakSubstring (BC.pack "*/") r2 of
          (body, rest)
            | B.null rest -> malformed "an unclosed block comment"
            | otherwise -> commentText body >> ws (B.drop 2 rest)
        _ -> malformed "a stray slash"
  _ -> Right s
  where
    commentText b = unless (validText b) (malformed "a comment that is not text")

value :: Int -> B.ByteString -> Either String (J, B.ByteString)
value d s = case B.uncons s of
  Nothing -> malformed "the text ends where a value belongs"
  Just (c, r)
    | c == 0x7B -> object (d + 1) r
    | c == 0x5B -> array (d + 1) r
    | c == 0x22 -> do
        (t, r2) <- str r
        pure (JStr t, r2)
    | c == 0x74 && B.isPrefixOf (BC.pack "true") s -> pure (JBool True, B.drop 4 s)
    | c == 0x66 && B.isPrefixOf (BC.pack "false") s -> pure (JBool False, B.drop 5 s)
    | c == 0x6E && B.isPrefixOf (BC.pack "null") s -> pure (JNull, B.drop 4 s)
    | c == 0x2D || (c >= 0x30 && c <= 0x39) -> number s
    | otherwise -> malformed "an unexpected byte"
  where
    _ = d

checkDepth :: Int -> Either String ()
checkDepth d = when (d > maxDepth) (malformed "nesting past 128")

object :: Int -> B.ByteString -> Either String (J, B.ByteString)
object d s0 = do
  checkDepth (d - 1)
  s <- ws s0
  case B.uncons s of
    Just (0x7D, r) -> pure (JObj [], r)
    _ -> go s [] S.empty
  where
    go s acc seen = do
      s1 <- ws s
      case B.uncons s1 of
        Just (0x7D, r) | not (null acc) -> pure (JObj (reverse acc), r) -- trailing comma
        Just (0x22, r) -> do
          (k, r2) <- str r
          when (S.member k seen) (malformed "a repeated key")
          r3 <- ws r2
          r4 <- case B.uncons r3 of
            Just (0x3A, x) -> ws x
            _ -> malformed "a key with no colon"
          (v, r5) <- value d r4
          r6 <- ws r5
          case B.uncons r6 of
            Just (0x2C, r7) -> go r7 ((k, v) : acc) (S.insert k seen)
            Just (0x7D, r7) -> pure (JObj (reverse ((k, v) : acc)), r7)
            _ -> malformed "an object member with no comma or brace after it"
        _ -> malformed "an object key that is not a string"

array :: Int -> B.ByteString -> Either String (J, B.ByteString)
array d s0 = do
  checkDepth (d - 1)
  s <- ws s0
  case B.uncons s of
    Just (0x5D, r) -> pure (JArr [], r)
    _ -> go s []
  where
    go s acc = do
      s1 <- ws s
      case B.uncons s1 of
        Just (0x5D, r) | not (null acc) -> pure (JArr (reverse acc), r)
        _ -> do
          (v, r2) <- value d s1
          r3 <- ws r2
          case B.uncons r3 of
            Just (0x2C, r4) -> go r4 (v : acc)
            Just (0x5D, r4) -> pure (JArr (reverse (v : acc)), r4)
            _ -> malformed "an array element with no comma or bracket after it"

-- | A string body: raw bytes at or above 0x20, the escapes, surrogate pairs.
str :: B.ByteString -> Either String (B.ByteString, B.ByteString)
str s0 = go s0 []
  where
    go s acc = case B.uncons s of
      Nothing -> malformed "an unclosed string"
      Just (0x22, r) -> do
        let out = B.concat (reverse acc)
        unless (wellFormed out) (malformed "string_not_utf8")
        pure (out, r)
      Just (0x5C, r) -> case B.uncons r of
        Just (e, r2)
          | e == 0x22 -> go r2 (B.singleton 0x22 : acc)
          | e == 0x5C -> go r2 (B.singleton 0x5C : acc)
          | e == 0x2F -> go r2 (B.singleton 0x2F : acc)
          | e == 0x62 -> go r2 (B.singleton 0x08 : acc)
          | e == 0x66 -> go r2 (B.singleton 0x0C : acc)
          | e == 0x6E -> go r2 (B.singleton 0x0A : acc)
          | e == 0x72 -> go r2 (B.singleton 0x0D : acc)
          | e == 0x74 -> go r2 (B.singleton 0x09 : acc)
          | e == 0x75 -> do
              (u, r3) <- hex4 r2
              if u >= 0xD800 && u <= 0xDBFF
                then case B.splitAt 2 r3 of
                  (p, r4) | p == BC.pack "\\u" -> do
                    (l, r5) <- hex4 r4
                    unless (l >= 0xDC00 && l <= 0xDFFF) (malformed "string_lone_surrogate")
                    let cp = 0x10000 + ((u - 0xD800) `shiftL` 10) + (l - 0xDC00)
                    go r5 (B.pack (encodeChar cp) : acc)
                  _ -> malformed "string_lone_surrogate"
                else do
                  when (u >= 0xDC00 && u <= 0xDFFF) (malformed "string_lone_surrogate")
                  -- §16.3: no read stores U+0000, placed or skipped
                  when (u == 0) (Left "text_holds_zero")
                  go r3 (B.pack (encodeChar u) : acc)
        _ -> malformed "a backslash that begins no escape"
      Just (c, _)
        | c < 0x20 -> malformed "a raw control byte in a string"
        | otherwise ->
            let (run, rest) = B.span (\x -> x >= 0x20 && x /= 0x22 && x /= 0x5C) s
             in go rest (run : acc)
    hex4 r =
      let h = B.take 4 r
       in if B.length h == 4 && B.all (isHexDigit . toEnum . fromIntegral) h
            then Right (foldl (\a x -> a * 16 + digitToInt (toEnum (fromIntegral x))) 0 (B.unpack h), B.drop 4 r)
            else malformed "a \\u escape without four hex digits"

-- | RFC 8259's number grammar, the token kept as written.
number :: B.ByteString -> Either String (J, B.ByteString)
number s = do
  let (neg, s1) = case B.uncons s of
        Just (0x2D, r) -> (True, r)
        _ -> (False, s)
  (ip, s2) <- case B.uncons s1 of
    Just (0x30, r) -> Right (B.singleton 0x30, r)
    Just (c, _) | c >= 0x31 && c <= 0x39 -> Right (B.span isD s1)
    _ -> malformed "a number with no digits"
  (fp, s3) <- case B.uncons s2 of
    Just (0x2E, r) -> do
      let (ds, r2) = B.span isD r
      when (B.null ds) (malformed "a fraction with no digits")
      Right (B.cons 0x2E ds, r2)
    _ -> Right (B.empty, s2)
  (ep, s4) <- case B.uncons s3 of
    Just (e, r) | e == 0x65 || e == 0x45 -> do
      let (sg, r2) = case B.uncons r of
            Just (x, y) | x == 0x2B || x == 0x2D -> (B.singleton x, y)
            _ -> (B.empty, r)
          (ds, r3) = B.span isD r2
      when (B.null ds) (malformed "an exponent with no digits")
      Right (B.concat [B.singleton 0x65, sg, ds], r3)
    _ -> Right (B.empty, s3)
  pure (JNum (BC.unpack (B.concat [if neg then BC.pack "-" else B.empty, ip, fp, ep])), s4)
  where
    isD c = c >= 0x30 && c <= 0x39

-- | A valid token's parts: sign, digits as an integer, power of ten.
numParts :: String -> (Bool, Integer, Integer, Int)
numParts t0 =
  let (neg, t) = case t0 of
        ('-' : r) -> (True, r)
        r -> (False, r)
      (ip, r1) = span isDigit t
      (fp, r2) = case r1 of
        ('.' : r) -> span isDigit r
        _ -> ("", r1)
      ex = case r2 of
        (e : r) | e == 'e' || e == 'E' -> case r of
          ('+' : ds) -> read ds
          ('-' : ds) -> negate (read ds)
          ds -> read ds
        _ -> 0 :: Integer
      digits = dropWhile (== '0') (ip ++ fp)
      m = if null digits then 0 else read digits :: Integer
   in (neg, m, ex - toInteger (length fp), length digits)

isIntToken :: String -> Bool
isIntToken t = all isDigit (dropWhile (== '-') t) && t /= "-0" && not (null (dropWhile (== '-') t))

validNumber :: String -> Bool
validNumber t = case number (BC.pack t) of
  Right (_, r) -> B.null r
  Left _ -> False

-- ======================================================================
-- the read

data Label = LNode Int String | LSkipped

data RS = RS
  { rsNodes :: IM.IntMap Node
  , rsNext :: Int
  , rsLabels :: M.Map Integer Label
  , rsOpen :: S.Set Integer
  }

type R a = StateT RS (Either String) a

no :: String -> R a
no = lift . Left

-- | The one text read: the whole value or a refusal (§6.5, §16.2).
fromJson :: Unit -> Rec -> B.ByteString -> Either String Doc
fromJson u root text = do
  j <- parseJ text
  (v, st) <- runStateT (rootValue j) (RS IM.empty 1 M.empty S.empty)
  pure (Doc v (rsNodes st))
  where
    rootValue j
      | rJsonArray root = case j of
          JArr _ -> arrayRoot root j
          _ -> no "json_root_not_array"
      | otherwise = case j of
          JObj ms -> table u root ms
          _ -> no "kind_mismatch: the root is not an object"
    arrayRoot r j = do
      let f = head (rFields r)
      v <- field u f j
      pure (VRec [v])

keyOK :: B.ByteString -> R ()
keyOK k = when (B.length k > 255) (no "out_of_range: a schema name past 255 bytes")

-- | A table's object: every key it can name, in the text's order.
table :: Unit -> Rec -> [(B.ByteString, J)] -> R Val
table u r ms = do
  let byKey = M.fromList [(BC.pack (fJson f), i) | (i, f) <- zip [0 :: Int ..] (rFields r)]
  placed <- foldM (place byKey) IM.empty ms
  let flds = rFields r
      vals = [IM.lookup i placed | i <- [0 .. length flds - 1]]
      vs = zipWith (\f mv -> fromMaybe (fieldDefault u f) mv) flds vals
      guardOn f = all (\(gi, want) -> vs !! gi == VBool want) (fGuards f)
  forM_ (zip flds vals) $ \(f, mv) ->
    when (fRequired f && not (isJust mv) && guardOn f) (no ("missing_required: " ++ fName f))
  pure (VRec vs)
  where
    place byKey acc (k, j) = do
      when (B.isPrefixOf (BC.pack "&") k) (no "malformed: a & key where the reader places keys")
      keyOK k
      case M.lookup k byKey of
        Nothing -> skip j >> pure acc
        Just i -> do
          v <- field u (rFields r !! i) j
          pure (IM.insert i v acc)

field :: Unit -> Field -> J -> R Val
field u f j = do
  v <- val u (fTy f) j
  when (overTotal f v) (no "length_over_bound: past the text list's total")
  case (fTy f, v) of
    (TEnum e, VEnum o) | o > 0 -> do
      let (_, _, dep) = eVariants (uEnums u M.! e) !! (o - 1)
      pure (if dep then fieldDefault u f else v)
    _ -> pure v

-- | A skipped value: nothing placed, every rule the bytes alone decide held.
-- SPEC-GAP 9: every object key inside a skipped value is held to the 255-byte cap.
-- SPEC-GAP 10: only `&node` takes the label rules here; another `&` key is data.
skip :: J -> R ()
skip = \case
  JArr xs -> mapM_ skip xs
  JObj ms -> do
    forM_ (zip [0 :: Int ..] ms) $ \(i, (k, _)) -> do
      keyOK k
      when (k == amp && i > 0) (no "malformed: &node after a field")
    case ms of
      ((k, lj) : rest) | k == amp -> do
        lab <- label lj
        st <- get
        if null rest
          then unless (M.member lab (rsLabels st)) (no "malformed: a reference to a label not defined")
          else do
            when (M.member lab (rsLabels st) || S.member lab (rsOpen st)) (no "malformed: a label defined twice")
            put st {rsOpen = S.insert lab (rsOpen st)}
            mapM_ (skip . snd) rest
            modify (\s -> s {rsOpen = S.delete lab (rsOpen s), rsLabels = M.insert lab LSkipped (rsLabels s)})
      _ -> mapM_ (skip . snd) ms
  _ -> pure ()

amp :: B.ByteString
amp = BC.pack "&node"

label :: J -> R Integer
label = \case
  JNum t | all isDigit t && take 1 t /= "0" && not (null t) -> do
    let n = read t
    when (n > 18446744073709551615) (no "malformed: a label past u64")
    pure n
  _ -> no "malformed: a label not spelled as a positive integer"

newNode :: Node -> R Int
newNode n = do
  st <- get
  let i = rsNext st
  put st {rsNodes = IM.insert i n (rsNodes st), rsNext = i + 1}
  pure i

val :: Unit -> Ty -> J -> R Val
val u ty j = case (ty, j) of
  (TOpt _, JNull) -> pure (VOpt Nothing)
  (TOpt t, _) -> VOpt . Just <$> val u t j
  (TPtr _, JNull) -> pure (VPtr 0)
  (TBlob _, JNull) -> pure (VPtr 0)
  (_, JNull) -> no "kind_mismatch: null"
  (TBool, JBool b) -> pure (VBool b)
  (TInt w _ _, _) -> integer (w > 4) j >>= ranged
  (TBits n, _) -> integer (n > 32) j >>= ranged
  (TFixedPt _ i f _, _) -> fixedPt (i + f > 32) f j >>= ranged
  (TTime, JStr s) -> either (const (no "timestamp_not_rfc3339")) (pure . VI) (parseTime (BC.unpack s))
  (TFloat32 c, JNum t) -> do
    w <- maybe (no "out_of_range: past float32") pure (readF32 t)
    case c of
      Nothing -> pure (VF32 w)
      Just (lo, hi, _) -> do
        let w' = if f32IsNegZero w then 0 else w
            x = f32ToRat w'
        when (x < lo || x > hi) (no "out_of_range")
        pure (VF32 w')
  (TFloat64, JNum t) -> maybe (no "out_of_range: past float64") (pure . VF64) (readF64 t)
  (TStr b, JStr s) -> do
    when (maybe False (\n -> toInteger (B.length s) > n) b) (no "length_over_bound")
    pure (VBytes s)
  (TBytes n, JStr s) -> do
    bs <- either no pure (base64Decode s)
    when (toInteger (B.length bs) > n) (no "length_over_bound")
    pure (VBytes bs)
  (TEnum e, JStr s) -> do
    keyOK s
    let vars = eVariants (uEnums u M.! e)
    pure $ case [i | (i, (n, _, _)) <- zip [1 ..] vars, BC.pack n == s] of
      (i : _) -> VEnum i
      [] -> VEnum 0 -- None, or an unknown name: None, counted
  (TFlags fl, JArr xs) -> do
    let names = flVariants (uFlags u M.! fl)
    (bits, _) <- foldM (flag names) (0 :: Word64, S.empty) xs
    pure (VFlags bits)
  (TRecT n, _) -> do
    let r = lookupRec u n
    if rJsonArray r
      then case j of
        JArr _ -> VRec . (: []) <$> field u (head (rFields r)) j
        _ -> no "kind_mismatch: a json=array table that is not an array"
      else case j of
        JObj ms -> table u r ms
        _ -> no "kind_mismatch: a table that is not an object"
  (TUnionT un, JObj ms) -> case ms of
    [] -> pure (VUnion 0 VUnit)
    [(k, aj)] -> do
      when (B.isPrefixOf (BC.pack "&") k) (no "malformed: a & key in a union")
      keyOK k
      let arms = uArms (uUnions u M.! un)
      case [(i, a) | (i, a) <- zip [1 ..] arms, BC.pack (aName a) == k] of
        [] -> skip aj >> pure (VUnion 0 VUnit) -- unknown arm, or None: counted
        ((i, a) : _) -> case aTy a of
          Nothing -> case aj of
            JNull -> pure (VUnion i VUnit)
            _ -> no "kind_mismatch: a payload-free arm holding a value"
          Just at -> VUnion i <$> val u at aj
    _ -> no "malformed: a union with two keys"
  (TPtr n, JObj ms) -> pointer u n ms
  (TBlob isStr, JStr s) ->
    if isStr
      then VPtr <$> newNode (NBlob True s)
      else do
        bs <- either no pure (base64Decode s)
        VPtr <$> newNode (NBlob False bs)
  (TArr sh et, JArr xs) -> do
    let n = toInteger (length xs)
    case sh of
      SFixedN m -> when (n > m) (no "count_over_bound")
      SCount a b -> when (n > b || n < a) (no "count_over_bound") -- SPEC-GAP 11
      SUnbounded -> pure ()
    vs <- forM xs $ \x -> element et x
    pure $ case sh of
      SFixedN m -> VArr (vs ++ replicate (fromInteger m - length vs) (defaultOf u et))
      _ -> VArr vs
  (TKeyed e et, JObj ms) -> do
    let vars = eVariants (uEnums u M.! e)
    slots <- foldM (\acc (k, x) -> do
                      when (B.isPrefixOf (BC.pack "&") k) (no "malformed: a & key in an enum-keyed array")
                      keyOK k
                      case [i | (i, (n, _, _)) <- zip [0 :: Int ..] vars, BC.pack n == k] of
                        (i : _) -> do
                          v <- val u et x
                          pure (IM.insert i v acc)
                        [] -> skip x >> pure acc) IM.empty ms
    pure (VKeyed [IM.findWithDefault (defaultOf u et) i slots | i <- [0 .. length vars - 1]])
  (TMap kt vt, JObj ms) -> do
    ents <- forM ms $ \(k, x) -> do
      kv <- mapKey kt k
      v <- val u vt x
      pure (kv, v)
    pure (VMap (sortMapEntries ents))
  _ -> no "kind_mismatch"
  where
    ranged v = case intRange ty of
      Just (lo, hi) | v < lo || v > hi -> no "out_of_range"
      _ -> pure (VI v)
    element et x
      | isTextListOpt et = case x of
          JNull -> pure (VOpt Nothing)
          JStr _ -> val u et x
          _ -> no "kind_mismatch: a text-list element"
      | otherwise = val u et x
    flag names (acc, seen) x = case x of
      JStr s -> do
        keyOK s
        case [i | (i, n) <- zip [0 :: Int ..] names, BC.pack n == s] of
          (i : _) -> do
            when (S.member i seen) (no "malformed: a flag named twice")
            pure (acc .|. (1 `shiftL` i), S.insert i seen)
          [] -> pure (acc, seen) -- unknown: skipped, counted
      _ -> no "kind_mismatch: a flags element that is not a string"

-- | An integer: a number token (narrow) or a string of digits (wide).
integer :: Bool -> J -> R Integer
integer wide j = case (wide, j) of
  (False, JNum t) -> tok t
  (True, JStr s) -> do
    let t = BC.unpack s
    unless (validNumber t) (no "malformed: a wide integer that is no number")
    tok t
  _ -> no "kind_mismatch: an integer of the wrong JSON type"
  where
    tok t
      | isIntToken t = pure (read (dropPlus t))
      | otherwise = no "kind_mismatch: a token no integer writer produces"
    dropPlus ('-' : r) = '-' : r
    dropPlus r = r

fixedPt :: Bool -> Int -> J -> R Integer
fixedPt wide f j = do
  t <- case (wide, j) of
    (False, JNum t) -> pure t
    (True, JStr s) -> do
      let t = BC.unpack s
      unless (validNumber t) (no "kind_mismatch: a wide fixed that is no number")
      pure t
    _ -> no "kind_mismatch: a fixed value of the wrong JSON type"
  let (neg, m, e, nd) = numParts t
  if m == 0
    then pure 0
    else do
      let adj = e + toInteger nd
      when (adj > 60) (no "out_of_range")
      when (adj < -60) (no "kind_mismatch: finer than the fraction")
      let r = (if e >= 0 then fromInteger (m * 10 ^ e) else m % (10 ^ negate e)) * (2 ^ f) :: Rational
      unless (denominator r == 1) (no "kind_mismatch: finer than the fraction")
      pure (if neg then negate (numerator r) else numerator r)

readF :: (Rational -> Maybe w) -> Int -> String -> Maybe (Either Bool w)
readF conv hiExp t =
  let (neg, m, e, nd) = numParts t
      adj = e + toInteger nd
   in if m == 0
        then Just (Left neg)
        else
          if adj > toInteger hiExp
            then Nothing
            else
              if adj < negate (toInteger hiExp) - 30
                then Just (Left neg)
                else
                  let r = if e >= 0 then fromInteger (m * 10 ^ e) else m % (10 ^ negate e)
                   in Right <$> conv (if neg then negate r else r)

readF32 :: String -> Maybe Word32
readF32 t = case readF ratToF32 40 t of
  Nothing -> Nothing
  Just (Left neg) -> Just (if neg then 0x80000000 else 0)
  Just (Right w) -> Just w

readF64 :: String -> Maybe Word64
readF64 t = case readF ratToF64 310 t of
  Nothing -> Nothing
  Just (Left neg) -> Just (if neg then 0x8000000000000000 else 0)
  Just (Right w) -> Just w

mapKey :: Ty -> B.ByteString -> R Val
mapKey kt k = case kt of
  TStr b -> do
    when (maybe False (\n -> toInteger (B.length k) > n) b) (no "length_over_bound: a map key")
    pure (VBytes k)
  TInt {} -> do
    let t = BC.unpack k
    unless (validNumber t) (no "malformed: an integer map key")
    unless (isIntToken t) (no "kind_mismatch: an integer map key")
    let v = read t
    case intRange kt of
      Just (lo, hi) | v < lo || v > hi -> no "out_of_range: a map key"
      _ -> pure (VI v)
  _ -> no "kind_mismatch: a map key kind"

pointer :: Unit -> String -> [(B.ByteString, J)] -> R Val
pointer u n ms = do
  forM_ (zip [0 :: Int ..] ms) $ \(i, (k, _)) -> when (k == amp && i > 0) (no "malformed: &node after a field")
  case ms of
    ((k, lj) : rest) | k == amp -> do
      lab <- label lj
      st <- get
      if null rest
        then case M.lookup lab (rsLabels st) of
          Just (LNode i tn)
            | tn == n -> pure (VPtr i)
            | otherwise -> no "kind_mismatch: a label of another table"
          Just LSkipped -> pure (VPtr 0)
          Nothing -> no "malformed: a reference to a label not defined"
        else do
          when (M.member lab (rsLabels st) || S.member lab (rsOpen st)) (no "malformed: a label defined twice")
          i <- newNode (NRec n VUnit)
          modify (\s -> s {rsOpen = S.insert lab (rsOpen s)})
          v <- table u (lookupRec u n) rest
          modify (\s -> s {rsOpen = S.delete lab (rsOpen s), rsLabels = M.insert lab (LNode i n) (rsLabels s), rsNodes = IM.insert i (NRec n v) (rsNodes s)})
          pure (VPtr i)
    _ -> do
      i <- newNode (NRec n VUnit)
      v <- table u (lookupRec u n) ms
      modify (\s -> s {rsNodes = IM.insert i (NRec n v) (rsNodes s)})
      pure (VPtr i)

-- ======================================================================
-- base64 (§16.8): the standard alphabet, padded, pad bits zero, and only the
-- spelling the writer writes

b64chars :: B.ByteString
b64chars = BC.pack "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"

base64Encode :: B.ByteString -> B.ByteString
base64Encode bs = B.pack (go (B.unpack bs))
  where
    ch i = B.index b64chars (fromIntegral i)
    go (a : b : c : r) =
      let n = (toI a `shiftL` 16) .|. (toI b `shiftL` 8) .|. toI c
       in [ch (n `shiftR` 18), ch ((n `shiftR` 12) .&. 63), ch ((n `shiftR` 6) .&. 63), ch (n .&. 63)] ++ go r
    go [a, b] =
      let n = (toI a `shiftL` 16) .|. (toI b `shiftL` 8)
       in [ch (n `shiftR` 18), ch ((n `shiftR` 12) .&. 63), ch ((n `shiftR` 6) .&. 63), 0x3D]
    go [a] =
      let n = toI a `shiftL` 16
       in [ch (n `shiftR` 18), ch ((n `shiftR` 12) .&. 63), 0x3D, 0x3D]
    go [] = []
    toI :: Word8 -> Int
    toI = fromIntegral

base64Decode :: B.ByteString -> Either String B.ByteString
base64Decode s = do
  when (B.length s `mod` 4 /= 0) (Left "kind_mismatch: not base64")
  let body = B.takeWhile (/= 0x3D) s
      pad = B.length s - B.length body
  unless (pad <= 2 && B.all (== 0x3D) (B.drop (B.length body) s)) (Left "kind_mismatch: not base64")
  vals <- forM (B.unpack body) $ \c -> maybe (Left "kind_mismatch: not base64") Right (B.elemIndex c b64chars)
  let bits = concatMap (\v -> [testBit v i | i <- [5, 4 .. 0]]) vals
      nbytes = (length vals * 6) `div` 8
      bytes = [fromIntegral (foldl (\a b -> a * 2 + fromEnum b) 0 (take 8 (drop (8 * k) bits))) | k <- [0 .. nbytes - 1]]
      out = B.pack bytes
  unless (base64Encode out == s) (Left "base64_not_canonical")
  pure out

-- ======================================================================
-- timestamps (§16.2, W13)

daysFromCivil :: Integer -> Integer -> Integer -> Integer
daysFromCivil y0 m d =
  let y = if m <= 2 then y0 - 1 else y0
      era = (if y >= 0 then y else y - 399) `div` 400
      yoe = y - era * 400
      doy = (153 * (if m > 2 then m - 3 else m + 9) + 2) `div` 5 + d - 1
      doe = yoe * 365 + yoe `div` 4 - yoe `div` 100 + doy
   in era * 146097 + doe - 719468

civilFromDays :: Integer -> (Integer, Integer, Integer)
civilFromDays z0 =
  let z = z0 + 719468
      era = (if z >= 0 then z else z - 146096) `div` 146097
      doe = z - era * 146097
      yoe = (doe - doe `div` 1460 + doe `div` 36524 - doe `div` 146096) `div` 365
      y = yoe + era * 400
      doy = doe - (365 * yoe + yoe `div` 4 - yoe `div` 100)
      mp = (5 * doy + 2) `div` 153
      d = doy - (153 * mp + 2) `div` 5 + 1
      m = if mp < 10 then mp + 3 else mp - 9
   in (if m <= 2 then y + 1 else y, m, d)

parseTime :: String -> Either String Integer
parseTime s = do
  let num k t = if length (take k t) == k && all isDigit (take k t) then Right (read (take k t) :: Integer, drop k t) else Left "bad"
      lit c t = case t of
        (x : r) | x == c -> Right r
        _ -> Left "bad"
  (y, r1) <- num 4 s
  r2 <- lit '-' r1
  (mo, r3) <- num 2 r2
  r4 <- lit '-' r3
  (d, r5) <- num 2 r4
  r6 <- case r5 of
    (c : r) | c == 'T' || c == 't' -> Right r
    _ -> Left "bad"
  (hh, r7) <- num 2 r6
  r8 <- lit ':' r7
  (mi, r9) <- num 2 r8
  r10 <- lit ':' r9
  (ss, r11) <- num 2 r10
  (frac, r12) <- case r11 of
    ('.' : r) -> let (ds, rr) = span isDigit r in if null ds then Left "bad" else Right (ds, rr)
    _ -> Right ("", r11)
  off <- case r12 of
    [c] | c == 'Z' || c == 'z' -> Right 0
    [sg, h1, h2, ':', m1, m2]
      | (sg == '+' || sg == '-') && all isDigit [h1, h2, m1, m2] -> do
          let oh = read [h1, h2] :: Integer
              om = read [m1, m2]
          when (oh > 23 || om > 59) (Left "bad")
          Right ((if sg == '-' then -1 else 1) * (oh * 60 + om))
    _ -> Left "bad"
  let leap = (y `mod` 4 == 0 && y `mod` 100 /= 0) || y `mod` 400 == 0
      mdays = [31, if leap then 29 else 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  when (mo < 1 || mo > 12) (Left "bad")
  when (d < 1 || d > mdays !! fromInteger (mo - 1)) (Left "bad")
  when (hh > 23 || mi > 59 || ss > 59) (Left "bad")
  let ns = read (take 9 (frac ++ replicate 9 '0')) :: Integer
      secs = daysFromCivil y mo d * 86400 + hh * 3600 + mi * 60 + ss - off * 60
      total = secs * 1000000000 + ns
  when (total < -(2 ^ (63 :: Int)) || total > 2 ^ (63 :: Int) - 1) (Left "bad")
  pure total

showTime :: Integer -> String
showTime t =
  let (secs, ns) = t `divMod` 1000000000
      (days, sod) = secs `divMod` 86400
      (y, m, d) = civilFromDays days
      (hh, r) = sod `divMod` 3600
      (mi, ss) = r `divMod` 60
      p k n = let x = show n in replicate (k - length x) '0' ++ x
      fr = if ns == 0 then "" else '.' : reverse (dropWhile (== '0') (reverse (p 9 ns)))
   in p 4 y ++ "-" ++ p 2 m ++ "-" ++ p 2 d ++ "T" ++ p 2 hh ++ ":" ++ p 2 mi ++ ":" ++ p 2 ss ++ fr ++ "Z"

-- ======================================================================
-- the write (§16.14)

data WS = WS {wsCount :: IM.IntMap Int, wsOpen :: IS.IntSet, wsLabel :: IM.IntMap Int, wsNextLabel :: Int}

type W a = StateT WS (Either String) a

wno :: String -> W a
wno = lift . Left

-- | ToJson: the canonical text, or a refusal (W0).
toJson :: Unit -> Rec -> Doc -> Either String B.ByteString
toJson u root doc = do
  checkValue u root doc
  checkNames u root doc
  st <- execStateT (countRec u doc root (docRoot doc)) (WS IM.empty IS.empty IM.empty 1)
  (b, _) <- runStateT (wTable u doc 0 root (docRoot doc)) st {wsOpen = IS.empty}
  pure (BL.toStrict (BB.toLazyByteString (b <> BB.char7 '\n')))

-- | W0's refusals the wire writer has no reason to make: a deprecated enum
-- variant held, a flags bit no variant names.
checkNames :: Unit -> Rec -> Doc -> Either String ()
checkNames u root doc = do
  goRec root (docRoot doc)
  forM_ (IM.elems (docNodes doc)) $ \case
    NRec tn v -> goRec (lookupRec u tn) v
    _ -> pure ()
  where
    goRec r (VRec vs) = sequence_ (zipWith (\f v -> go (fTy f) v) (rFields r) vs)
    goRec _ _ = pure ()
    go ty v = case (ty, v) of
      (TOpt t, VOpt (Just x)) -> go t x
      (TEnum e, VEnum o) | o > 0 -> do
        let (_, _, dep) = eVariants (uEnums u M.! e) !! (o - 1)
        when dep (Left "invalid_value: a deprecated variant")
      (TFlags fl, VFlags x) -> do
        let n = length (flVariants (uFlags u M.! fl))
        when (n < 64 && x `shiftR` n /= 0) (Left "invalid_value: a flags bit no variant names")
      (TRecT n, _) -> goRec (lookupRec u n) v
      (TArr _ t, VArr xs) -> mapM_ (go t) xs
      (TKeyed _ t, VKeyed xs) -> mapM_ (go t) xs
      (TMap _ t, VMap es) -> mapM_ (go t . snd) es
      (TUnionT un, VUnion i x) | i > 0 -> maybe (pure ()) (\at -> go at x) (aTy (uArms (uUnions u M.! un) !! (i - 1)))
      _ -> pure ()

written :: Field -> [Val] -> Val -> Bool
written f vs v = all (\(gi, want) -> vs !! gi == VBool want) (fGuards f) && v /= VOpt Nothing

-- W1: how many slots name each node, a node's fields walked once.
countRec :: Unit -> Doc -> Rec -> Val -> W ()
countRec u doc r (VRec vs) = forM_ (zip (rFields r) vs) $ \(f, v) -> when (written f vs v) (countTy u doc (fTy f) v)
countRec _ _ _ _ = pure ()

countTy :: Unit -> Doc -> Ty -> Val -> W ()
countTy u doc ty v = case (ty, v) of
  (TPtr _, VPtr n) | n /= 0 -> visit n
  (TBlob _, VPtr n) | n /= 0 -> visit n
  (TRecT rn, _) -> countRec u doc (lookupRec u rn) v
  (TOpt t, VOpt (Just x)) -> countTy u doc t x
  (TArr _ t, VArr xs) -> mapM_ (countTy u doc t) xs
  (TKeyed _ t, VKeyed xs) -> mapM_ (countTy u doc t) xs
  (TMap _ t, VMap es) -> mapM_ (countTy u doc t . snd) (sortMapEntries es)
  (TUnionT un, VUnion i x) | i > 0 -> maybe (pure ()) (\at -> countTy u doc at x) (aTy (uArms (uUnions u M.! un) !! (i - 1)))
  _ -> pure ()
  where
    visit n = do
      st <- get
      when (IS.member n (wsOpen st)) (wno "data_cycle")
      let c = IM.findWithDefault 0 n (wsCount st)
      put st {wsCount = IM.insert n (c + 1) (wsCount st)}
      when (c == 0) $ case IM.lookup n (docNodes doc) of
        Just (NRec tn nv) -> do
          modify (\s -> s {wsOpen = IS.insert n (wsOpen s)})
          countRec u doc (lookupRec u tn) nv
          modify (\s -> s {wsOpen = IS.delete n (wsOpen s)})
        Just (NBlob _ _) -> pure ()
        Nothing -> wno "invalid_value: a pointer to no node"
      when (c >= 1) $ case IM.lookup n (docNodes doc) of
        Just (NBlob _ _) -> wno "blob_shared"
        _ -> pure ()

nl :: Int -> BB.Builder
nl d = BB.char7 '\n' <> BB.string7 (replicate (2 * d) ' ')

members :: Int -> [BB.Builder] -> BB.Builder
members _ [] = BB.string7 "{}"
members d ms = BB.char7 '{' <> mconcat (zipWith (\i m -> (if i > (0 :: Int) then BB.char7 ',' else mempty) <> nl (d + 1) <> m) [0 ..] ms) <> nl d <> BB.char7 '}'

elements :: Int -> [BB.Builder] -> BB.Builder
elements _ [] = BB.string7 "[]"
elements d es = BB.char7 '[' <> mconcat (zipWith (\i m -> (if i > (0 :: Int) then BB.char7 ',' else mempty) <> nl (d + 1) <> m) [0 ..] es) <> nl d <> BB.char7 ']'

member :: B.ByteString -> BB.Builder -> BB.Builder
member k v = jstr k <> BB.string7 ": " <> v

-- W8
jstr :: B.ByteString -> BB.Builder
jstr s = BB.char7 '"' <> mconcat (map esc (B.unpack s)) <> BB.char7 '"'
  where
    esc c = case c of
      0x22 -> BB.string7 "\\\""
      0x5C -> BB.string7 "\\\\"
      0x08 -> BB.string7 "\\b"
      0x0C -> BB.string7 "\\f"
      0x0A -> BB.string7 "\\n"
      0x0D -> BB.string7 "\\r"
      0x09 -> BB.string7 "\\t"
      _
        | c < 0x20 -> BB.string7 "\\u00" <> BB.word8HexFixed c
        | otherwise -> BB.word8 c

textOK :: B.ByteString -> W ()
textOK s = unless (validText s) (wno "invalid_value: storage that is not text")

-- W3 (and W14 for a json=array table)
wTable :: Unit -> Doc -> Int -> Rec -> Val -> W BB.Builder
wTable u doc d r v@(VRec vs)
  | rJsonArray r = wVal u doc d (fTy (head (rFields r))) (head vs)
  | otherwise = do
      when (d > maxDepth) (wno "a table value past the depth cap")
      ms <- fieldsOf u doc d r v
      pure (members d ms)
  where
    _ = vs
wTable _ _ _ _ _ = wno "invalid_value: not a record"

fieldsOf :: Unit -> Doc -> Int -> Rec -> Val -> W [BB.Builder]
fieldsOf u doc d r (VRec vs) =
  fmap concat . forM (zip (rFields r) vs) $ \(f, v) ->
    if written f vs v
      then do
        b <- wVal u doc (d + 1) (fTy f) v
        pure [member (BC.pack (fJson f)) b]
      else pure []
fieldsOf _ _ _ _ _ = wno "invalid_value: not a record"

wVal :: Unit -> Doc -> Int -> Ty -> Val -> W BB.Builder
wVal u doc d ty v = case (ty, v) of
  (TOpt t, VOpt (Just x)) -> wVal u doc d t x
  (TOpt _, VOpt Nothing) -> pure (BB.string7 "null")
  (TBool, VBool b) -> pure (BB.string7 (if b then "true" else "false"))
  (TInt w _ _, VI n) -> pure (intOut (w > 4) n)
  (TBits n, VI x) -> pure (intOut (n > 32) x)
  (TFixedPt _ i f _, VI x) -> pure (let t = fixedText f x in if i + f > 32 then jstr (BC.pack t) else BB.string7 t)
  (TTime, VI x) -> pure (jstr (BC.pack (showTime x)))
  (TFloat32 _, VF32 w) -> do
    unless (f32IsFinite w) (wno "invalid_value: a non-finite float")
    pure (BB.string7 (fmtFloat32 w))
  (TFloat64, VF64 w) -> do
    unless (f64IsFinite w) (wno "invalid_value: a non-finite float")
    pure (BB.string7 (fmtFloat64 w))
  (TStr _, VBytes s) -> textOK s >> pure (jstr s)
  (TBytes _, VBytes s) -> pure (jstr (base64Encode s))
  (TEnum e, VEnum o) -> pure (jstr (BC.pack (if o == 0 then "None" else let (n, _, _) = eVariants (uEnums u M.! e) !! (o - 1) in n)))
  (TFlags fl, VFlags x) -> do
    let names = flVariants (uFlags u M.! fl)
    pure (elements d [jstr (BC.pack n) | (i, n) <- zip [0 ..] names, testBit x i])
  (TRecT n, _) -> wTable u doc d (lookupRec u n) v
  (TUnionT un, VUnion i x) ->
    if i == 0
      then pure (BB.string7 "{}")
      else do
        let a = uArms (uUnions u M.! un) !! (i - 1)
        b <- maybe (pure (BB.string7 "null")) (\at -> wVal u doc (d + 1) at x) (aTy a)
        pure (members d [member (BC.pack (aName a)) b])
  (TPtr _, VPtr 0) -> pure (BB.string7 "null")
  (TBlob _, VPtr 0) -> pure (BB.string7 "null")
  (TBlob _, VPtr n) -> case IM.lookup n (docNodes doc) of
    Just (NBlob True s) -> textOK s >> pure (jstr s)
    Just (NBlob False s) -> pure (jstr (base64Encode s))
    _ -> wno "invalid_value: a blob slot naming no blob"
  (TPtr _, VPtr n) -> case IM.lookup n (docNodes doc) of
    Just (NRec tn nv) -> do
      let r = lookupRec u tn
      st <- get
      if IM.findWithDefault 0 n (wsCount st) > 1
        then case IM.lookup n (wsLabel st) of
          Just l -> do
            when (d > maxDepth) (wno "a table value past the depth cap")
            pure (members d [member amp (BB.intDec l)])
          Nothing -> do
            when (d > maxDepth) (wno "a table value past the depth cap")
            let l = wsNextLabel st
            put st {wsLabel = IM.insert n l (wsLabel st), wsNextLabel = l + 1}
            ms <- fieldsOf u doc d r nv
            when (null ms) (wno "invalid_value: a &node definition holding no field")
            pure (members d (member amp (BB.intDec l) : ms))
        else wTable u doc d r nv
    _ -> wno "invalid_value: a pointer naming no table"
  (TArr _ et, VArr xs) -> elements d <$> mapM (wVal u doc (d + 1) et) xs
  (TKeyed e et, VKeyed xs) -> do
    let vars = eVariants (uEnums u M.! e)
    bs <- forM (zip vars xs) $ \((n, _, _), x) -> member (BC.pack n) <$> wVal u doc (d + 1) et x
    pure (members d bs)
  (TMap kt vt, VMap es) -> do
    bs <- forM (sortMapEntries es) $ \(k, x) -> do
      kb <- case (kt, k) of
        (TStr _, VBytes s) -> textOK s >> pure s
        (_, VI n) -> pure (BC.pack (show n))
        _ -> wno "invalid_value: a map key"
      member kb <$> wVal u doc (d + 1) vt x
    pure (members d bs)
  _ -> wno ("invalid_value: a value of the wrong shape for " ++ show ty)
  where
    intOut wide n = if wide then jstr (BC.pack (show n)) else BB.string7 (show n)

-- W11
fixedText :: Int -> Integer -> String
fixedText f r =
  let a = abs r
      whole = a `shiftR` f
      frac = a .&. (2 ^ f - 1)
      digits 0 = []
      digits x = let y = x * 10 in chr (ord '0' + fromInteger (y `shiftR` f)) : digits (y .&. (2 ^ f - 1))
      fd = if frac == 0 then "0" else digits frac
   in (if r < 0 then "-" else "") ++ show whole ++ "." ++ fd
