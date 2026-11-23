-- | The conformance driver (test/conformance/README.md, "The driver contract"):
--   driver <manifest> list
--   driver <manifest> <surface> <outdir>
-- Surfaces: wire, json-read, json-write. A FIXED root (form 3, out of this
-- port's scope) is answered `<case>.absent`.
module Main (main) where

import Control.Exception (SomeException, evaluate, try)
import Control.Monad (forM_, unless, when)
import qualified Data.ByteString as B
import qualified Data.ByteString.Char8 as BC
import Data.IORef
import qualified Data.Map.Strict as M
import Json
import Schema
import System.Directory (createDirectoryIfMissing, doesFileExist)
import System.Environment (getArgs)
import System.Exit (exitWith, ExitCode (..))
import System.FilePath ((</>))
import System.IO (hPutStrLn, stderr)
import Wire

surfaces :: [String]
surfaces = ["wire", "json-read", "json-write", "report", "framing", "json-hostile"]

data Inst = Inst {iName, iUnit, iRoot, iWire :: String, iNoText, iRefused :: Bool}

data JWR = JWR {jCase, jUnit, jRoot, jWire, jDamage :: String}

main :: IO ()
main = do
  args <- getArgs
  case args of
    [_, "list"] -> mapM_ putStrLn surfaces
    [m, s, out] | s `elem` surfaces -> run m s out
    [_, _, _] -> exitWith (ExitFailure 2)
    _ -> hPutStrLn stderr "usage: driver <manifest> list | <manifest> <surface> <outdir>" >> exitWith (ExitFailure 1)

run :: FilePath -> String -> FilePath -> IO ()
run manifest surface out = do
  createDirectoryIfMissing True out
  ls <- map words . lines <$> readFile manifest
  let units = M.fromList [(k, ps) | ("unit" : k : ps) <- ls]
      insts = [Inst n un r w ("no-text" `elem` fl) ("wire-refused" `elem` fl) | ("instance" : n : un : r : w : fl) <- ls]
      jwrs = [JWR c un r w dmg | ("json-write-refusal" : c : un : r : w : dmg : _) <- ls]
  cache <- newIORef M.empty
  let getUnit k = do
        c <- readIORef cache
        case M.lookup k c of
          Just x -> pure x
          Nothing -> do
            x <- loadUnit (M.findWithDefault [] k units)
            either (\e -> hPutStrLn stderr ("unit " ++ k ++ ": " ++ e)) (const (pure ())) x
            modifyIORef' cache (M.insert k x)
            pure x
      absent name = B.writeFile (out </> (name ++ ".absent")) B.empty
      refused path = B.writeFile path (BC.pack "refused\n")
  when (surface `elem` ["report", "framing", "json-hostile"]) $
    forM_ [(c, un, r, src) | (k : c : un : r : src : _) <- ls, k == surface] $ \(c, un, rn, src) -> do
      eu <- getUnit un
      case eu of
        Right u | Just root <- M.lookup rn (uRecs u), not (rFixed root) -> do
          r <- safely $ do
            input <- if surface == "json-hostile" then B.readFile (src </> (rn ++ ".json")) else B.readFile src
            let doc = if surface == "json-hostile" then fromJson u root input else load u root input
            pure (c, doc >>= \d -> (,) <$> toJson u root d <*> save u root d >>= \(t, w) -> Right (B.concat [BC.pack "read\n", t, BC.pack "\0", w]))
          case r of
            Right (_, Right bs) -> do
              let (t, w) = B.break (== 0) bs
              B.writeFile (out </> c) t
              B.writeFile (out </> (c ++ ".wire")) (B.drop 1 w)
            Right (_, Left _) -> refused (out </> c)
            Left e -> hPutStrLn stderr (c ++ ": driver failure: " ++ e) >> absent c
        _ -> absent c
  when (surface `elem` ["wire", "json-read", "json-write"]) $
   forM_ insts $ \i -> do
    let textSurface = surface /= "wire"
    unless (textSurface && iNoText i) $ do
      eu <- getUnit (iUnit i)
      case eu of
        Left _ -> absent (caseOut i)
        Right u -> case M.lookup (iRoot i) (uRecs u) of
          Nothing -> absent (caseOut i)
          Just root
            | rFixed root -> absent (caseOut i) -- form 3: out of scope
            | otherwise -> do
                r <- safely (answer u root i)
                case r of
                  Left e -> hPutStrLn stderr (iName i ++ ": driver failure: " ++ e) >> absent (caseOut i)
                  Right (path, Left why) -> do
                    when (not (iRefused i)) (hPutStrLn stderr (iName i ++ ": refused: " ++ why))
                    -- SPEC-GAP 12: README says a wire-refused case may emit `refused` or
                    -- `<case>.absent`; the harness reads `refused` and `refused\n` there as
                    -- value bytes, so a wire refusal is said with the marker.
                    if surface == "wire" then absent (caseOut i) else refused (out </> path)
                  Right (path, Right bs) -> B.writeFile (out </> path) bs
  when (surface == "json-write") $
    forM_ jwrs $ \j -> do
      eu <- getUnit (jUnit j)
      case eu of
        Left _ -> absent (jCase j ++ ".json")
        Right u -> case M.lookup (jRoot j) (uRecs u) of
          Just root | not (rFixed root) -> do
            wire <- B.readFile (jWire j)
            r <- safely (pure (jCase j, load u root wire >>= damage u root (jDamage j) >>= toJson u root))
            case r of
              Right (_, Right bs) -> B.writeFile (out </> (jCase j ++ ".json")) bs
              Right (_, Left _) -> refused (out </> (jCase j ++ ".json"))
              Left e -> hPutStrLn stderr (jCase j ++ ": driver failure: " ++ e) >> absent (jCase j ++ ".json")
          _ -> absent (jCase j ++ ".json")
  where
    -- SPEC-GAP 13: `<case>.absent` sits "beside where the answer would go";
    -- json-write's answer is `<name>.json`, so its marker is `<name>.json.absent`.
    caseOut i = if surface == "json-write" then iName i ++ ".json" else iName i
    answer u root i = case surface of
      "wire" -> do
        bs <- B.readFile (iWire i)
        pure (iName i, load u root bs >>= save u root)
      "json-read" -> do
        let p = "testdata/conformance/tables/json" </> (iName i ++ ".json")
        ok <- doesFileExist p
        if not ok
          then pure (iName i, Left "no text")
          else do
            t <- B.readFile p
            pure (iName i, fromJson u root t >>= save u root)
      _ -> do
        bs <- B.readFile (iWire i)
        pure (iName i ++ ".json", load u root bs >>= toJson u root)

safely :: IO (String, Either String B.ByteString) -> IO (Either String (String, Either String B.ByteString))
safely act = do
  r <- try (act >>= \(p, x) -> evaluate (forceE x) >> pure (p, x))
  pure (either (\e -> Left (show (e :: SomeException))) Right r)
  where
    forceE (Left s) = length s `seq` ()
    forceE (Right b) = B.length b `seq` ()

-- | json-write-refusal's damage, applied in code (FORMAT.md): `narrow:<f>`
-- sets a top-level text field to 61 FF 62; `wide:<f>` to this leg's nearest
-- value that is not text, the unpaired surrogate U+D800 as its three
-- (ill-formed) UTF-8 bytes ED A0 80 between 'a' and 'b'.
damage :: Unit -> Rec -> String -> Doc -> Either String Doc
damage _ root dmg doc = case break (== ':') dmg of
  ("none", _) -> Right doc
  (k, ':' : fname) | k == "narrow" || k == "wide" -> do
    let bytes = if k == "narrow" then B.pack [0x61, 0xFF, 0x62] else B.pack [0x61, 0xED, 0xA0, 0x80, 0x62]
    case docRoot doc of
      VRec vs -> do
        let vs' = [if fName f == fname then setText bytes v else v | (f, v) <- zip (rFields root) vs]
        Right doc {docRoot = VRec vs'}
      _ -> Left "not a record"
  _ -> Left ("unknown damage " ++ dmg)
  where
    setText b (VOpt (Just _)) = VOpt (Just (VBytes b))
    setText b _ = VBytes b
