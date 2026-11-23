-- | The schema language: lexer and parser (docs/SPEC.md §4.1, §4.2).
--
-- Written from the spec alone. Doc comments (`///`) are read as ordinary
-- comments: they move no byte on any wire this port carries (SPEC.md §4.1,
-- "A doc comment moves nothing").
module Syntax
  ( Expr (..)
  , Attr (..)
  , AttrVal (..)
  , TypeAst (..)
  , BoundAst (..)
  , ScalarAst (..)
  , Item (..)
  , FieldAst (..)
  , Variant (..)
  , Arm (..)
  , Decl (..)
  , DefaultAst (..)
  , parseFile
  ) where

import Data.Char (isAlpha, isAlphaNum, isDigit, isHexDigit, isSpace)
import Numeric (readHex)

-- ---------------------------------------------------------------- lexer

data Tok
  = TIdent String
  | TInt Integer
  | TFloat String          -- the literal text, kept exact
  | TStr String
  | TPunct String
  | TNL
  | TEOF
  deriving (Eq, Show)

lexSchema :: String -> Either String [Tok]
lexSchema = go
  where
    go [] = Right [TNL, TEOF]
    go s@(c : cs)
      | c == '\n' = (TNL :) <$> go cs
      | c == '\r' = go cs
      | isSpace c = go cs
      | take 2 s == "//" = go (dropWhile (/= '\n') s)
      | take 2 s == "/*" = blockComment (drop 2 s)
      | isAlpha c || c == '_' =
          let (w, rest) = span (\x -> isAlphaNum x || x == '_') s
           in (TIdent w :) <$> go rest
      | isDigit c = number s
      | c == '"' =
          let (body, rest) = break (== '"') cs
           in case rest of
                ('"' : r) -> (TStr body :) <$> go r
                _ -> Left "unterminated string literal"
      | take 2 s == ".." = (TPunct ".." :) <$> go (drop 2 s)
      | take 2 s == "::" = (TPunct "::" :) <$> go (drop 2 s)
      | c `elem` "{}()[],:=!?.|+-*/%" = (TPunct [c] :) <$> go cs
      | otherwise = Left ("unexpected character " ++ show c)
    blockComment s = case s of
      [] -> Left "unterminated block comment"
      ('*' : '/' : r) -> go r
      ('\n' : r) -> blockComment r -- a block comment swallows its newline
      (_ : r) -> blockComment r
    number s
      | take 2 s == "0x" || take 2 s == "0X" =
          let (h, rest) = span isHexDigit (drop 2 s)
           in case readHex h of
                [(v, "")] -> (TInt v :) <$> go rest
                _ -> Left "bad hex literal"
      | take 2 s == "0b" || take 2 s == "0B" =
          let (b, rest) = span (`elem` "01") (drop 2 s)
           in (TInt (foldl (\a d -> a * 2 + (if d == '1' then 1 else 0)) 0 b) :) <$> go rest
      | otherwise =
          let (ds, rest) = span isDigit s
              (frac, rest2) = case rest of
                ('.' : r@(d : _)) | isDigit d -> let (f, r') = span isDigit r in ('.' : f, r')
                ('.' : r) | take 1 r /= "." -> (".", r)
                _ -> ("", rest)
              (ex, rest3) = case rest2 of
                (e : r) | e `elem` "eE" ->
                  let (sgn, r1) = case r of
                        (x : y) | x `elem` "+-" -> ([x], y)
                        _ -> ("", r)
                      (ed, r2) = span isDigit r1
                   in if null ed then ("", rest2) else (e : sgn ++ ed, r2)
                _ -> ("", rest2)
           in if length ds > 1 && head ds == '0'
                then Left "a decimal number never starts with 0 followed by a digit"
                else
                  if null frac && null ex
                    then (TInt (read ds) :) <$> go rest3
                    else (TFloat (ds ++ frac ++ ex) :) <$> go rest3

-- ---------------------------------------------------------------- AST

data Expr
  = EInt Integer
  | EFloat String
  | EName String
  | EDot String String      -- E.Max, F.Count
  | ENeg Expr
  | EBin Char Expr Expr
  deriving (Eq, Show)

data AttrVal = AVExpr Expr | AVStr String | AVQual [String]
  deriving (Eq, Show)

