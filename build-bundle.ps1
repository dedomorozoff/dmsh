# nlsh Windows Bundle Build Script
# Downloads model and prepares files for offline installer
# Run with: .\build-bundle.ps1

$ErrorActionPreference = "Stop"

$ProjectRoot = $PSScriptRoot
if (-not $ProjectRoot) {
    $ProjectRoot = Get-Location
}

Write-Host "=== nlsh Windows Bundle Build ===" -ForegroundColor Cyan

# Model to bundle (recommended model)
$ModelName = "Qwopus3.5-9B-coder-Exp-Q3_K_S.gguf"
$ModelURL = "https://huggingface.co/Jackrong/Qwopus3.5-9B-Coder-GGUF/resolve/main/Qwopus3.5-9B-coder-Exp-Q3_K_S.gguf"
$BundleDir = "$ProjectRoot\bundle"

# 1. Build nlsh.exe if not exists
if (-not (Test-Path "$ProjectRoot\bin\nlsh.exe")) {
    Write-Host "[1/3] Building nlsh.exe..." -ForegroundColor Yellow
    if (Test-Path "$ProjectRoot\third_party\llama.cpp\build") {
        & "$ProjectRoot\build.ps1"
    } else {
        Write-Host "  llama.cpp not built. Building stub version..." -ForegroundColor Yellow
        go build -ldflags "-s -w" -o "$ProjectRoot\bin\nlsh.exe" "$ProjectRoot\cmd\nlsh"
    }
} else {
    Write-Host "[1/3] nlsh.exe already exists" -ForegroundColor Green
}

# 2. Download model if not exists
if (-not (Test-Path "$BundleDir\$ModelName")) {
    Write-Host "[2/3] Downloading model: $ModelName (~4.3 GB)..." -ForegroundColor Yellow
    New-Item -ItemType Directory -Path $BundleDir -Force | Out-Null

    $req = [System.Net.HttpWebRequest]::Create($ModelURL)
    $req.AllowAutoRedirect = $true
    $req.Timeout = 600000
    $req.ReadWriteTimeout = 600000

    $resp = $req.GetResponse()
    $stream = $resp.GetResponseStream()

    $fileStream = [System.IO.File]::Create("$BundleDir\$ModelName")
    $buffer = New-Object byte[] 65536
    $totalRead = 0
    $totalLength = $resp.ContentLength

    while ($true) {
        $read = $stream.Read($buffer, 0, $buffer.Length)
        if ($read -le 0) { break }
        $fileStream.Write($buffer, 0, $read)
        $totalRead += $read
        $pct = [math]::Round(($totalRead / $totalLength) * 100)
        Write-Host "`r  Downloaded: $([math]::Round($totalRead/1MB)) MB / $([math]::Round($totalLength/1MB)) MB ($pct%)" -NoNewline
    }

    $fileStream.Close()
    $stream.Close()
    $resp.Close()
    Write-Host ""
    Write-Host "  Model downloaded successfully" -ForegroundColor Green
} else {
    Write-Host "[2/3] Model already exists in bundle/" -ForegroundColor Green
}

# 3. Copy MinGW DLLs to bundle
Write-Host "[3/3] Preparing bundle directory..." -ForegroundColor Yellow
$MinGWBin = "C:\ProgramData\mingw64\mingw64\bin"
$DLLs = @(
    "libstdc++-6.dll",
    "libgcc_s_seh-1.dll",
    "libgomp-1.dll",
    "libwinpthread-1.dll"
)

foreach ($dll in $DLLs) {
    $src = "$MinGWBin\$dll"
    if (Test-Path $src) {
        Copy-Item $src "$BundleDir\" -Force
        Write-Host "  Copied $dll" -ForegroundColor Green
    } else {
        Write-Host "  WARNING: $dll not found" -ForegroundColor Red
    }
}

# Copy nlsh.exe
Copy-Item "$ProjectRoot\bin\nlsh.exe" "$BundleDir\" -Force
Write-Host "  Copied nlsh.exe" -ForegroundColor Green

Write-Host ""
Write-Host "=== Bundle ready ===" -ForegroundColor Green
Write-Host "Directory: $BundleDir"
Write-Host "Run: iscc installer-bundle.iss"
