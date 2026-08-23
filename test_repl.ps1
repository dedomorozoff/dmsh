# Базовые тесты dmsh
# Запускать из корня проекта: .\test_repl.ps1

$ErrorActionPreference = "Continue"
$dmsh = "bin\dmsh.exe"
$passed = 0
$failed = 0

function Test-Command {
    param($desc, $input, $shouldContain)
    Write-Host "Test: $desc" -ForegroundColor Cyan
    Write-Host "  Input: $input"
    $out = echo $input | & $dmsh 2>&1
    if ($out -match $shouldContain) {
        Write-Host "  [PASS]" -ForegroundColor Green
        $script:passed++
    } else {
        Write-Host "  [FAIL]" -ForegroundColor Red
        Write-Host "  Expected to contain: $shouldContain"
        Write-Host "  Got: $out"
        $script:failed++
    }
}

Write-Host "=== dmsh Basic Tests ===" -ForegroundColor Yellow

Test-Command "Help command" "/help" "dmsh"

Write-Host ""
Write-Host "Results: $passed passed, $failed failed" -ForegroundColor $(if ($failed -eq 0) { "Green" } else { "Red" })