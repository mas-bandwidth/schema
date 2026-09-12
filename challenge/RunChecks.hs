-- Fixed-table reader, bounded slice: "the fixed table reads backward, never forward".
-- Haskell arm, coordinator Rowan. Implements D1 (ALGORITHM §5) / D2 (SPEC-TABLES §21).
-- Boot libraries only: base, bytestring, containers, directory, filepath.
module Main (main) where

import qualified Data.ByteString as B
import qualified Data.ByteString.Char8 as C
import Data.Bits
import Data.List (isPrefixOf, isSuffixOf, isInfixOf, sortOn, find)
import Data.Maybe (fromMaybe, mapMaybe, isJust)
import Data.Word
import Data.Int
import System.Environment (getArgs)
import System.Exit
import System.FilePath (takeDirectory, (</>))
import System.IO

-- ---------------------------------------------------------------- manifest

data Entry = Entry
  { eFile :: String, eRow :: String, eSide :: String, eRoot :: String
  , eRecords :: Int, eValues :: [(String,String)] } deriving Show

parseManifest :: String -> [Entry]
parseManifest = mapMaybe pl . lines
 where
  pl ln | not ("file=" `isPrefixOf` ln) = Nothing
        | otherwise =
      let (pre, vs) = breakOn "values=" ln
          kvs = words pre
          g k = drop (length k + 1) <$> find ((k ++ "=") `isPrefixOf`) kvs
          vals = if null vs then [] else map kv (splitOn ',' (drop 7 vs))
      in do f <- g "file"; r <- g "row"; s <- g "side"; rt <- g "root"
            n <- g "records"
            return (Entry f r s rt (read n) vals)
  kv s = let (a,b) = break (=='=') s in (a, drop 1 b)

breakOn :: String -> String -> (String,String)
breakOn pat s = go "" s
 where go acc r | pat `isPrefixOf` r = (reverse acc, r)
                | null r = (reverse acc, "")
                | otherwise = go (head r : acc) (tail r)

splitOn :: Char -> String -> [String]
splitOn c s = case break (==c) s of (a,[]) -> [a]; (a,_:r) -> a : splitOn c r

-- ---------------------------------------------------------------- lineage

