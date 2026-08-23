# dmsh Windows Build Script
# Run with: .\build.ps1

$ErrorActionPreference = "Stop"

$ProjectRoot = $PSScriptRoot
if (-not $ProjectRoot) {
    $ProjectRoot = Get-Location
}

$CMake = "${env:ProgramFiles}\CMake\bin\cmake.exe"
$MinGWBin = "C:\ProgramData\mingw64\mingw64\bin"

Write-Host "=== dmsh Windows Build ===" -ForegroundColor Cyan

# 1. Init submodule if needed
Write-Host "[1/4] Checking submodule..." -ForegroundColor Yellow
if (-not (Test-Path "$ProjectRoot\third_party\llama.cpp\CMakeLists.txt")) {
    git submodule update --init --recursive
}

# 2. Build llama.cpp
Write-Host "[2/4] Building llama.cpp..." -ForegroundColor Yellow
if (-not (Test-Path "$ProjectRoot\third_party\llama.cpp\build")) {
    New-Item -ItemType Directory -Path "$ProjectRoot\third_party\llama.cpp\build" -Force | Out-Null
}

# MinGW GCC emits aligned vmovaps to 16-byte-aligned stack slots at -O3,
# crashing the AVX K-quant kernels (0xC0000005). -O2 avoids that codegen.
& $CMake -G "MinGW Makefiles" -S "$ProjectRoot\third_party\llama.cpp" -B "$ProjectRoot\third_party\llama.cpp\build" `
    -DBUILD_SHARED_LIBS=OFF `
    -DLLAMA_BUILD_TESTS=OFF `
    -DLLAMA_BUILD_EXAMPLES=OFF `
    -DLLAMA_BUILD_SERVER=OFF `
    -DCMAKE_BUILD_TYPE=Release `
    -DCMAKE_C_FLAGS_RELEASE="-O2 -DNDEBUG" `
    -DCMAKE_CXX_FLAGS_RELEASE="-O2 -DNDEBUG"

& $CMake --build "$ProjectRoot\third_party\llama.cpp\build" --config Release -j

# 3. Build dmsh
Write-Host "[3/4] Building dmsh..." -ForegroundColor Yellow
$Version = git describe --always --dirty 2>$null
if (-not $Version) { $Version = "dev" }

go build -tags llama `
    -ldflags "-s -w -X github.com/dedomorozoff/dmsh/internal/cli.Version=$Version" `
    -o "$ProjectRoot\bin\dmsh.exe" `
    "$ProjectRoot\cmd\dmsh"

# 4. Copy MinGW DLLs
Write-Host "[4/4] Copying MinGW DLLs..." -ForegroundColor Yellow
$DLLs = @(
    "libstdc++-6.dll",
    "libgcc_s_seh-1.dll",
    "libgomp-1.dll",
    "libwinpthread-1.dll"
)

foreach ($dll in $DLLs) {
    $src = "$MinGWBin\$dll"
    if (Test-Path $src) {
        Copy-Item $src "$ProjectRoot\bin\" -Force
        Write-Host "  Copied $dll" -ForegroundColor Green
    } else {
        Write-Host "  WARNING: $dll not found" -ForegroundColor Red
    }
}

Write-Host ""
Write-Host "=== Build complete ===" -ForegroundColor Green
Write-Host "Executable: $ProjectRoot\bin\dmsh.exe"
Write-Host ""
Write-Host "Usage: bin\dmsh.exe ask ""list files"" --model <model.gguf>"
Write-Host "       bin\dmsh.exe --help"