data Attr = Attr String (Maybe AttrVal)
  deriving (Eq, Show)

data BoundAst
  = BExact Expr
  | BUpTo Expr
  | BRange Expr Expr
  | BNone                   -- `[]`, unbounded (SPEC-TABLES.md §2.9)
  deriving (Eq, Show)

data ScalarAst
  = SName String
  | SBits Expr
  | SString (Maybe Expr)
  | SBytes Expr
  | SFixed Bool Expr Expr
  | SPtr String             -- *T, *bytes, *string
  deriving (Eq, Show)

data TypeAst
  = TyOpt TypeAst
  | TyArr BoundAst TypeAst
  | TyMap ScalarAst TypeAst
  | TyScalar ScalarAst
  deriving (Eq, Show)

data DefaultAst
  = DExpr Expr
  | DStr String
  | DFlags [String]
  deriving (Eq, Show)

data FieldAst = FieldAst
  { faName :: String
  , faType :: TypeAst
  , faDefault :: Maybe DefaultAst
  , faAttrs :: [Attr]
  }
  deriving (Eq, Show)

data Item
  = IField FieldAst
  | IIf Bool String [Item] [Item]   -- negated, guard, then, else
  | IOther                          -- const( ), reserved( ), align: packet-only
  deriving (Eq, Show)

data Variant = Variant String [Attr]
  deriving (Eq, Show)

data Arm = Arm String (Maybe TypeAst) [Attr]
  deriving (Eq, Show)

data Decl
  = DPackage String
  | DConst String (Maybe String) Expr
  | DEnum String [Attr] [Variant]
  | DFlagsDecl String [Attr] [Variant]
  | DType String [Attr] [Item]
  | DTable Bool String [Attr] [Item]   -- fixed?, name
  | DUnion String [Attr] [Arm]
  deriving (Eq, Show)

-- ---------------------------------------------------------------- parser

newtype P a = P {runP :: [Tok] -> Either String (a, [Tok])}

instance Functor P where
  fmap f (P p) = P $ \ts -> case p ts of
    Left e -> Left e
    Right (a, r) -> Right (f a, r)

instance Applicative P where
  pure a = P $ \ts -> Right (a, ts)
  P pf <*> P pa = P $ \ts -> case pf ts of
    Left e -> Left e
    Right (f, r) -> case pa r of
      Left e -> Left e
      Right (a, r') -> Right (f a, r')

instance Monad P where
  P p >>= f = P $ \ts -> case p ts of
    Left e -> Left e
    Right (a, r) -> runP (f a) r

failP :: String -> P a
failP e = P $ \ts -> Left (e ++ " near " ++ show (take 6 ts))

peek :: P Tok
peek = P $ \ts -> case ts of
  (t : _) -> Right (t, ts)
  [] -> Right (TEOF, ts)

peek2 :: P (Tok, Tok)
peek2 = P $ \ts -> case ts of
  (a : b : _) -> Right ((a, b), ts)
  [a] -> Right ((a, TEOF), ts)
  [] -> Right ((TEOF, TEOF), ts)

advance :: P Tok
advance = P $ \ts -> case ts of
  (t : r) -> Right (t, r)
  [] -> Right (TEOF, [])

punct :: String -> P ()
punct p = do
  t <- advance
  if t == TPunct p then pure () else failP ("expected " ++ p)

isPunct :: String -> Tok -> Bool
isPunct p t = t == TPunct p

ident :: P String
ident = do
  t <- advance
  case t of
    TIdent s -> pure s
    _ -> failP "expected identifier"

skipNL :: P ()
skipNL = do
  t <- peek
  if t == TNL then advance >> skipNL else pure ()

-- | After an item: a newline, or a closing brace left in place.
endItem :: P ()
endItem = do
  t <- peek
  case t of
    TNL -> skipNL
    TPunct "}" -> pure ()
    TEOF -> pure ()
    _ -> failP "expected end of line"

-- SPEC-GAP 3: beyond SPEC.md §4's grammar this also takes `[]T`, a bare
-- `string`, `*bytes` and `*string`, `[]?string(M)` and `fixed table`, as
-- SPEC-TABLES.md §2.2, §2.5, §2.9 and §2.13 use them.
parseFile :: String -> Either String [Decl]
parseFile src = do
  toks <- lexSchema src
  fst <$> runP decls toks
  where
    decls = do
      skipNL
      t <- peek
      case t of
        TEOF -> pure []
        _ -> (:) <$> decl <*> decls

