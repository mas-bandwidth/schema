{-# LANGUAGE LambdaCase #-}

-- | The checked unit: the resolved declarations a reader and a writer walk.
-- docs/SPEC.md §4 (the language), docs/SPEC-TABLES.md §2 (tables), §5 (ids).
module Schema
  ( Ty (..)
  , Shape (..)
  , Field (..)
  , Rec (..)
  , EnumD (..)
  , FlagsD (..)
  , UnionD (..)
  , Arm' (..)
  , Unit (..)
  , Val (..)
  , fnv1a64
  , loadUnit
  , checkUnit
  , kindOf
  , elemKindOf
  , defaultOf
  , fieldDefault
  , recDefault
  , lookupRec
  , isTextListOpt
  , intRange
  , mapKeyId
  , mapValueId
  , blobId
  , nodeTableId
  ) where

import Control.Monad (foldM, forM, unless, when)
import Data.Bits (shiftL, xor, (.|.))
import qualified Data.ByteString as B
import qualified Data.ByteString.Char8 as BC
import Data.Char (ord)
import Data.List (elemIndex, isSuffixOf, sort)
import qualified Data.Map.Strict as M
import Data.Maybe (fromMaybe, isJust, mapMaybe)
import Data.Ratio (denominator, numerator)
import Data.Word (Word32, Word64)
import FloatBits (float32FromRational, float64FromRational, f32Bits, f64Bits)
import Syntax
import System.Directory (doesDirectoryExist, listDirectory)
import System.FilePath ((</>))

-- ---------------------------------------------------------------- types

data Shape = SFixedN Integer | SCount Integer Integer | SUnbounded
  deriving (Eq, Show)

data Ty
  = TBool
  | TInt Int Bool (Maybe (Integer, Integer)) -- width in bytes (1..16), signed, declared range
  | TBits Int
  | TFloat32 (Maybe (Rational, Rational, Rational)) -- compressed: min, max, resolution
  | TFloat64
  | TFixedPt Bool Int Int (Integer, Integer) -- signed, I, F, RAW range
  | TTime
  | TStr (Maybe Integer)
  | TBytes Integer
  | TEnum String
  | TFlags String
  | TRecT String
  | TUnionT String
  | TPtr String
  | TBlob Bool -- True = *string, False = *bytes
  | TArr Shape Ty
  | TKeyed String Ty
  | TMap Ty Ty
  | TOpt Ty
  deriving (Eq, Show)

-- | A value of a declared type. Graph edges are node ids (0 = null).
data Val
  = VBool Bool
  | VI Integer -- every integer kind, bits(N), fixed raw, timestamp nanoseconds
  | VF32 Word32
  | VF64 Word64
  | VBytes B.ByteString -- string(N), string, bytes(N)
  | VEnum Int -- 0 = None, k = the k-th declared variant
  | VFlags Word64
  | VRec [Val] -- one per field, declaration order
  | VArr [Val]
  | VKeyed [Val] -- one per declared variant, ordinal order
  | VMap [(Val, Val)] -- ascending key order
  | VUnion Int Val -- 0 = None; k = the k-th arm
  | VOpt (Maybe Val)
  | VPtr Int -- 0 = null; else a node of the document's graph
  | VUnit
  deriving (Eq, Show)

data Field = Field
  { fName :: String
  , fId :: Word64
  , fJson :: String
  , fTy :: Ty
  , fDecl :: Maybe Val -- a specified default
  , fGuards :: [(Int, Bool)] -- (index of the guarding bool field, value it must hold)
  , fRequired :: Bool
  , fTotal :: Maybe Integer -- a text list's `total = N` (SPEC-TABLES.md §2.13)
  }
  deriving (Show)

data Rec = Rec
  { rName :: String
  , rId :: Word64
  , rFixed :: Bool
  , rTable :: Bool
  , rFields :: [Field]
  , rIntegrity :: Bool
  , rJsonArray :: Bool
  }
  deriving (Show)

data EnumD = EnumD {eName :: String, eVariants :: [(String, Word64, Bool)]}
  deriving (Show)

data FlagsD = FlagsD {flName :: String, flVariants :: [String]}
  deriving (Show)

data Arm' = Arm' {aName :: String, aId :: Word64, aTy :: Maybe Ty}
  deriving (Show)

data UnionD = UnionD {uName :: String, uArms :: [Arm']}
  deriving (Show)

data Unit = Unit
  { uRecs :: M.Map String Rec
  , uEnums :: M.Map String EnumD
  , uFlags :: M.Map String FlagsD
  , uUnions :: M.Map String UnionD
  , uRecById :: M.Map Word64 String -- table type ids (SPEC-TABLES.md §3.1)
  }

lookupRec :: Unit -> String -> Rec
lookupRec u n = fromMaybe (error ("no table " ++ n)) (M.lookup n (uRecs u))

-- ---------------------------------------------------------------- ids

fnv1a64 :: String -> Word64
fnv1a64 = B.foldl' step 0xcbf29ce484222325 . BC.pack . encodeUtf8
  where
    step h b = (h `xor` fromIntegral b) * 0x100000001b3

encodeUtf8 :: String -> String
encodeUtf8 = concatMap enc
  where
    enc c
      | o < 0x80 = [c]
      | o < 0x800 = map toEnum [0xC0 .|. (o `div` 64), 0x80 .|. (o `mod` 64)]
      | o < 0x10000 = map toEnum [0xE0 .|. (o `div` 4096), 0x80 .|. ((o `div` 64) `mod` 64), 0x80 .|. (o `mod` 64)]
      | otherwise = map toEnum [0xF0 .|. (o `div` 262144), 0x80 .|. ((o `div` 4096) `mod` 64), 0x80 .|. ((o `div` 64) `mod` 64), 0x80 .|. (o `mod` 64)]
      where
        o = ord c

mapKeyId, mapValueId, nodeTableId :: Word64
mapKeyId = 0x3DC94A19365B10EC
mapValueId = 0x7CE4FD9430E80CEA
nodeTableId = 0xFFFFFFFFFFFFFFFF

blobId :: Bool -> Word64
blobId True = fnv1a64 "string"
blobId False = fnv1a64 "bytes"

-- ---------------------------------------------------------------- kinds

kindOf :: Unit -> Ty -> Int
kindOf u = \case
  TBool -> 1
  TInt w s _ -> intKind w s
  TBits n -> intKind (bitsWidth n) False
  TFloat32 _ -> 10
  TFloat64 -> 11
  TFixedPt s i f _ ->
    let w = (i + f) `div` 8
        base = if s then 20 else 25
     in base + widthIx w
  TTime -> 36
  TStr _ -> 12
  TBytes _ -> 14
  TEnum _ -> 30
  TFlags _ -> 9
  TRecT n -> if rJsonArray (lookupRec u n) then 13 else 13
  TUnionT _ -> 15
  TPtr _ -> 17
  TBlob _ -> 17
  TArr _ _ -> 14
  TKeyed _ _ -> 16
  TMap _ _ -> 14
  TOpt t -> kindOf u t

-- | The element kind byte an array or keyed body carries for elements of a type.
elemKindOf :: Unit -> Ty -> Int
elemKindOf u t
  | isTextListOpt t = 13
  | otherwise = kindOf u t

-- | `?string(M)` / `?string` as a text-list element: a body holding `value`.
isTextListOpt :: Ty -> Bool
isTextListOpt (TOpt (TStr _)) = True
isTextListOpt _ = False

intKind :: Int -> Bool -> Int
intKind w s = (if s then [2, 3, 4, 5, 18] else [6, 7, 8, 9, 19]) !! widthIx w

widthIx :: Int -> Int
widthIx w = case w of
  1 -> 0
  2 -> 1
  4 -> 2
  8 -> 3
  _ -> 4

bitsWidth :: Int -> Int
bitsWidth n
  | n <= 8 = 1
  | n <= 16 = 2
  | n <= 32 = 4
  | otherwise = 8

-- | The range a read holds an integer-shaped value to, and a writer refuses past.
intRange :: Ty -> Maybe (Integer, Integer)
intRange = \case
  TInt w s r -> Just (fromMaybe (storage w s) r)
  TBits n -> Just (0, 2 ^ n - 1)
  TFixedPt _ _ _ r -> Just r
  TTime -> Just (-(2 ^ (63 :: Int)), 2 ^ (63 :: Int) - 1)
  _ -> Nothing
  where
    storage w s =
      let b = 8 * w
       in if s then (-(2 ^ (b - 1)), 2 ^ (b - 1) - 1) else (0, 2 ^ b - 1)

-- ---------------------------------------------------------------- defaults

defaultOf :: Unit -> Ty -> Val
defaultOf u = \case
  TBool -> VBool False
  TInt {} -> VI 0
  TBits _ -> VI 0
  TFloat32 _ -> VF32 0
  TFloat64 -> VF64 0
  TFixedPt {} -> VI 0
  TTime -> VI 0
  TStr _ -> VBytes B.empty
  TBytes _ -> VBytes B.empty
  TEnum _ -> VEnum 0
  TFlags _ -> VFlags 0
  TRecT n -> recDefault u (lookupRec u n)
  TUnionT _ -> VUnion 0 VUnit
  TPtr _ -> VPtr 0
  TBlob _ -> VPtr 0
  TArr sh t -> case sh of
    SFixedN n -> VArr (replicate (fromIntegral n) (defaultOf u t))
    SCount a _ -> VArr (replicate (fromIntegral a) (defaultOf u t))
    SUnbounded -> VArr []
  TKeyed e t -> VKeyed (replicate (length (eVariants (uEnums u M.! e))) (defaultOf u t))
  TMap _ _ -> VMap []
  TOpt _ -> VOpt Nothing

fieldDefault :: Unit -> Field -> Val
fieldDefault u f = fromMaybe (defaultOf u (fTy f)) (fDecl f)

recDefault :: Unit -> Rec -> Val
recDefault u r = VRec (map (fieldDefault u) (rFields r))

-- ---------------------------------------------------------------- loading

-- SPEC-GAP 1: no checked IR is published for a port to take, so the unit is
-- parsed from its `.schema` text and checked here.
-- SPEC-GAP 5: a TABLE line also takes `integrity` and `json = "array"`.
-- | A unit is one or more `.schema` paths; a directory contributes every
-- `.schema` file directly inside it.
loadUnit :: [FilePath] -> IO (Either String Unit)
loadUnit paths = do
  files <- concat <$> mapM expand paths
  srcs <- mapM readFile files
  pure $ do
    ds <- concat <$> mapM (\(f, s) -> either (\e -> Left (f ++ ": " ++ e)) Right (parseFile s)) (zip files srcs)
    checkUnit ds
  where
    expand p = do
      d <- doesDirectoryExist p
      if d
        then map (p </>) . sort . filter (".schema" `isSuffixOf`) <$> listDirectory p
        else pure [p]

-- ---------------------------------------------------------------- checking

data CVal = CInt Integer | CFloat Rational Bool -- Bool: came from float64 arithmetic

checkUnit :: [Decl] -> Either String Unit
checkUnit ds = do
  let consts = M.fromList [(n, e) | DConst n _ e <- ds]
      enumAsts = M.fromList [(n, vs) | DEnum n _ vs <- ds]
      flagAsts = M.fromList [(n, (as, vs)) | DFlagsDecl n as vs <- ds]
      typeNames = M.fromList ([(n, False) | DType n _ _ <- ds] ++ [(n, True) | DTable _ n _ _ <- ds])
      unionNames = M.fromList [(n, ()) | DUnion n _ _ <- ds]
      env = Env consts enumAsts flagAsts typeNames unionNames
  enums <- fmap M.fromList . forM (M.toList enumAsts) $ \(n, vs) ->
    pure (n, EnumD n [(v, wireId v as, hasTag "deprecated" as) | Variant v as <- vs])
  let flags = M.fromList [(n, FlagsD n [v | Variant v _ <- vs]) | (n, (_, vs)) <- M.toList flagAsts]
  unions <- fmap M.fromList . forM [(n, as) | DUnion n _ as <- ds] $ \(n, arms) -> do
    as' <- forM arms $ \(Arm an mt attrs) -> do
      t <- traverse (\ta -> resolveType env enums ta attrs) mt
      pure (Arm' an (wireId an attrs) t)
    pure (n, UnionD n as')
  let unit0 = Unit M.empty enums flags unions M.empty
  recs <- fmap M.fromList . forM (recDecls ds) $ \(fixed, isTable, n, attrs, items) -> do
    fs0 <- flattenItems env enums unit0 items []
    fs <- resolveGuards n fs0
    pure
      ( n
      , Rec
          { rName = n
          , rId = wireId n attrs
          , rFixed = fixed
          , rTable = isTable
          , rFields = fs
          , rIntegrity = isJust (lookupAttr "integrity" attrs)
          , rJsonArray = lookupAttr "json" attrs == Just (Just (AVStr "array"))
          }
      )
  -- defaults need the whole unit (a nested record's default); resolve them now
  let unit1 = unit0 {uRecs = recs}
  recs' <- forM recs $ \r -> do
    fs <- forM (rFields r) $ \f -> pure f
    pure r {rFields = fs}
  let byId = M.fromList [(rId r, rName r) | r <- M.elems recs', rTable r]
  pure unit1 {uRecs = recs', uRecById = byId}
  where
    recDecls xs = [(False, False, n, as, b) | DType n as b <- xs] ++ [(fx, True, n, as, b) | DTable fx n as b <- xs]

data Env = Env
  { envConsts :: M.Map String Expr
  , envEnums :: M.Map String [Variant]
  , envFlags :: M.Map String ([Attr], [Variant])
  , envTypes :: M.Map String Bool
  , envUnions :: M.Map String ()
  }

hasTag :: String -> [Attr] -> Bool
hasTag t = any (\(Attr k v) -> k == t && v == Nothing)

lookupAttr :: String -> [Attr] -> Maybe (Maybe AttrVal)
lookupAttr k as = case [v | Attr k' v <- as, k' == k] of
  (v : _) -> Just v
  [] -> Nothing

wireId :: String -> [Attr] -> Word64
wireId n as = case lookupAttr "was" as of
  Just (Just (AVStr old)) -> fnv1a64 old
  _ -> fnv1a64 n

flattenItems :: Env -> M.Map String EnumD -> Unit -> [Item] -> [(String, Bool)] -> Either String [(Field, [(String, Bool)])]
flattenItems env enums unit0 items guards = go items []
  where
    go [] acc = Right (reverse acc)
    go (IOther : r) acc = go r acc
    go (IField fa : r) acc = do
      f <- mkField env enums unit0 fa
      go r ((f, guards) : acc)
    go (IIf neg g th el : r) acc = do
      let gs = guards ++ [(g, not neg)]
          gs' = guards ++ [(g, neg)]
      a <- flattenItems env enums unit0 th gs
      b <- flattenItems env enums unit0 el gs'
      go r (reverse b ++ reverse a ++ acc)

-- | A guard names a bool field of the same body.
resolveGuards :: String -> [(Field, [(String, Bool)])] -> Either String [Field]
resolveGuards rn fs = forM fs $ \(f, gs) -> do
  gs' <- forM gs $ \(g, v) -> case elemIndex g names of
    Just i -> Right (i, v)
    Nothing -> Left ("guard " ++ g ++ " names no field of " ++ rn)
  pure f {fGuards = gs'}
  where
    names = map (fName . fst) fs

-- Guards are resolved to field indices after flattening (see resolveGuards).
mkField :: Env -> M.Map String EnumD -> Unit -> FieldAst -> Either String Field
mkField env enums _ (FieldAst n ta md as) = do
  t <- resolveType env enums ta as
  d <- traverse (evalDefault env enums t) md
  tot <- case lookupAttr "total" as of
    Just (Just (AVExpr e)) -> Just <$> evalInt env e
    _ -> Right Nothing
  pure
    Field
      { fName = n
      , fId = wireId n as
      , fJson = case lookupAttr "json" as of
          Just (Just (AVStr k)) -> k
          _ -> n
      , fTy = t
      , fDecl = d
      , fGuards = []
      , fRequired = hasTag "required" as
      , fTotal = tot
      }

resolveType :: Env -> M.Map String EnumD -> TypeAst -> [Attr] -> Either String Ty
resolveType env enums ta attrs = go ta
  where
    go = \case
      TyOpt t -> TOpt <$> go t
      TyMap k v -> TMap <$> scalarTy k [] <*> go v
      TyArr b t -> do
        et <- go t
        case b of
          BNone -> pure (TArr SUnbounded et)
          BUpTo e -> TArr . SCount 0 <$> evalInt env e <*> pure et
          BRange a c -> TArr <$> (SCount <$> evalInt env a <*> evalInt env c) <*> pure et
          BExact (EName e) | M.member e enums -> pure (TKeyed e et)
          BExact e -> TArr . SFixedN <$> evalInt env e <*> pure et
      TyScalar s -> scalarTy s attrs
    scalarTy s as = case s of
      SPtr "bytes" -> pure (TBlob False)
      SPtr "string" -> pure (TBlob True)
      SPtr n -> pure (TPtr n)
      SBits e -> TBits . fromIntegral <$> evalInt env e
      SString Nothing -> pure (TStr Nothing)
      SString (Just e) -> TStr . Just <$> evalInt env e
      SBytes e -> TBytes <$> evalInt env e
      SFixed sg ie fe -> do
        i <- fromIntegral <$> evalInt env ie
        f <- fromIntegral <$> evalInt env fe
        lo <- attrInt "min" as
        hi <- attrInt "max" as
        let raw x = x * 2 ^ f
        pure (TFixedPt sg i f (raw (fromMaybe 0 lo), raw (fromMaybe 0 hi)))
      SName n -> case n of
        "bool" -> pure TBool
        "float64" -> pure TFloat64
        "float32" -> do
          lo <- attrRat "min" as
          hi <- attrRat "max" as
          re <- attrRat "resolution" as
          pure (TFloat32 ((,,) <$> lo <*> hi <*> re))
        "timestamp" -> pure TTime
        "uint128" -> pure (TInt 16 False Nothing)
        "int128" -> intT 16 True
        _ | Just (w, sg) <- lookup n intNames -> intT w sg
        _ | M.member n (envEnums env) -> pure (TEnum n)
        _ | M.member n (envFlags env) -> pure (TFlags n)
        _ | M.member n (envUnions env) -> pure (TUnionT n)
        _ | M.member n (envTypes env) -> pure (TRecT n)
        _ -> Left ("unknown type " ++ n)
      where
        intT w sg = do
          lo <- attrInt "min" as
          hi <- attrInt "max" as
          let b = 8 * w
              smin = if sg then -(2 ^ (b - 1)) else 0
              smax = if sg then 2 ^ (b - 1) - 1 else 2 ^ b - 1
          pure $ case (lo, hi) of
            (Nothing, Nothing) -> TInt w sg Nothing
            -- one-sided ranges: SPEC-TABLES.md §4.3
            _ -> TInt w sg (Just (fromMaybe smin lo, fromMaybe smax hi))
    attrInt k as = case lookupAttr k as of
      Just (Just (AVExpr e)) -> Just <$> evalInt env e
      _ -> pure Nothing
    attrRat k as = case lookupAttr k as of
      Just (Just (AVExpr e)) -> Just . toRat <$> evalC env e
      _ -> pure Nothing
    _ = enums

intNames :: [(String, (Int, Bool))]
intNames =
  [ ("int8", (1, True)), ("int16", (2, True)), ("int32", (4, True)), ("int64", (8, True))
  , ("uint8", (1, False)), ("uint16", (2, False)), ("uint32", (4, False)), ("uint64", (8, False))
  ]

toRat :: CVal -> Rational
toRat (CInt i) = fromIntegral i
toRat (CFloat r _) = r

evalInt :: Env -> Expr -> Either String Integer
evalInt env e = evalC env e >>= \case
  CInt i -> Right i
  CFloat _ _ -> Left "float where an integer is required"

-- | Constant evaluation. Integers are exact; a float expression is exact
-- until an operator joins two values (SPEC-GAP 4: SPEC.md §4.2 says float64
-- arithmetic; a lone literal is kept exact so a default rounds once).
evalC :: Env -> Expr -> Either String CVal
evalC env = go (0 :: Int)
  where
    go d ex | d > 64 = Left "constant reference cycle"
    go d ex = case ex of
      EInt i -> Right (CInt i)
      EFloat s -> Right (CFloat (readRat s) False)
      EName n -> case M.lookup n (envConsts env) of
        Just e -> go (d + 1) e
        Nothing -> Left ("unknown constant " ++ n)
      EDot n "Max" -> case M.lookup n (envEnums env) of
        Just vs -> Right (CInt (fromIntegral (length vs)))
        Nothing -> Left ("unknown enum " ++ n)
      EDot n "Count" -> case M.lookup n (envFlags env) of
        Just (_, vs) -> Right (CInt (fromIntegral (length vs)))
        Nothing -> Left ("unknown flags " ++ n)
      EDot n m -> Left ("unknown reference " ++ n ++ "." ++ m)
      ENeg a -> go d a >>= \case
        CInt i -> Right (CInt (negate i))
        CFloat r b -> Right (CFloat (negate r) b)
      EBin op a b -> do
        x <- go d a
        y <- go d b
        case (x, y) of
          (CInt i, CInt j) -> case op of
            '+' -> Right (CInt (i + j))
            '-' -> Right (CInt (i - j))
            '*' -> Right (CInt (i * j))
            '/' -> if j == 0 then Left "division by zero" else Right (CInt (i `quot` j))
            '%' -> if j == 0 then Left "division by zero" else Right (CInt (i `rem` j))
            _ -> Left "bad operator"
          _ ->
            let p = toDouble (toRat x)
                q = toDouble (toRat y)
                r = case op of
                  '+' -> Right (p + q)
                  '-' -> Right (p - q)
                  '*' -> Right (p * q)
                  '/' -> Right (p / q)
                  _ -> Left "no % on floats"
             in (\v -> CFloat (toRational v) True) <$> r
    toDouble :: Rational -> Double
    toDouble = fromRational

readRat :: String -> Rational
readRat s =
  let (mant, ex) = break (`elem` "eE") s
      (ip, fp) = break (== '.') mant
      fd = drop 1 fp
      digits = read (if null (ip ++ fd) then "0" else ip ++ fd) :: Integer
      e = (case ex of
             (_ : '+' : r) -> read r
             (_ : r@(_ : _)) -> read r
             _ -> 0) - length fd
   in if e >= 0 then fromIntegral (digits * 10 ^ e) else fromIntegral digits / fromIntegral (10 ^ negate e :: Integer)

evalDefault :: Env -> M.Map String EnumD -> Ty -> DefaultAst -> Either String Val
evalDefault env enums t d = case (t, d) of
  (TStr _, DStr s) -> Right (VBytes (BC.pack (encodeUtf8 s)))
  (TBytes _, DStr s) -> Right (VBytes (BC.pack (encodeUtf8 s)))
  (TFlags fl, DFlags ns) -> do
    let (_, vs) = envFlags env M.! fl
        names = [v | Variant v _ <- vs]
    bits <- forM ns $ \n -> maybe (Left ("unknown flag " ++ n)) (Right . (shiftL 1 :: Int -> Word64)) (elemIndex n names)
    Right (VFlags (foldl (.|.) 0 bits))
  (TBool, DExpr (EName "true")) -> Right (VBool True)
  (TBool, DExpr (EName "false")) -> Right (VBool False)
  (TEnum en, DExpr (EName v)) -> case elemIndex v [n | (n, _, _) <- eVariants (enums M.! en)] of
    Just i -> Right (VEnum (i + 1))
    Nothing -> Left ("unknown variant " ++ v)
  (TFloat32 _, DExpr e) -> VF32 . f32Bits . float32FromRational . toRat <$> evalC env e
  (TFloat64, DExpr e) -> VF64 . f64Bits . float64FromRational . toRat <$> evalC env e
  (TFixedPt _ _ f _, DExpr e) -> do
    r <- toRat <$> evalC env e
    let raw = r * 2 ^ f
    unless (denominator raw == 1) (Left "fixed default not representable")
    Right (VI (numerator raw))
  (TOpt _, _) -> Left "an optional takes no default"
  (_, DExpr e) -> VI <$> evalInt env e
  _ -> Left "bad default"
