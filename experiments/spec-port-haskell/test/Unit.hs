-- | Unit tests: pure functions only, no time, no sockets, no network, no
-- files outside the test's own strings.
module Main (main) where

import Control.Monad (unless)
import qualified Data.ByteString as B
import qualified Data.ByteString.Char8 as BC
import qualified Data.IntMap.Strict as IM
import Data.IORef
import qualified Data.Map.Strict as M
import FloatBits
import Json
import Schema
import Syntax (parseFile)
import System.Exit (exitFailure)
import Text.Printf (printf)
import Wire

schemaText :: String
schemaText =
  unlines
    [ "enum Color"
    , "{"
    , "    Red"
    , "    Green"
    , "    Blue"
    , "}"
    , "table Node"
    , "{"
    , "    value int32"
    , "    next  *Node"
    , "}"
    , "table Root"
    , "{"
    , "    name  string(16)"
    , "    hp    int16 = 7"
    , "    tags  []string"
    , "    color Color"
    , "    head  *Node"
    , "    big   uint64"
    , "    m     map[string]int32"
    , "}"
    ]

main :: IO ()
main = do
  failures <- newIORef (0 :: Int)
  let check name ok = unless ok $ do
        printf "FAIL %s\n" (name :: String)
        modifyIORef' failures (+ 1)
  -- ids (SPEC-TABLES.md §5)
  check "fnv1a64 head" (fnv1a64 "head" == 0x0a8f12cc5f9a0c03)
  -- W12's own examples
  check "float32 double-round witness" (fmtFloat32 0x15AE43FE == "7.0385313e-26")
  check "float32 least subnormal" (fmtFloat32 1 == "1.4013e-45")
  check "float64 least subnormal" (fmtFloat64 1 == "4.94065645841247e-324")
  check "float64 0.1+0.2" (fmtFloat64 0x3FD3333333333334 == "0.30000000000000004")
  check "float32 negative zero" (fmtFloat32 0x80000000 == "-0")
  check "float32 100" (fmap fmtFloat32 (ratToF32 100) == Just "100")
  -- the unit round trip through both forms
  case parseFile schemaText >>= checkUnit of
    Left e -> check ("schema: " ++ e) False
    Right u -> do
      let root = uRecs u M.! "Root"
      let doc =
            Doc
              (VRec [VBytes (BC.pack "ship"), VI 7, VArr [VBytes (BC.pack "a\"b")], VEnum 2, VPtr 1, VI 18446744073709551615, VMap [(VBytes (BC.pack "b"), VI 2), (VBytes (BC.pack "a"), VI (-1))]])
              (IM.fromList [(1, NRec "Node" (VRec [VI 5, VPtr 0]))])
      case save u root doc of
        Left e -> check ("save: " ++ e) False
        Right bs -> do
          check "form byte" (B.head bs == 1)
          case load u root bs of
            Left e -> check ("load: " ++ e) False
            Right d2 -> check "save . load . save" (save u root d2 == Right bs)
          case toJson u root doc of
            Left e -> check ("toJson: " ++ e) False
            Right t -> do
              check "text ends with one newline" (B.last t == 10 && B.index t (B.length t - 2) /= 10)
              check "wide integer as a string" (BC.pack "\"big\": \"18446744073709551615\"" `B.isInfixOf` t)
              check "map keys ascending" (BC.pack "\"a\": -1,\n    \"b\": 2" `B.isInfixOf` t)
              case fromJson u root t >>= save u root of
                Left e -> check ("fromJson: " ++ e) False
                Right bs2 -> check "text round trip" (bs2 == bs)
          -- one damaged byte at a time never crashes the reader
          let flips = [B.concat [B.take i bs, B.singleton (B.index bs i + 1), B.drop (i + 1) bs] | i <- [0 .. B.length bs - 1]]
          check "flips decide" (all (\b -> either (const True) (const True) (load u root b)) flips)
      -- text refusals (§16.2)
      let refuses t = either (const True) (const False) (fromJson u root (BC.pack t))
      check "repeated key" (refuses "{\"hp\": 1, \"hp\": 2}")
      check "data after root" (refuses "{} x")
      check "narrow int as string" (refuses "{\"hp\": \"1\"}")
      check "wide int as number" (refuses "{\"big\": 1}")
      check "int with fraction" (refuses "{\"hp\": 1.0}")
      check "minus zero int" (refuses "{\"hp\": -0}")
      check "string past bound" (refuses "{\"name\": \"12345678901234567\"}")
      check "zero escape" (refuses "{\"name\": \"a\\u0000\"}")
      check "lone surrogate" (refuses "{\"name\": \"\\ud800\"}")
      check "& key at root" (refuses "{\"&node\": 1, \"hp\": 2}")
      check "reference before definition" (refuses "{\"head\": {\"&node\": 1}}")
      let accepts t = either (const False) (const True) (fromJson u root (BC.pack t))
      check "trailing comma and comments" (accepts "\xEF\xBB\xBF{ // c\n \"hp\": 3, /* x */ }")
      check "unknown key skipped" (accepts "{\"zzz\": [1, {\"q\": null}], \"hp\": 3}")
  n <- readIORef failures
  if n == 0 then putStrLn "unit: all passed" else exitFailure