decl :: P Decl
decl = do
  t <- advance
  case t of
    TIdent "package" -> DPackage <$> ident <* endItem
    TIdent "const" -> do
      n <- ident
      ty <- do
        t' <- peek
        case t' of
          TIdent w -> advance >> pure (Just w)
          _ -> pure Nothing
      punct "="
      skipNL
      e <- expr
      _ <- optAttrs
      endItem
      pure (DConst n ty e)
    TIdent "enum" -> do
      n <- ident
      as <- optAttrs
      vs <- variantList
      endItem
      pure (DEnum n as vs)
    TIdent "flags" -> do
      n <- ident
      as <- optAttrs
      vs <- variantList
      endItem
      pure (DFlagsDecl n as vs)
    TIdent "type" -> do
      n <- ident
      as <- optAttrs
      b <- block
      endItem
      pure (DType n as b)
    TIdent "fixed" -> do
      t' <- advance
      if t' /= TIdent "table" then failP "expected table after fixed" else pure ()
      n <- ident
      as <- optAttrs
      b <- block
      endItem
      pure (DTable True n as b)
    TIdent "table" -> do
      n <- ident
      as <- optAttrs
      b <- block
      endItem
      pure (DTable False n as b)
    TIdent "union" -> do
      n <- ident
      as <- optAttrs
      skipNL
      punct "{"
      arms <- armList
      endItem
      pure (DUnion n as arms)
    _ -> failP "expected a declaration"

-- | An optional qualification section; it runs to the end of the line.
-- SPEC-GAP 2: a declaration's `{` on the qualification's own line
-- (Features.schema) is accepted as ending the section.
optAttrs :: P [Attr]
optAttrs = do
  t <- peek
  if isPunct "|" t then advance >> attrList else pure []

attrList :: P [Attr]
attrList = do
  a <- attr
  t <- peek
  if isPunct "," t then advance >> (a :) <$> attrList else pure [a]

attr :: P Attr
attr = do
  k <- ident
  t <- peek
  if isPunct "=" t
    then do
      _ <- advance
      v <- attrVal
      pure (Attr k (Just v))
    else pure (Attr k Nothing)

attrVal :: P AttrVal
attrVal = do
  t <- peek
  case t of
    TStr s -> advance >> pure (AVStr s)
    TIdent _ -> do
      (_, t2) <- peek2
      if isPunct "::" t2
        then AVQual <$> qual
        else AVExpr <$> expr
    _ -> AVExpr <$> expr
  where
    qual = do
      i <- ident
      t <- peek
      if isPunct "::" t then advance >> (i :) <$> qual else pure [i]

variantList :: P [Variant]
variantList = do
  skipNL
  punct "{"
  go
  where
    go = do
      skipSeps
      t <- peek
      case t of
        TPunct "}" -> advance >> pure []
        _ -> do
          n <- ident
          as <- optAttrs
          (Variant n as :) <$> go
    skipSeps = do
      t <- peek
      if t == TNL || isPunct "," t then advance >> skipSeps else pure ()

armList :: P [Arm]
armList = do
  skipNL
  t <- peek
  case t of
    TPunct "}" -> advance >> pure []
    _ -> do
      n <- ident
      t' <- peek
      ty <-
        if t' == TNL || isPunct "|" t' || isPunct "}" t'
          then pure Nothing
          else Just <$> typeAst
      as <- optAttrs
      endItem
      (Arm n ty as :) <$> armList

block :: P [Item]
block = do
  skipNL
  punct "{"
  items

