# install.ps1 - Скрипт установки dmsh для Windows с автоматической загрузкой модели

$ErrorActionPreference = "Stop"

Write-Host "=== dmsh (Direct Model Shell) Installer ===" -ForegroundColor Cyan

# 1. Сборка бинарного файла с поддержкой llama.cpp (требуется GCC / CGO)
Write-Host "`n[1/4] Сборка dmsh.exe..." -ForegroundColor Yellow
if (Get-Command "go" -ErrorAction SilentlyContinue) {
    # Пытаемся собрать с CGO и llama.cpp, если доступен GCC
    try {
        Write-Host "Запуск сборки с тегом llama..." -ForegroundColor Gray
        go build -tags llama -o bin/dmsh.exe ./cmd/dmsh
        Write-Host "Успешно собрано с поддержкой локального инференса (llama.cpp)!" -ForegroundColor Green
    } catch {
        Write-Warning "Не удалось собрать с тегом llama (возможно, не установлен gcc/mingw)."
        Write-Host "Сборка в режиме-заглушке (stub)..." -ForegroundColor Yellow
        go build -o bin/dmsh.exe ./cmd/dmsh
        Write-Host "Собрано в stub-режиме." -ForegroundColor Green
    }
} else {
    Write-Error "Утилита 'go' не найдена в PATH. Пожалуйста, установите Go перед запуском установщика."
}

# 2. Копирование бинарного файла в директорию установки
$InstallDir = Join-Path $HOME "AppData\Local\Programs\dmsh"
Write-Host "`n[2/4] Копирование файлов в $InstallDir..." -ForegroundColor Yellow
if (!(Test-Path $InstallDir)) {
    New-Item -ItemType Directory -Path $InstallDir | Out-Null
}
Copy-Item "bin\dmsh.exe" -Destination (Join-Path $InstallDir "dmsh.exe") -Force
Write-Host "Файлы скопированы!" -ForegroundColor Green

# 3. Добавление директории установки в PATH пользователя
Write-Host "`n[3/4] Добавление dmsh в PATH пользователя..." -ForegroundColor Yellow
$UserPath = [Environment]::GetEnvironmentVariable("Path", "User")
if ($UserPath -notlike "*$InstallDir*") {
    $NewPath = "$UserPath;$InstallDir"
    # Удаляем возможные двойные точки с запятой
    $NewPath = $NewPath -replace ";+", ";"
    [Environment]::SetEnvironmentVariable("Path", $NewPath, "User")
    # Обновляем PATH в текущей сессии
    $env:Path = "$env:Path;$InstallDir"
    Write-Host "Путь $InstallDir успешно добавлен в PATH!" -ForegroundColor Green
} else {
    Write-Host "dmsh уже присутствует в PATH." -ForegroundColor Gray
}

# 4. Автоматическое скачивание рекомендуемой модели
Write-Host "`n[4/4] Скачивание рекомендуемой LLM-модели..." -ForegroundColor Yellow
$DmshExe = Join-Path $InstallDir "dmsh.exe"
try {
    & $DmshExe model download --set-default
    Write-Host "`nМодель успешно скачана и установлена по умолчанию!" -ForegroundColor Green
} catch {
    Write-Warning "Не удалось запустить автоматическое скачивание модели. Вы можете сделать это позже вручную через команду: dmsh model download"
}

Write-Host "`n===============================================" -ForegroundColor Cyan
Write-Host "Установка успешно завершена!" -ForegroundColor Green
Write-Host "Откройте НОВОЕ окно терминала и наберите 'dmsh' для запуска." -ForegroundColor Yellow
Write-Host "===============================================" -ForegroundColor Cyan
