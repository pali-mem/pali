#!/usr/bin/env pwsh
param(
    [ValidateSet("lite", "main", "promotion")]
    [string]$Profile = "main",
    [int]$Runs = 0,
    [string]$OutDir = "research/results/paperlite_locked",
    [switch]$ReuseCache,
    [switch]$ResetCache
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

$RepoRoot = (Get-Item "$PSScriptRoot\..").FullName
Set-Location $RepoRoot

if ($ReuseCache -and $ResetCache) {
    throw "ReuseCache and ResetCache are mutually exclusive."
}

$numConvs = 10
$maxQueries = 120
$runsResolved = $Runs

switch ($Profile) {
    "lite" {
        $numConvs = 3
        $maxQueries = 40
        if ($runsResolved -le 0) { $runsResolved = 1 }
    }
    "main" {
        if ($runsResolved -le 0) { $runsResolved = 1 }
    }
    "promotion" {
        if ($runsResolved -le 0) { $runsResolved = 3 }
    }
}

New-Item -ItemType Directory -Force -Path $OutDir | Out-Null

Write-Host "==> Locked profile: $Profile"
Write-Host "    runs          : $runsResolved"
Write-Host "    num_convs     : $numConvs"
Write-Host "    max_queries   : $maxQueries"
Write-Host "    out_dir       : $OutDir"

$capturedRuns = @()
for ($i = 1; $i -le $runsResolved; $i++) {
    Write-Host ""
    Write-Host "==> Locked run $i/$runsResolved"
    $runArgs = @{
        OutDir = $OutDir
        TopK = 60
        MaxQueries = $maxQueries
        NumConvs = $numConvs
        AnswerMode = "hybrid"
        AnswerModel = "qwen2.5:7b"
        AnswerTopDocs = 8
        AnswerTimeout = 45
        AnswerMaxTokens = 96
        AnswerTemperature = 0.0
        RetrievalQueryVariants = 3
        RetrievalRrfK = 60.0
        ContextNeighborWindow = 1
        ContextMaxItems = 24
        EvidenceMaxLines = 10
        StructuredMaxObs = 4
        ExtractiveThreshold = 0.42
        ParserProvider = "heuristic"
        ParserMaxFacts = 5
        ParserDedupeThreshold = 0.88
        ParserUpdateThreshold = 0.94
        ParserStoreRaw = $true
        StoreBatchSize = 64
        StoreBatchTimeoutSecs = 90
        StoreSingleTimeoutSecs = 45
    }
    if ($ReuseCache) {
        $runArgs["ReuseCache"] = $true
    } elseif ($ResetCache) {
        $runArgs["ResetCache"] = $true
    }

    & "$PSScriptRoot/run_locomo_paper_aligned_lite.ps1" @runArgs
    if ($LASTEXITCODE -ne 0) {
        throw "Locked profile run failed at iteration $i."
    }

    $latest = Get-ChildItem -Path $OutDir -Directory | Sort-Object LastWriteTimeUtc -Descending | Select-Object -First 1
    if (-not $latest) {
        throw "Could not find run output directory in $OutDir."
    }
    $ollamaJson = Join-Path $latest.FullName "ollama.json"
    $lexicalJson = Join-Path $latest.FullName "lexical.json"
    if (-not (Test-Path $ollamaJson) -or -not (Test-Path $lexicalJson)) {
        throw "Missing run artifacts under $($latest.FullName)."
    }

    $ollama = Get-Content $ollamaJson -Raw | ConvertFrom-Json
    $lexical = Get-Content $lexicalJson -Raw | ConvertFrom-Json
    $capturedRuns += [PSCustomObject]@{
        run_dir = $latest.FullName
        ollama_f1_paper = [double]$ollama.qa_metrics_paper_scale.f1_generated
        lexical_f1_paper = [double]$lexical.qa_metrics_paper_scale.f1_generated
        ollama_recall = [double]$ollama.retrieval_metrics.recall_at_k
        lexical_recall = [double]$lexical.retrieval_metrics.recall_at_k
    }
}

function Get-Median([double[]]$values) {
    $arr = @($values)
    if ($arr.Count -eq 0) { return 0.0 }
    $sorted = $arr | Sort-Object
    $n = $sorted.Count
    if ($n % 2 -eq 1) { return [double]$sorted[[int]($n / 2)] }
    $left = [double]$sorted[[int]($n / 2) - 1]
    $right = [double]$sorted[[int]($n / 2)]
    return ($left + $right) / 2.0
}

$summary = [PSCustomObject]@{
    profile = $Profile
    runs = $runsResolved
    created_utc = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
    run_dirs = $capturedRuns.run_dir
    metrics = [PSCustomObject]@{
        ollama_f1_paper_median = Get-Median ([double[]]@($capturedRuns.ollama_f1_paper))
        lexical_f1_paper_median = Get-Median ([double[]]@($capturedRuns.lexical_f1_paper))
        ollama_recall_median = Get-Median ([double[]]@($capturedRuns.ollama_recall))
        lexical_recall_median = Get-Median ([double[]]@($capturedRuns.lexical_recall))
    }
    runs_detail = $capturedRuns
}

$summaryPath = Join-Path $OutDir ("locked_profile_{0}_{1}.summary.json" -f $Profile, (Get-Date -Format "yyyyMMddTHHmmssZ"))
$summary | ConvertTo-Json -Depth 8 | Out-File -Encoding utf8 $summaryPath

Write-Host ""
Write-Host "==> Locked profile complete"
Write-Host "    summary: $summaryPath"
Write-Host "    ollama_f1_paper_median : $($summary.metrics.ollama_f1_paper_median)"
Write-Host "    lexical_f1_paper_median: $($summary.metrics.lexical_f1_paper_median)"