items :: P [Item]
items = do
  skipNL
  t <- peek
  case t of
    TPunct "}" -> advance >> pure []
    TIdent "if" -> do
      _ <- advance
      neg <- do
        t' <- peek
        if isPunct "!" t' then advance >> pure True else pure False
      g <- ident
      th <- block
      el <- do
        save <- P $ \ts -> Right (ts, ts)
        skipNL
        t' <- peek
        if t' == TIdent "else"
          then advance >> block
          else P (\_ -> Right ((), save)) >> pure []
      endItem
      (IIf neg g th el :) <$> items
    TIdent "const" -> do
      (_, t2) <- peek2
      if isPunct "(" t2 then skipLine >> (IOther :) <$> items else field
    TIdent "reserved" -> do
      (_, t2) <- peek2
      if isPunct "(" t2 then skipLine >> (IOther :) <$> items else field
    TIdent "align" -> do
      (_, t2) <- peek2
      if t2 == TNL || isPunct "}" t2 then advance >> endItem >> (IOther :) <$> items else field
    _ -> field
  where
    field = do
      f <- fieldAst
      (IField f :) <$> items
    skipLine = do
      t <- peek
      case t of
        TNL -> skipNL
        TPunct "}" -> pure ()
        TEOF -> pure ()
        _ -> advance >> skipLine

fieldAst :: P FieldAst
fieldAst = do
  n <- ident
  ty <- typeAst
  t <- peek
  d <-
    if isPunct "=" t
      then advance >> skipNL >> Just <$> defaultAst
      else pure Nothing
  as <- optAttrs
  endItem
  pure (FieldAst n ty d as)

defaultAst :: P DefaultAst
defaultAst = do
  t <- peek
  case t of
    TStr s -> advance >> pure (DStr s)
    TPunct "{" -> do
      _ <- advance
      let go = do
            skipNL
            t' <- peek
            case t' of
              TPunct "}" -> advance >> pure []
              TPunct "," -> advance >> go
              _ -> (:) <$> ident <*> go
      DFlags <$> go
    _ -> DExpr <$> expr

typeAst :: P TypeAst
typeAst = do
  t <- peek
  case t of
    TPunct "?" -> advance >> TyOpt <$> typeAst
    TPunct "[" -> do
      _ <- advance
      b <- bound
      punct "]"
      TyArr b <$> typeAst
    TIdent "map" -> do
      (_, t2) <- peek2
      if isPunct "[" t2
        then do
          _ <- advance
          punct "["
          k <- scalar
          punct "]"
          TyMap k <$> typeAst
        else TyScalar <$> scalar
    _ -> TyScalar <$> scalar

bound :: P BoundAst
bound = do
  t <- peek
  case t of
    TPunct "]" -> pure BNone
    TPunct ".." -> advance >> BUpTo <$> expr
    _ -> do
      a <- expr
      t' <- peek
      if isPunct ".." t' then advance >> BRange a <$> expr else pure (BExact a)

scalar :: P ScalarAst
scalar = do
  t <- advance
  case t of
    TPunct "*" -> SPtr <$> ident
    TIdent "bits" -> SBits <$> paren1
    TIdent "bytes" -> SBytes <$> paren1
    TIdent "string" -> do
      t' <- peek
      if isPunct "(" t' then SString . Just <$> paren1 else pure (SString Nothing)
    TIdent "fixed" -> uncurry (SFixed True) <$> paren2
    TIdent "ufixed" -> uncurry (SFixed False) <$> paren2
    TIdent n -> pure (SName n)
    _ -> failP "expected a type"
  where
    paren1 = punct "(" *> expr <* punct ")"
    paren2 = do
      punct "("
      a <- expr
      punct ","
      b <- expr
      punct ")"
      pure (a, b)

-- expressions: + - (lowest), * / %, unary -, atoms
expr :: P Expr
expr = term >>= rest
  where
    rest l = do
      t <- peek
      case t of
        TPunct [c] | c `elem` "+-" -> advance >> skipNL >> term >>= rest . EBin c l
        _ -> pure l

term :: P Expr
term = unary >>= rest
  where
    rest l = do
      t <- peek
      case t of
        TPunct [c] | c `elem` "*/%" -> advance >> skipNL >> unary >>= rest . EBin c l
        _ -> pure l

unary :: P Expr
unary = do
  t <- peek
  case t of
    TPunct "-" -> advance >> ENeg <$> unary
    TPunct "(" -> do
      _ <- advance
      e <- expr
      punct ")"
      pure e
    TInt i -> advance >> pure (EInt i)
    TFloat f -> advance >> pure (EFloat f)
    TIdent n -> do
      _ <- advance
      t' <- peek
      if isPunct "." t'
        then do
          _ <- advance
          m <- ident
          pure (EDot n m)
        else pure (EName n)
    _ -> failP "expected an expression"