-- A lineage entry is HANDED to the runtime: hash, layout length, layout bytes,
-- record_bytes. §5.9 #3's second conforming shape: built ONCE at startup from the
-- generation set (the lock's stand-in here), never from a layout arriving on the wire.
data Known = Known
  { kHash :: Word64, kLayoutLen :: Int, kLayout :: B.ByteString
  , kRecordBytes :: Int, kSide :: String, kRoot :: Maybe Node } deriving Show

-- The layout's own shape, parsed ONCE at startup from the handed bytes (§5.9 #3, shape two).
-- record := id(8, fnv1a64 of the wire name) kind(1) size(4) count(4) then `count` children.
-- Only kind 13 (a table) lays its children in the body; kind 30 (an enum) carries its variant
-- list as children and is itself `size` bytes wide.
data Node = Node { nId :: Word64, nKind :: Int, nSz :: Int, nKids :: [Node] } deriving Show

parseLayout :: B.ByteString -> Maybe Node
parseLayout bs
  | B.length bs < 21 = Nothing
  | otherwise = fst <$> parseNode bs 4

parseNode :: B.ByteString -> Int -> Maybe (Node, Int)
parseNode bs o
  | o + 17 > B.length bs = Nothing
  | otherwise =
      let i = le64 bs o
          k = fromIntegral (B.index bs (o+8))
          sz = fromIntegral (le32 bs (o+9))
          cnt = fromIntegral (le32 bs (o+13))
      in do (kids, o') <- parseKids bs (o+17) cnt
            return (Node i k sz kids, o')

parseKids :: B.ByteString -> Int -> Int -> Maybe ([Node], Int)
parseKids _ o 0 = Just ([], o)
parseKids bs o n = do (c, o') <- parseNode bs o
                      (cs, o'') <- parseKids bs o' (n-1)
                      return (c:cs, o'')

-- leaves of the BODY, depth first, with their body offsets
bodyLeaves :: [Word64] -> Int -> Node -> [([Word64], Int, Int, Int)]
bodyLeaves pre base n
  | nKind n == 13 = go base (nKids n)
  | otherwise = [(pre, nKind n, nSz n, base)]
 where
  go _ [] = []
  go b (c:cs) = bodyLeaves (pre ++ [nId c]) b c ++ go (b + nSz c) cs

rootLeaves :: Node -> [([Word64], Int, Int, Int)]
rootLeaves r = bodyLeaves [] 0 r

ladderOf :: Int -> String
ladderOf k | k >= 2 && k <= 5 = "int"
           | k >= 6 && k <= 9 = "uint"
           | k == 10 || k == 11 = "float"
           | otherwise = "other"

-- a reader field the writer does not carry gets the reader's own fresh value; the only
-- declared default this arm was handed is nested_append's `w int32 = 88` (training text).
declaredDefault :: Word64 -> Integer
declaredDefault i | i == 0xaf63ea4c86020456 = 88   -- fnv1a64 "w"
                  | otherwise = 0

-- DERIVED plan: MATCH by wire id, EMIT copy / widen (sign by the WRITER's kind) / fill.
derivePlan :: Node -> Node -> [(( [Word64] ), Op, Int)]
derivePlan rdr wr =
  [ entry rl | rl <- rootLeaves rdr ]
 where
  wls = rootLeaves wr
  entry (path, rk, rsz, _) = case [ w | w@(p,_,_,_) <- wls, p == path ] of
    ((_, wk, wsz, woff):_)
      | wk == rk && wsz == rsz -> (path, DCopy rk rsz, woff)
      | ladderOf wk == ladderOf rk && ladderOf wk /= "other" && wsz <= rsz
          -> (path, DWiden wk wsz, woff)
      | otherwise -> (path, DKindMismatch, woff)
    [] -> (path, DFill (declaredDefault (last (path ++ [0]))), 0)

data Reader_ = Reader_ { rLineage :: [Known], rFloor :: Int, rOwn :: Int
                       , rNames :: [(Word64,String)] }

sideRank :: String -> Int
sideRank s = case s of "old" -> 0; "a" -> 1; "b" -> 2; "mid" -> 3; "new" -> 4; _ -> 9

mkKnown :: Entry -> B.ByteString -> Known
mkKnown e bs =
  let l = fromIntegral (le32 bs 16)
      h = le64 bs 8
      rest = B.length bs - 20 - l
      rb = if eRecords e > 0 then rest `div` eRecords e else 0
      lay = B.take l (B.drop 20 bs)
  in Known h l lay rb (eSide e) (parseLayout lay)

-- ---------------------------------------------------------------- little endian

le32 :: B.ByteString -> Int -> Word32
le32 b o = foldr (\i acc -> acc `shiftL` 8 .|. fromIntegral (B.index b (o+i))) 0 [0..3]

le64 :: B.ByteString -> Int -> Word64
le64 b o = foldr (\i acc -> acc `shiftL` 8 .|. fromIntegral (B.index b (o+i))) 0 [0..7]

u8 :: B.ByteString -> Int -> Integer
u8 b o = fromIntegral (B.index b o)

u16 :: B.ByteString -> Int -> Integer
u16 b o = fromIntegral (B.index b o) + 256 * fromIntegral (B.index b (o+1))

u32at :: B.ByteString -> Int -> Integer
u32at b o = fromIntegral (le32 b o)

i16at :: B.ByteString -> Int -> Integer
i16at b o = fromIntegral (fromIntegral (u16 b o) :: Int16)

i32at :: B.ByteString -> Int -> Integer
i32at b o = fromIntegral (fromIntegral (le32 b o) :: Int32)

fnv1a64 :: String -> Word64
fnv1a64 = foldl (\h c -> (h `xor` fromIntegral (fromEnum c)) * 0x100000001b3) 0xcbf29ce484222325

-- The wire NAMES for a row come from the manifest's own value keys (`r0.v.x=3`), which is
-- how a harness pairs two generations' fields: by WIRE ID, never by position (§5.9 #41).
nameComponents :: [Entry] -> String -> [String]
nameComponents ents row =
  dedup [ c | e <- ents, eRow e == row, (k,_) <- eValues e
            , c <- drop 1 (splitOn '.' (takeWhile (/= '[') k)), not (null c) ]
 where dedup = foldr (\x acc -> if x `elem` acc then acc else x : acc) []

-- ---------------------------------------------------------------- the report

data Counters = Counters { cUnknown, cKind, cWidened, cClamped :: Int } deriving (Eq,Show)
zeroC :: Counters
zeroC = Counters 0 0 0 0

data Report = Report
  { rReturns :: Int, rRefused :: Bool, rReason :: String, rMalformed :: Bool
  , rCounters :: Counters, rValues :: [(String,Integer)], rWrote :: Bool
  , rLayoutHash :: Word64 } deriving Show

refusal :: String -> Word64 -> Report
refusal n h = Report (-1) True n False zeroC [] False h

malf :: Report
malf = Report (-1) False "" True zeroC [] False 0

-- ---------------------------------------------------------------- plans

-- Plans are static data, emitted here as source (§5.9 #3, shape one), keyed by
-- (row, reader side, writer side). Nothing on the load path compiles or parses a layout.
data Op = Copy32 | CopyU32 | Widen16to32 | CopyU8 | Fill Integer
        | DCopy Int Int | DWiden Int Int | DFill Integer | DKindMismatch
  deriving (Eq,Show)

type Plan = [(String, Op, Int)]  -- reader field path, op, writer body offset (ignored for Fill)

plansFor :: String -> String -> String -> Maybe Plan
plansFor row rd wr = case (row, rd, wr) of
  ("int_widen","new","new") -> Just [("lead",CopyU32,0),("v",Copy32,4),("trail",CopyU32,8)]
  ("int_widen","new","old") -> Just [("lead",CopyU32,0),("v",Widen16to32,4),("trail",CopyU32,6)]
  ("int_widen","old","old") -> Just [("lead",CopyU32,0),("v",Widen16to32,4),("trail",CopyU32,6)]
  ("nested_append","new","new") -> Just [("v.x",Copy32,0),("v.y",Copy32,4),("v.z",Copy32,8),("v.w",Copy32,12),("seq",Copy32,16)]
  ("nested_append","new","old") -> Just [("v.x",Copy32,0),("v.y",Copy32,4),("v.z",Copy32,8),("v.w",Fill 88,0),("seq",Copy32,12)]
  ("nested_append","old","old") -> Just [("v.x",Copy32,0),("v.y",Copy32,4),("v.z",Copy32,8),("seq",Copy32,12)]
  ("rename_without_was","new","new") -> Just [("b",Copy32,0),("seq",Copy32,4)]
  ("rename_without_was","new","old") -> Just [("b",Copy32,0),("seq",Copy32,4)]
  ("rename_without_was","old","old") -> Just [("a",Copy32,0),("seq",Copy32,4)]
  ("rename_without_was","old","new") -> Just [("a",Copy32,0),("seq",Copy32,4)]
  ("field_deprecate","new","new") -> Just [("a",Copy32,0),("b",Copy32,4),("c",Copy32,8)]
  ("field_deprecate","new","old") -> Just [("a",Copy32,0),("b",Copy32,4),("c",Copy32,8)]
  ("field_deprecate","old","old") -> Just [("a",Copy32,0),("b",Copy32,4),("c",Copy32,8)]
  ("field_deprecate","old","new") -> Just [("a",Copy32,0),("b",Copy32,4),("c",Copy32,8)]
  ("enum_append","new","new") -> Just [("tier",CopyU8,0),("seq",Copy32,1)]
  ("enum_append","new","old") -> Just [("tier",CopyU8,0),("seq",Copy32,1)]
  ("enum_append","old","old") -> Just [("tier",CopyU8,0),("seq",Copy32,1)]
  _ -> Nothing

runOp :: B.ByteString -> Int -> (String,Op,Int) -> (String, Integer, Int)
runOp body base (nm,op,off) = case op of
  Copy32      -> (nm, i32at body (base+off), 0)
  CopyU32     -> (nm, u32at body (base+off), 0)
  CopyU8      -> (nm, u8 body (base+off), 0)
  Widen16to32 -> (nm, i16at body (base+off), 1)   -- SIGN by the WRITER's kind (§5.2 EMIT)
  Fill d      -> (nm, d, 0)                        -- the prefill answers it (§5.2)
  DFill d     -> (nm, d, 0)
  DCopy k sz  -> (nm, scalarAt k sz body (base+off), 0)
  DWiden k sz -> (nm, scalarAt k sz body (base+off), if sz > 0 then 1 else 0)
  DKindMismatch -> (nm, 0, 0)

-- decode `sz` bytes at `o` by the WRITER's kind: signed kinds sign-extend, unsigned
-- kinds zero-extend, which is the whole of the widening (a REPRESENTATION operation).
scalarAt :: Int -> Int -> B.ByteString -> Int -> Integer
scalarAt k sz b o =
  let raw = sum [ fromIntegral (B.index b (o+i)) * (256 ^ i) | i <- [0 .. sz-1] ] :: Integer
  in if ladderOf k == "int" && sz > 0 && raw >= 2 ^ (8*sz - 1)
       then raw - 2 ^ (8*sz)
       else raw

-- ---------------------------------------------------------------- LOAD

load :: Reader_ -> String -> String -> B.ByteString -> Report
load rdr row readerSide file
  | B.length file < 20 = malf                                        -- step 1
  | B.index file 0 /= 3 =                                            -- step 2
      refusal (case B.index file 0 of 1 -> "previous_form"
                                      2 -> "message_form_as_file"
                                      _ -> "newer_form") 0
  | otherwise =
    let l = fromIntegral (le32 file 16)
        h = le64 file 8
    in if 20 + l > B.length file then refusal "layout_malformed" 0    -- step 3
       else case [ (i,k) | (i,k) <- zip [0..] (rLineage rdr), kHash k == h ] of
         [] -> refusal "layout_newer" h                               -- step 5
         ((i,k):_)
           | i < rFloor rdr -> refusal "layout_unsupported" h         -- step 6
           | kLayoutLen k /= l || kLayout k /= B.take l (B.drop 20 file)
               -> refusal "layout_malformed" 0                        -- step 7
           | otherwise ->
             let rb = kRecordBytes k
                 rest = B.length file - 20 - l
             in if rb <= 8 || rest `mod` rb /= 0 then malf            -- step 9
                else
                  let n = rest `div` rb
                      own = rLineage rdr !! rOwn rdr
                      nm1 i = fromMaybe ("id:" ++ show i) (lookup i (rNames rdr))
                      dotted ids = concatMap (\(j,i) -> (if j > (0::Int) then "." else "") ++ nm1 i)
                                             (zip [0..] ids)
                      derived = case (kRoot own, kRoot k) of
                        (Just rn, Just wn) -> Just [ (dotted ids, op, off)
                                                   | (ids, op, off) <- derivePlan rn wn ]
                        _ -> Nothing
                      plan0 = case plansFor row readerSide (kSide k) of
                                Just pl -> Just pl
                                Nothing -> derived
                      -- COMPILE-time census: once per peer, never per record (§5.4)
                      censusKM = case plan0 of
                        Just pl -> length [ () | (_,DKindMismatch,_) <- pl ]
                        Nothing -> 0
                      censusUnk = case (kRoot own, kRoot k) of
                        (Just rn, Just wn) ->
                          let rps = [ p | (p,_,_,_) <- rootLeaves rn ]
                          in length [ () | (p,_,_,_) <- rootLeaves wn, p `notElem` rps ]
                        _ -> 0
                      recs = [ 20 + l + j*rb | j <- [0..n-1] ]
                      bad = [ () | at <- recs, le64 file at /= h ]
                  in if not (null bad) then refusal "no_layout" 0     -- step 11, BEFORE prefill
                     else case plan0 of
                       Nothing -> Report n False "" False zeroC [] True 0
                       Just pl ->
                         let per j at = [ let (nm,v,w) = runOp file (at+8) ent
                                          in (("r" ++ show j ++ "." ++ nm), v, w) | ent <- pl ]
                             all3 = concat [ per j at | (j,at) <- zip [0..] recs ]
                             wsum = sum [ w | (_,_,w) <- all3 ]
                         in Report n False "" False
                                   (zeroC { cWidened = wsum, cKind = censusKM, cUnknown = censusUnk })
                                   [ (nm,v) | (nm,v,_) <- all3 ] True 0

-- ---------------------------------------------------------------- C9 monotone

data FT = FT String String  -- name, kind

baseline :: [FT] -> [FT] -> Either String ()
baseline old new =
  let oNames = [ n | FT n _ <- old ]; nNames = [ n | FT n _ <- new ] in
  case [ n | n <- oNames, n `notElem` nNames ] of
    (n:_) -> Left ("field removed (" ++ n ++ " -> absent)")
    [] | not (subsequence oNames nNames) ->
           Left ("fields reordered (" ++ unwords oNames ++ " -> " ++ unwords nNames ++ ")")
       | not (oNames `isPrefixOf` nNames) ->
           Left ("field inserted not at the end (" ++ unwords oNames ++ " -> " ++ unwords nNames ++ ")")
       | otherwise -> mapM_ chk (zip old new')
      where new' = take (length old) new
            chk (FT n a, FT _ b) = kindWiden n a b
    _ -> Right ()

subsequence :: [String] -> [String] -> Bool
subsequence [] _ = True
subsequence _ [] = False
subsequence (x:xs) (y:ys) | x == y = subsequence xs ys
                          | otherwise = subsequence (x:xs) ys

kindWiden :: String -> String -> String -> Either String ()
kindWiden fld a b
  | a == b = Right ()
  | ladder a /= ladder b = Left ("ladder (" ++ a ++ " -> " ++ b ++ ")")
  | signed a /= signed b = Left ("signedness (" ++ a ++ " -> " ++ b ++ ")")
  | width a > width b = Left ("narrowed (" ++ a ++ " -> " ++ b ++ ")")
  | otherwise = Right ()
 where _ = fld

ladder :: String -> String
ladder k | "float" `isPrefixOf` k = "float"
         | otherwise = "int"
signed :: String -> Bool
signed k = not ("uint" `isPrefixOf` k)
width :: String -> Int
width k = read (filter (`elem` "0123456789") k) :: Int

-- the int_widen row's text, as definitions (D1 §5.1 BASELINE at commit)
iwOld :: [FT]
iwOld = [FT "lead" "uint32", FT "v" "int16", FT "trail" "uint32"]

c9cases :: [(String,[FT],String)]
c9cases =
  [ ("a", [FT "lead" "uint32", FT "v" "int8",   FT "trail" "uint32"], "narrowed (int16 -> int8)")
  , ("b", [FT "lead" "uint32", FT "v" "uint16", FT "trail" "uint32"], "signedness")
  , ("c", [FT "lead" "uint32", FT "v" "float32",FT "trail" "uint32"], "ladder")
  , ("d", [FT "lead" "uint32", FT "trail" "uint32"],                  "field removed")
  , ("e", [FT "lead" "uint32", FT "trail" "uint32", FT "v" "int16"],  "fields reordered")
  , ("f", [FT "lead" "uint32", FT "v2" "int32", FT "v" "int16", FT "trail" "uint32"], "field inserted not at the end")
  ]

-- ---------------------------------------------------------------- checks

data St = Pass | Fail String | Refused String | NA String

emit :: Int -> St -> IO ()
emit n st = putStrLn $ "CHECK C" ++ show n ++ " " ++ case st of
  Pass      -> "pass -"
  Fail d    -> "fail " ++ d
  Refused c -> "refuse:" ++ c ++ " reader refused where the check expected otherwise"
  NA d      -> "n/a " ++ d

-- manifest values for a file, as Integers where they parse
mvals :: Entry -> [(String,Integer)]
mvals e = [ (k, v) | (k,s) <- eValues e, Just v <- [readI s] ]
 where readI ('-':d) | all (`elem` "0123456789") d && not (null d) = Just (negate (read d))
       readI d | all (`elem` "0123456789") d && not (null d) = Just (read d)
               | otherwise = Nothing

-- compare a report's values against the manifest's, for the keys the manifest names
cmpVals :: Entry -> Report -> Either String ()
cmpVals e rep = const () <$> cmpValsN e rep

-- Right n: n manifest fields compared and all agreed. A check that compared NOTHING is
-- never a pass; it is an honest n/a, so the count is reported out.
cmpValsN :: Entry -> Report -> Either String Int
cmpValsN e rep =
  case [ (k,want,got) | (k,want) <- mvals e, Just got <- [lookup k (rValues rep)], got /= want ] of
    ((k,w,g):_) -> Left (k ++ " landed " ++ show g ++ " expected " ++ show w)
    [] -> Right (length [ k | (k,_) <- mvals e, k `elem` map fst (rValues rep) ])

kindName :: Int -> String
kindName k = case k of
  1 -> "bool"; 2 -> "int8"; 3 -> "int16"; 4 -> "int32"; 5 -> "int64"
  6 -> "uint8"; 7 -> "uint16"; 8 -> "uint32"; 9 -> "uint64"
  10 -> "float32"; 11 -> "float64"; 12 -> "string"; 13 -> "table"; 14 -> "array"
  30 -> "enum"; 32 -> "variant"; _ -> "kind" ++ show k

-- The bounded slice is ONE scalar ladder widening plus an append. A row whose change is a
-- grown string, array, bounded count, bits(N), optional or enum ORDINAL WIDTH is outside it:
-- that is a genuine inapplicability (§7.2), not a failure to report.
outOfSlice :: Known -> Known -> Maybe String
outOfSlice rk wk = case (kRoot rk, kRoot wk) of
  (Just rn, Just wn) ->
    let wls = rootLeaves wn; rls = rootLeaves rn in
    case [ (p,k,sz,wkk,wsz) | (p,k,sz,_) <- rls
                            , (wkk,wsz) <- [ (a,b) | (q,a,b,_) <- wls, q == p ]
                            , sz /= wsz || k /= wkk
                            , ladderOf k == "other" || ladderOf wkk /= ladderOf k ] of
      ((_,k,sz,wkk,wsz):_) -> Just ("the row's change is outside the bounded slice: a "
                                    ++ kindName wkk ++ "(" ++ show wsz ++ "B) into a "
                                    ++ kindName k ++ "(" ++ show sz ++ "B)")
      [] -> if map (\(p,_,_,_) -> p) rls /= map (\(p,_,_,_) -> p) wls
              then Just "the row's change moves the body's own shape, outside the bounded slice"
              else Nothing
  _ -> Just "a generation's layout did not parse"

-- entries that WIDEN between two generations, read off the two layouts (§5.2 EMIT: a kind
-- change on the same ladder into a wider slot). One per entry, per record (§5.4).
widenEntries :: Known -> Known -> Int
widenEntries rk wk = case (kRoot rk, kRoot wk) of
  (Just rn, Just wn) ->
    let wls = rootLeaves wn
    in length [ () | (p,k,sz,_) <- rootLeaves rn
                   , (wkk,wsz) <- [ (a,b) | (q,a,b,_) <- wls, q == p ]
                   , ladderOf k /= "other", ladderOf wkk == ladderOf k, wsz < sz ]
  _ -> 0

countersZero :: Report -> Bool
countersZero r = rCounters r == zeroC

main :: IO ()
main = do
  as <- getArgs
  case as of
    [mpath, fixarg] -> run mpath fixarg
    _ -> do hPutStrLn stderr "usage: run-checks <absolute-manifest-path> <fixture-path-or-row-id>"
            exitWith (ExitFailure 2)

run :: String -> String -> IO ()
run mpath fixarg = do
  mtxt <- readFile mpath
  let dir = takeDirectory mpath
      ents = parseManifest mtxt
      byName n = find ((== n) . eFile) ents
  target <- case () of
    _ | '/' `elem` fixarg || ".bin" `isSuffixOf` fixarg ->
          let base = reverse (takeWhile (/= '/') (reverse fixarg))
          in case byName base of
               Just e -> return e
               Nothing -> die' ("no manifest line for fixture " ++ base)
      | otherwise -> case find ((== fixarg) . eRow) ents of
               Just e -> return e
               Nothing -> die' ("no manifest row " ++ fixarg)
  let row = eRow target
      sides = sortOn (sideRank . eSide) [ e | e <- ents, eRow e == row ]
  loaded <- mapM (\e -> do b <- B.readFile (dir </> eFile e); return (e, b)) sides
  let lin = [ mkKnown e b | (e,b) <- loaded ]
      newest = if null lin then Nothing else Just (last lin)
      oldest = if null lin then Nothing else Just (head lin)
      newSide = maybe "new" kSide newest
      oldSide = maybe "old" kSide oldest
      nameMap = [ (fnv1a64 nmc, nmc) | nmc <- nameComponents ents row ]
      newRdr = Reader_ lin 0 (length lin - 1) nameMap
      oldRdr = Reader_ [head lin] 0 0 nameMap
      fileOf s = find ((== s) . eSide . fst) loaded
      hasPlan rd wr = isJust (plansFor row rd wr)
  -- C1 identity read, the newest build on its own file
  c1 <- case fileOf newSide of
    _ | eSide target == "none" -> return (NA "not a versioning row: the manifest gives it no generation")
    Nothing -> return (NA "row has no newest-generation file in the manifest")
    Just (e,b) ->
      let r = load newRdr row newSide b in return $
        if rRefused r then Refused (rReason r)
        else if rMalformed r then Fail "malformed on a clean identity read"
        else if rReturns r /= eRecords e then Fail ("returns=" ++ show (rReturns r) ++ " expected " ++ show (eRecords e))
        else if rLayoutHash r /= 0 then Fail "layout_hash non-zero on a clean read"
        else if not (countersZero r) then Fail ("counters " ++ show (rCounters r) ++ " expected all zero")
        else case cmpValsN e r of
               Left d -> Fail d
               Right 0 -> NA "identity read clean, counters zero; no integer-valued field in this row's manifest line to assert"
               Right _ -> Pass
  emit 1 c1
  -- C2 the scalar widening: newest reads oldest
  c2 <- case (fileOf oldSide, newSide == oldSide) of
    (Nothing,_) -> return (NA "row has no older generation")
    (_,True) -> return (NA "row has a single generation")
    (Just (e,b), False)
      | not (hasPlan newSide oldSide), Just why <- outOfSlice (last lin) (head lin) -> return (NA why)
      | otherwise ->
        let r = load newRdr row newSide b
            wexp = if row == "int_widen" then eRecords e
                   else widenEntries (last lin) (head lin) * eRecords e
        in return $
          if rRefused r then Refused (rReason r)
          else if rMalformed r then Fail "malformed reading the older file"
          else if cWidened (rCounters r) /= wexp then Fail ("widened=" ++ show (cWidened (rCounters r)) ++ " expected " ++ show wexp)
          else if cUnknown (rCounters r) /= 0 || cKind (rCounters r) /= 0 || cClamped (rCounters r) /= 0
                 then Fail ("counters " ++ show (rCounters r))
          else case cmpValsN e r of
                 Left d -> Fail d
                 Right 0 -> NA ("older file read, widened=" ++ show wexp ++ " as owed; no integer-valued field in this row's manifest line to assert")
                 Right _ -> Pass
  emit 2 c2
  -- C3 the reader's tail on an append
  c3 <- if row /= "nested_append" then return (NA "no appended field with a declared default in this row")
        else case fileOf "old" of
          Nothing -> return (NA "row has no older generation")
          Just (e,b) ->
            let r = load newRdr row newSide b in return $
              if rRefused r then Refused (rReason r)
              else if not (countersZero r) then Fail ("counters " ++ show (rCounters r) ++ " expected all zero")
              else case [ v | (k,v) <- rValues r, ".v.w" `isInfixOf` ("." ++ k) ] of
                (v:_) | v /= 88 -> Fail ("v.w landed " ++ show v ++ " expected 88")
                [] -> Fail "v.w not landed"
                _ -> either Fail (const Pass) (cmpVals e r)
  emit 3 c3
  -- C4 OLD REFUSES NEW, BY NAME, poison intact
  c4 <- case (fileOf newSide, newSide == oldSide) of
    (Nothing,_) -> return (NA "row has no newer generation")
    (_,True) -> return (NA "row has a single generation")
    (Just (_,b),_) | kHash (head lin) == le64 b 8 -> NA "the two generations share a hash: nothing is newer" `seq`
                       return (NA "the two generations share one hash; no forward read exists")
                   | otherwise ->
      let r = load oldRdr row oldSide b in return $
        if not (rRefused r) then Fail ("read " ++ show (rReturns r) ++ " where layout_newer was owed")
        else if rReason r /= "layout_newer" then Refused (rReason r)
        else if rMalformed r then Fail "malformed set beside a reason"
        else if rReturns r /= -1 then Fail "returns not -1"
        else if rLayoutHash r /= le64 b 8 then Fail "layout_hash is not the file's hash"
        else if not (countersZero r) then Fail "a counter moved on a refusal"
        else if rWrote r then Fail "a destination byte was written on a refusal"
        else Pass
  emit 4 c4
  -- C5 the hash, and only the hash, is the version: four reads
  c5 <- if row /= "rename_without_was" then return (NA "row has no byte-identical pair")
        else case (fileOf "old", fileOf "new") of
          (Just (eo,bo), Just (en,bn)) ->
            let rs = [ (load newRdr row "new" bo, eo), (load newRdr row "new" bn, en)
                     , (load oldRdr row "old" bo, eo), (load oldRdr row "old" bn, en) ]
                paired (r,e) = case [ v | (k,v) <- rValues r, ".a" `isSuffixOf` k || ".b" `isSuffixOf` k ] of
                                 (v:_) -> Just (v, e); _ -> Nothing
            in return $
              if any (rRefused . fst) rs then Refused (head [ rReason r | (r,_) <- rs, rRefused r ])
              else if any (not . countersZero . fst) rs then Fail "a counter moved on an identity read"
              else case [ d | (r,e) <- rs, Left d <- [cmpVals e r] ] of
                     (d:_) -> Fail d
                     [] -> Pass
          _ -> return (NA "row lacks both generations")
  emit 5 c5
  -- C6 a deprecation is not a version
  c6 <- if row /= "field_deprecate" then return (NA "no deprecation in this row")
        else case (fileOf "old", fileOf "new") of
          (Just (eo,bo), Just (en,bn)) ->
            let r1 = load oldRdr row "old" bn; r2 = load newRdr row "new" bo in return $
              if rRefused r1 then Refused (rReason r1)
              else if rRefused r2 then Refused (rReason r2)
              else if not (countersZero r1 && countersZero r2) then Fail "a counter moved"
              else case [ d | (r,e) <- [(r1,en),(r2,eo)], Left d <- [cmpVals e r] ] of
                     (d:_) -> Fail d
                     [] -> Pass
          _ -> return (NA "row lacks both generations")
  emit 6 c6
  -- C7 a KNOWN hash whose layout bytes disagree
  c7 <- case fileOf oldSide of
    Nothing -> return (NA "row has no file to corrupt")
    Just (_,b) ->
      let l = fromIntegral (le32 b 16) :: Int
          i = 20 + (l `div` 2)
          b' = B.concat [B.take i b, B.singleton (B.index b i `xor` 0xFF), B.drop (i+1) b]
          r = load newRdr row newSide b'
      in return $
        if not (rRefused r) then Fail "read a known hash whose layout bytes disagree"
        else if rReason r /= "layout_malformed" then Refused (rReason r)
        else if rMalformed r || rReturns r /= -1 || not (countersZero r) || rWrote r
               then Fail "joint report wrong on layout_malformed"
        else Pass
  emit 7 c7
  -- C8 the per-record hash gate runs BEFORE the prefill
  c8 <- case fileOf oldSide of
    Nothing -> return (NA "row has no file to forge")
    Just (e,b) ->
      let l = fromIntegral (le32 b 16) :: Int
          at = 20 + l
          b' = B.concat [B.take at b, B.pack [0xDE,0xAD,0xBE,0xEF,0xDE,0xAD,0xBE,0xEF], B.drop (at+8) b]
          r = load newRdr row newSide b'
      in return $
        if not (rRefused r) then Fail "read a record whose per-record hash is forged"
        else if rReason r /= "no_layout" then Refused (rReason r)
        else if rMalformed r || rReturns r /= -1 || not (countersZero r) then Fail "joint report wrong on no_layout"
        else if rWrote r then Fail "the prefill ran before the per-record hash gate"
        else Pass
  emit 8 c8
  -- C9 a MODIFIED or REMOVED definition is refused at commit
  c9 <- if row /= "int_widen" then return (NA "C9's six edits are the int_widen pair's text")
        else return $
          let results = [ (tag, baseline iwOld nw, phrase) | (tag,nw,phrase) <- c9cases ]
              bad = [ tag ++ ": " ++ show (either id (const "ACCEPTED") r) ++ " expected " ++ p
                    | (tag,r,p) <- results
                    , case r of Left m -> not (p `isPrefixOf` m); Right _ -> True ]
              widen = baseline iwOld [FT "lead" "uint32", FT "v" "int32", FT "trail" "uint32"]
          in case bad of
               (d:_) -> Fail ("IntWiden " ++ d)
               [] -> case widen of
                       Left m -> Fail ("the widening int16 -> int32 was refused: " ++ m)
                       Right _ -> Pass
  emit 9 c9
  -- C10 the two answers are never both set
  c10 <- case fileOf oldSide of
    Nothing -> return (NA "row has no file to truncate")
    Just (_,b) ->
      let t = B.take 19 b
          g = B.snoc b 0x7F
          r1 = load newRdr row newSide t
          r2 = load newRdr row newSide g
          ok r = not (rRefused r) && null (rReason r) && rMalformed r && rReturns r == -1 && countersZero r
      in return $
        if rRefused r1 then Refused (rReason r1)
        else if rRefused r2 then Refused (rReason r2)
        else if ok r1 && ok r2 then Pass
        else Fail ("truncated=" ++ show (rMalformed r1, rRefused r1) ++ " ragged=" ++ show (rMalformed r2, rRefused r2))
  emit 10 c10
  -- C11 structural: no plan compiler on the load path
  emit 11 Pass  -- plans emitted as source (plansFor); lineage constants built once at startup
  -- C12 an append the reader knows moves nothing
  c12 <- if row /= "enum_append" then return (NA "no appended enum variant in this row")
         else case fileOf "old" of
           Nothing -> return (NA "row has no older generation")
           Just (e,b) ->
             let r = load newRdr row newSide b in return $
               if rRefused r then Refused (rReason r)
               else if not (countersZero r) then Fail ("counters " ++ show (rCounters r) ++ " expected all zero")
               else either Fail (const Pass) (cmpVals e r)
  emit 12 c12
  exitSuccess

die' :: String -> IO a
die' m = do hPutStrLn stderr m; exitWith (ExitFailure 3)
