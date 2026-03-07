#!/usr/bin/env pwsh
# run_locomo_paper_aligned_lite.ps1 - Windows PowerShell port of run_locomo_paper_aligned_lite.sh
# Mirrors the bash script exactly; replaces pkill -> taskkill, python3 -> python, curl -> Invoke-WebRequest.
param(
    [string]$LocomoJson     = "research/data/locomo10.json",
    [string]$OutDir         = "research/results/paperlite",
    [int]   $TopK           = 60,
    [int]   $MaxQueries     = 150,
    [string]$EmbedModel     = "all-minilm",
    [string]$AnswerMode     = "hybrid",
    [string]$AnswerModel    = "qwen2.5:7b",
    [int]   $AnswerTopDocs  = 4,
    [int]   $AnswerTimeout  = 45,
    [int]   $AnswerMaxTokens = 96,
    [double]$AnswerTemperature = 0.0,
    [int]   $RetrievalQueryVariants = 3,
    [double]$RetrievalRrfK = 60.0,
    [int]   $ContextNeighborWindow = 0,
    [int]   $ContextMaxItems = 12,
    [int]   $EvidenceMaxLines        = 10,
    [int]   $StructuredMaxObs        = 4,
    [double]$ExtractiveThreshold     = 0.60,
    [string]$ParserProvider          = "heuristic",
    [string]$ParserOllamaModel       = "qwen2.5:7b",
    [int]   $ParserOllamaTimeoutMs   = 20000,
    [int]   $StoreBatchSize          = 64,
    [int]   $StoreBatchTimeoutSecs   = 90,
    [int]   $StoreSingleTimeoutSecs  = 45,
    [switch]$ParserStoreRaw          = $true,
    [int]   $ParserMaxFacts          = 5,
    [double]$ParserDedupeThreshold   = 0.88,
    [double]$ParserUpdateThreshold   = 0.94,
    [string]$CacheDb        = "research/cache/paperlite_structured_ollama.sqlite",
    [string]$CacheIndexMap  = "research/cache/paperlite_structured_ollama_idx_map.json",
    [switch]$ReuseCache,
    [switch]$ResetCache,
    [int]   $NumConvs       = 0,
    [int]   $OllamaPort     = 18086,
    [int]   $LexicalPort    = 18087,
    [switch]$NoKillStale,
    [int]   $ServerStartTimeout = 300,
    [string]$OllamaUrl      = "http://127.0.0.1:11434"
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

# Resolve paths relative to repo root
$RepoRoot = (Get-Item "$PSScriptRoot\..").FullName
Set-Location $RepoRoot

# Inject PATH for this session
foreach ($p in @(
    "C:\Program Files\Go\bin",
    "C:\Users\sugam\AppData\Local\Programs\Ollama",
    "C:\Users\sugam\AppData\Local\Programs\Python\Python312",
    "C:\Users\sugam\AppData\Local\Programs\Python\Python312\Scripts"
)) {
    if ($env:PATH -notlike "*$p*") { $env:PATH = "$p;$env:PATH" }
}
# jq installs to WindowsApps alias dir but the exe may be in a winget path
$jqCandidates = @(
    "$env:LOCALAPPDATA\Microsoft\WinGet\Packages\jqlang.jq_Microsoft.Winget.Source_8wekyb3d8bbwe\jq-windows-amd64.exe",
    "C:\ProgramData\scoop\apps\jq\current\jq.exe"
)
foreach ($jqPath in $jqCandidates) {
    if (Test-Path $jqPath) {
        $dir = Split-Path $jqPath
        if ($env:PATH -notlike "*$dir*") { $env:PATH = "$dir;$env:PATH" }
        break
    }
}

if ($ReuseCache -and $ResetCache) {
    Write-Error "ERROR: -ReuseCache and -ResetCache are mutually exclusive"; exit 1
}

# Auto-tune for ollama parser (heavy)
if ($ParserProvider -eq "ollama") {
    if ($StoreBatchSize -eq 64)         { $StoreBatchSize = 8 }
    if ($StoreBatchTimeoutSecs -eq 90)  { $StoreBatchTimeoutSecs = 600 }
    if ($StoreSingleTimeoutSecs -eq 45) { $StoreSingleTimeoutSecs = 120 }
}

# Kill stale pali servers
if (-not $NoKillStale) {
    Get-Process -ErrorAction SilentlyContinue | Where-Object {
        $_.MainWindowTitle -like "*pali*" -or
        ($_.Name -like "pali*" -and $_.Name -ne "pali.exe.not")
    } | Stop-Process -Force -ErrorAction SilentlyContinue

    # taskkill by command line fragment
    $procs = Get-CimInstance Win32_Process -Filter "CommandLine LIKE '%cmd/pali%'" -ErrorAction SilentlyContinue
    foreach ($p in $procs) {
        Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
        Write-Host "killed stale pali pid=$($p.ProcessId)"
    }
    Start-Sleep -Milliseconds 500
}

# Create directories
foreach ($d in @(
    (Split-Path $LocomoJson),
    $OutDir,
    (Split-Path $CacheDb),
    (Split-Path $CacheIndexMap)
)) {
    if ($d) { New-Item -ItemType Directory -Force -Path $d | Out-Null }
}

# Download locomo10.json if missing
if (-not (Test-Path $LocomoJson)) {
    Write-Host "==> locomo10.json not found - downloading from snap-research/locomo..."
    $url = "https://raw.githubusercontent.com/snap-research/locomo/main/data/locomo10.json"
    Invoke-WebRequest -Uri $url -OutFile $LocomoJson -UseBasicParsing
    Write-Host "    saved to $LocomoJson"
}

# Derive fixture/eval/stats paths (mini mode)
$FixtureOut = "research/data/locomo10.paperlite.fixture.json"
$EvalOut    = "research/data/locomo10.paperlite.eval.json"
$StatsOut   = "research/data/locomo10.paperlite.stats.json"
$PrepExtraArgs = @()
if ($NumConvs -gt 0) {
    $FixtureOut = "research/data/locomo10.paperlite.mini${NumConvs}.fixture.json"
    $EvalOut    = "research/data/locomo10.paperlite.mini${NumConvs}.eval.json"
    $StatsOut   = "research/data/locomo10.paperlite.mini${NumConvs}.stats.json"
    $PrepExtraArgs = @("--max-conversations", "$NumConvs")
    Write-Host "==> [mini] Using first $NumConvs conversations"
}

# Convert LOCOMO -> fixture/eval
Write-Host "==> Converting LOCOMO to paperlite fixture/eval"
& python research/prepare_locomo_eval.py `
    --locomo-json $LocomoJson `
    --fixture-out $FixtureOut `
    --eval-out    $EvalOut `
    --stats-out   $StatsOut `
    --mode        paperlite `
    --sanitize-percent `
    @PrepExtraArgs
if ($LASTEXITCODE -ne 0) { Write-Error "prepare_locomo_eval.py failed"; exit 1 }

$RunId  = (Get-Date -Format "yyyyMMddTHHmmssZ")
$RunDir = "$OutDir/$RunId"
New-Item -ItemType Directory -Force -Path $RunDir | Out-Null

$OllamaJson   = "$RunDir/ollama.json"
$OllamaTxt    = "$RunDir/ollama.summary.txt"
$LexicalJson  = "$RunDir/lexical.json"
$LexicalTxt   = "$RunDir/lexical.summary.txt"
$CompareJson  = "$RunDir/comparison.json"
$OllamaTrace  = "$RunDir/ollama.trace.jsonl"
$LexicalTrace = "$RunDir/lexical.trace.jsonl"

# Cache flags
$OllamaCacheFlags = @("--db-path", $CacheDb, "--index-map-path", $CacheIndexMap)
if ($ReuseCache) {
    $OllamaCacheFlags += "--reuse-existing-store"
} else {
    $OllamaCacheFlags += "--reset-db"
}
if ($ResetCache) {
    foreach ($f in @($CacheDb, "$CacheDb-shm", "$CacheDb-wal", $CacheIndexMap)) {
        if (Test-Path $f) { Remove-Item $f -Force; Write-Host "reset-cache: removed $f" }
    }
}

# Parser flags
$ParserFlags = @(
    "--parser-enabled",
    "--parser-provider",          $ParserProvider,
    "--parser-max-facts",         "$ParserMaxFacts",
    "--parser-dedupe-threshold",  "$ParserDedupeThreshold",
    "--parser-update-threshold",  "$ParserUpdateThreshold",
    "--parser-ollama-url",        $OllamaUrl,
    "--parser-ollama-model",      $ParserOllamaModel,
    "--parser-ollama-timeout-ms", "$ParserOllamaTimeoutMs"
)
if ($ParserStoreRaw) { $ParserFlags += "--parser-store-raw-turn" }
else                 { $ParserFlags += "--no-parser-store-raw-turn" }

Write-Host "==> Store ingest settings"
Write-Host "    batch size    : $StoreBatchSize"
Write-Host "    batch timeout : ${StoreBatchTimeoutSecs}s"
Write-Host "    single timeout: ${StoreSingleTimeoutSecs}s"
Write-Host "    ollama port   : $OllamaPort"
Write-Host "    lexical port  : $LexicalPort"
Write-Host "    q variants    : $RetrievalQueryVariants"
Write-Host "    rrf k         : $RetrievalRrfK"
Write-Host "    neighbor win  : $ContextNeighborWindow"
Write-Host "    context max   : $ContextMaxItems"

# Shared eval flags (no embedding-provider yet)
$CommonEvalFlags = @(
    "--fixture",                    $FixtureOut,
    "--eval-set",                   $EvalOut,
    "--embedding-model",            $EmbedModel,
    "--ollama-url",                 $OllamaUrl,
    "--top-k",                      "$TopK",
    "--max-queries",                "$MaxQueries",
    "--host",                       "127.0.0.1",
    "--server-start-timeout-seconds", "$ServerStartTimeout",
    "--answer-mode",                $AnswerMode,
    "--answer-model",               $AnswerModel,
    "--answer-top-docs",            "$AnswerTopDocs",
    "--answer-ollama-url",          $OllamaUrl,
    "--answer-timeout-seconds",     "$AnswerTimeout",
    "--answer-max-tokens",          "$AnswerMaxTokens",
    "--answer-temperature",         "$AnswerTemperature",
    "--extractive-confidence-threshold", "$ExtractiveThreshold",
    "--prefer-extractive-for-temporal",
    "--retrieval-query-variants",   "$RetrievalQueryVariants",
    "--retrieval-rrf-k",            "$RetrievalRrfK",
    "--retrieval-kind-routing",
    "--context-neighbor-window",    "$ContextNeighborWindow",
    "--context-max-items",          "$ContextMaxItems",
    "--evidence-max-lines",         "$EvidenceMaxLines",
    "--structured-memory-enabled",
    "--structured-query-routing-enabled",
    "--structured-max-observations","$StructuredMaxObs",
    "--store-batch-size",           "$StoreBatchSize",
    "--store-batch-timeout-seconds","$StoreBatchTimeoutSecs",
    "--store-single-timeout-seconds","$StoreSingleTimeoutSecs"
)

# --- Ollama run ---
Write-Host ""
Write-Host "==> Running answer-mode=$AnswerMode (embedding-provider=ollama, parser-provider=$ParserProvider)"
& python research/eval_locomo_f1_bleu.py `
    @CommonEvalFlags `
    --embedding-provider ollama `
    --port               "$OllamaPort" `
    @ParserFlags `
    --trace-jsonl        $OllamaTrace `
    @OllamaCacheFlags `
    --out-json           $OllamaJson `
    --out-summary        $OllamaTxt
if ($LASTEXITCODE -ne 0) { Write-Error "ollama eval failed"; exit 1 }

# --- Lexical run ---
Write-Host ""
Write-Host "==> Running answer-mode=$AnswerMode (embedding-provider=lexical, parser-provider=$ParserProvider)"
& python research/eval_locomo_f1_bleu.py `
    @CommonEvalFlags `
    --embedding-provider lexical `
    --port               "$LexicalPort" `
    @ParserFlags `
    --trace-jsonl        $LexicalTrace `
    --out-json           $LexicalJson `
    --out-summary        $LexicalTxt
if ($LASTEXITCODE -ne 0) { Write-Error "lexical eval failed"; exit 1 }

# --- Comparison via jq (or fallback to Python) ---
$_jqCmd = Get-Command jq -ErrorAction SilentlyContinue
$jqExe = if ($_jqCmd) { $_jqCmd.Source } else { $null }
if ($jqExe) {
    $jqFilter = '{
  ollama: {
    f1_generated: $o[0].qa_metrics.f1_generated,
    bleu1_generated: $o[0].qa_metrics.bleu1_generated,
    f1_generated_paper_scale: $o[0].qa_metrics_paper_scale.f1_generated,
    bleu1_generated_paper_scale: $o[0].qa_metrics_paper_scale.bleu1_generated,
    retrieval_recall_at_k: $o[0].retrieval_metrics.recall_at_k,
    retrieval_ndcg_at_k: $o[0].retrieval_metrics.ndcg_at_k,
    retrieval_mrr: $o[0].retrieval_metrics.mrr,
    em_generated_normalized: $o[0].qa_metrics_companion.em_generated_normalized,
    top1_unique_rate: $o[0].retrieval_diagnostics.top1_unique_rate
  },
  lexical: {
    f1_generated: $l[0].qa_metrics.f1_generated,
    bleu1_generated: $l[0].qa_metrics.bleu1_generated,
    f1_generated_paper_scale: $l[0].qa_metrics_paper_scale.f1_generated,
    bleu1_generated_paper_scale: $l[0].qa_metrics_paper_scale.bleu1_generated,
    retrieval_recall_at_k: $l[0].retrieval_metrics.recall_at_k,
    retrieval_ndcg_at_k: $l[0].retrieval_metrics.ndcg_at_k,
    retrieval_mrr: $l[0].retrieval_metrics.mrr,
    em_generated_normalized: $l[0].qa_metrics_companion.em_generated_normalized,
    top1_unique_rate: $l[0].retrieval_diagnostics.top1_unique_rate
  },
  delta_ollama_minus_lexical: {
    f1_generated: ($o[0].qa_metrics.f1_generated - $l[0].qa_metrics.f1_generated),
    bleu1_generated: ($o[0].qa_metrics.bleu1_generated - $l[0].qa_metrics.bleu1_generated),
    f1_generated_paper_scale: ($o[0].qa_metrics_paper_scale.f1_generated - $l[0].qa_metrics_paper_scale.f1_generated),
    bleu1_generated_paper_scale: ($o[0].qa_metrics_paper_scale.bleu1_generated - $l[0].qa_metrics_paper_scale.bleu1_generated),
    retrieval_recall_at_k: ($o[0].retrieval_metrics.recall_at_k - $l[0].retrieval_metrics.recall_at_k),
    retrieval_ndcg_at_k: ($o[0].retrieval_metrics.ndcg_at_k - $l[0].retrieval_metrics.ndcg_at_k),
    retrieval_mrr: ($o[0].retrieval_metrics.mrr - $l[0].retrieval_metrics.mrr),
    em_generated_normalized: ($o[0].qa_metrics_companion.em_generated_normalized - $l[0].qa_metrics_companion.em_generated_normalized),
    top1_unique_rate: ($o[0].retrieval_diagnostics.top1_unique_rate - $l[0].retrieval_diagnostics.top1_unique_rate)
  }
}'
    & $jqExe -n --slurpfile o $OllamaJson --slurpfile l $LexicalJson $jqFilter | Out-File -Encoding utf8 $CompareJson
} else {
    # Pure-Python fallback comparison
    $pyCompare = @"
import json, sys
o = json.load(open(sys.argv[1]))
l = json.load(open(sys.argv[2]))
fields = [
    ('qa_metrics','f1_generated'),
    ('qa_metrics','bleu1_generated'),
    ('qa_metrics_paper_scale','f1_generated'),
    ('qa_metrics_paper_scale','bleu1_generated'),
    ('retrieval_metrics','recall_at_k'),
    ('retrieval_metrics','ndcg_at_k'),
    ('retrieval_metrics','mrr'),
    ('qa_metrics_companion','em_generated_normalized'),
    ('retrieval_diagnostics','top1_unique_rate'),
]
def pick(d, section, key):
    return (d.get(section) or {}).get(key)
def flat(d):
    return dict((key, pick(d, sec, key)) for sec, key in fields)
fo, fl = flat(o), flat(l)
delta = dict((k, (fo[k] - fl[k]) if fo[k] is not None and fl[k] is not None else None) for k in fo)
print(json.dumps({'ollama': fo, 'lexical': fl, 'delta_ollama_minus_lexical': delta}, indent=2))
"@
    & python -c $pyCompare $OllamaJson $LexicalJson | Out-File -Encoding utf8 $CompareJson
}

Write-Host ""
Write-Host "==> Paper-aligned-lite run complete"
Write-Host "    run dir      : $RunDir"
Write-Host "    ollama json  : $OllamaJson"
Write-Host "    ollama trace : $OllamaTrace"
Write-Host "    lexical json : $LexicalJson"
Write-Host "    lexical trace: $LexicalTrace"
Write-Host "    cache db     : $CacheDb"
Write-Host "    cache idxmap : $CacheIndexMap"
Write-Host "    comparison   : $CompareJson"

# Print comparison summary
if (Test-Path $CompareJson) {
    Write-Host ""
    Write-Host "==> Comparison (ollama vs lexical):"
    Get-Content $CompareJson
}

