# dmsh — Direct Model Shell
#
# Две среды сборки: Unix (Linux/macOS/Cygwin/MSYS) и нативный Windows
# (choco make). IS_UNIX различает их и выбирает ветку команд.

.DEFAULT_GOAL := help

UNAME_S := $(shell uname -s 2>/dev/null)
IS_UNIX := $(if $(or $(findstring Linux,$(UNAME_S)),$(findstring Darwin,$(UNAME_S)),$(findstring CYGWIN,$(UNAME_S)),$(findstring MSYS,$(UNAME_S))),1,)

LLAMA_DIR   := third_party/llama.cpp
LLAMA_BUILD := $(LLAMA_DIR)/build

GO   ?= go
GOFLAGS ?=

# Имя собранного бинарника: Windows требует расширения для запуска из make.
ifeq ($(IS_UNIX),1)
BIN := bin/dmsh
else
BIN := bin/dmsh.exe
endif

# sed/git есть в sh; в нативном Windows используем git из PATH, а если его
# нет — откатываемся на "dev".
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^llama-//;s/^v//' 2>/dev/null || echo dev)
ifeq ($(VERSION),)
VERSION := dev
endif
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || echo unknown)
LDFLAGS ?= -s -w -X github.com/dedomorozoff/dmsh/internal/cli.Version=$(VERSION) -X github.com/dedomorozoff/dmsh/internal/cli.BuildDate=$(BUILD_DATE)

# По умолчанию собираем CPU-вариант. Через GPU=1 включаются ускорители.
GPU ?= 0
# Путь к CUDA Toolkit (переопределить под свою систему при GPU=cuda)
CUDA_PATH ?= /usr/local/cuda
# Портативная сборка: без AVX2/FMA/AVX512/BMI2, чтобы бинарник работал на старых CPU.
# Ускорить под свою машину: make CMAKE_ARCH_FLAGS="-DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_BMI2=ON"
CMAKE_ARCH_FLAGS ?= -DGGML_AVX2=OFF -DGGML_FMA=OFF -DGGML_AVX512=OFF -DGGML_BMI2=OFF
CMAKE_FLAGS := -DBUILD_SHARED_LIBS=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_TOOLS=OFF -DLLAMA_BUILD_SERVER=OFF -DGGML_NATIVE=OFF -DGGML_CUDA=OFF -DCMAKE_BUILD_TYPE=Release $(CMAKE_ARCH_FLAGS)
ifeq ($(GPU),cuda)
CMAKE_FLAGS += -DGGML_CUDA=ON
endif
ifeq ($(GPU),metal)
CMAKE_FLAGS += -DGGML_METAL=ON
endif
ifeq ($(GPU),vulkan)
CMAKE_FLAGS += -DGGML_VULKAN=ON
endif

# Ограничение параллелизма для сборки llama.cpp, чтобы не кончилась память.
LLAMA_JOBS ?= 2

# MinGW нужен для сборки llama.cpp и линковки cgo на Windows. Переопределите
# MINGW_BIN под свою установку. Значение по умолчанию — типичный путь choco.
MINGW_BIN ?= /c/ProgramData/mingw64/mingw64/bin

ifeq ($(GPU),cuda)
BUILD_TAGS := llama cuda
CUDA_GO_LDFLAGS := CGO_LDFLAGS="-L$(CUDA_PATH)/lib64 -L$(CUDA_PATH)/lib/x64 -Wl,-rpath,$(CUDA_PATH)/lib64"
else
BUILD_TAGS := llama
CUDA_GO_LDFLAGS :=
endif

# Рецепты для Windows оформлены через powershell, а не через синтаксис cmd
# («if not exist», «copy /Y»): make запускает рецепты через sh, где такой
# синтаксис не разбирается.
WIN_MKDIR_BIN = powershell -Command "if (-not (Test-Path bin)) { New-Item -ItemType Directory -Path bin | Out-Null }"
WIN_MKDIR_DIST = powershell -Command "if (-not (Test-Path dist)) { New-Item -ItemType Directory -Path dist | Out-Null }"
# Упаковка в zip отдельным скриптом: Compress-Archive живёт в модуле
# Microsoft.PowerShell.Archive, который на части сборок Windows не
# подгружается, а tar -a требует Windows 10 1803+. Скрипт пробует оба.
WIN_ZIP = powershell -ExecutionPolicy Bypass -File scripts/zip-dir.ps1 -Source dist -Destination
WIN_CLEAN_DIST = powershell -Command "Remove-Item -Recurse -Force dist"

# Кросс-компиляция из Windows. Переменные окружения задаются через
# powershell "Set-Item Env:..." — без знака доллара. Рецепты make выполняются
# через sh, где $env: превращается в PID, а "VAR=value cmd" и cmd-синтаксис
# не поддерживаются вовсе. CGO выключен всегда: llama.cpp собран под текущую
# платформу, кросс-линковка с CGO невозможна.
define win_cross_build
	powershell -Command "Set-Item Env:GOOS $(1); Set-Item Env:GOARCH $(2); Set-Item Env:CGO_ENABLED 0; $(GO) build -ldflags \"$(LDFLAGS)\" -o $(3) ./cmd/dmsh"
endef
# $$(...) экранируется: make отдаёт sh "$d", и sh подставит вместо него pid.
# В powershell переменная цикла называется $$d, поэтому после экранирования
# наверх уходит именно $$d.
WIN_COPY_MINGW_DLLS = powershell -ExecutionPolicy Bypass -File scripts/copy-mingw-dlls.ps1 -MingwBin "$(MINGW_BIN)" -Dest bin

.PHONY: help
help: ## Показать список доступных целей
	@echo "dmsh targets:"
	@echo
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*##"} {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
	@echo
	@echo "Переменные: GOFLAGS, LDFLAGS, GPU (0|cuda|metal|vulkan), LLAMA_JOBS,"
	@echo "            CMAKE_ARCH_FLAGS, MINGW_BIN, RUN_ARGS"

.PHONY: run
run: build-stub ## Собрать без llama.cpp и запустить REPL (make run RUN_ARGS="...")
	$(BIN) $(RUN_ARGS)

.PHONY: run-llama
run-llama: build ## Собрать с llama.cpp и запустить REPL
	$(BIN) $(RUN_ARGS)

.PHONY: build
build: llama-prepare ## Собрать релизный бинарник с llama.cpp (CGO)
ifeq ($(IS_UNIX),1)
	@mkdir -p bin
	$(CUDA_GO_LDFLAGS) $(GO) build $(GOFLAGS) -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/dmsh
else
	$(WIN_MKDIR_BIN)
	$(CUDA_GO_LDFLAGS) $(GO) build $(GOFLAGS) -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/dmsh
	$(WIN_COPY_MINGW_DLLS)
endif

.PHONY: build-stub
build-stub: ## Собрать бинарник без CGO/llama.cpp (быстро)
ifeq ($(IS_UNIX),1)
	@mkdir -p bin
else
	$(WIN_MKDIR_BIN)
endif
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/dmsh

.PHONY: submodule
submodule: ## Инициализировать и обновить вложенный модуль llama.cpp
	git submodule update --init --recursive

.PHONY: llama llama-prepare
llama: llama-prepare
llama-prepare: submodule ## Собрать llama.cpp (CGO) для текущей платформы
ifeq ($(IS_UNIX),1)
	cmake -S $(LLAMA_DIR) -B $(LLAMA_BUILD) $(CMAKE_FLAGS)
	cmake --build $(LLAMA_BUILD) --config Release --parallel $(LLAMA_JOBS)
else
	powershell -Command "if (-not (Test-Path '$(subst /,\\,$(LLAMA_BUILD))')) { New-Item -ItemType Directory -Force -Path '$(subst /,\\,$(LLAMA_BUILD))' | Out-Null }"
	cmake -G "MinGW Makefiles" -S $(LLAMA_DIR) -B $(LLAMA_BUILD) $(CMAKE_FLAGS)
	cmake --build $(LLAMA_BUILD) --config Release --parallel $(LLAMA_JOBS)
endif

.PHONY: build-all
build-all: build-windows build-linux build-macos build-freebsd ## Собрать бинарники для всех платформ

# Кросс-компиляция с CGO невозможна: llama.cpp собран под текущую платформу.
# Вместо неё собирается stub, поэтому llama-теги и CGO выключены.
.PHONY: build-windows
build-windows: ## Собрать Windows-бинарник
ifeq ($(IS_UNIX),1)
	@mkdir -p bin
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-windows-amd64.exe ./cmd/dmsh
else
	$(WIN_MKDIR_BIN)
	$(GO) build -tags "$(BUILD_TAGS)" -ldflags "$(LDFLAGS)" -o bin/dmsh-windows-amd64.exe ./cmd/dmsh
	$(WIN_COPY_MINGW_DLLS)
endif

.PHONY: build-linux
build-linux: ## Собрать Linux-бинарник
ifeq ($(IS_UNIX),1)
	@mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-linux-amd64 ./cmd/dmsh
else
	$(WIN_MKDIR_BIN)
	$(call win_cross_build,linux,amd64,bin/dmsh-linux-amd64)
endif

.PHONY: build-macos
build-macos: ## Собрать macOS-бинарники (amd64 + arm64)
ifeq ($(IS_UNIX),1)
	@mkdir -p bin
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-macos-amd64 ./cmd/dmsh
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-macos-arm64 ./cmd/dmsh
else
	$(WIN_MKDIR_BIN)
	$(call win_cross_build,darwin,amd64,bin/dmsh-macos-amd64)
	$(call win_cross_build,darwin,arm64,bin/dmsh-macos-arm64)
endif

.PHONY: build-freebsd
build-freebsd: ## Собрать FreeBSD-бинарник
ifeq ($(IS_UNIX),1)
	@mkdir -p bin
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-freebsd-amd64 ./cmd/dmsh
else
	$(WIN_MKDIR_BIN)
	$(call win_cross_build,freebsd,amd64,bin/dmsh-freebsd-amd64)
endif

.PHONY: test
test: ## Запустить все тесты
	$(GO) test ./...

.PHONY: test-race
test-race: ## Запустить тесты с детектором гонок
	$(GO) test -race ./...

.PHONY: vet
vet: ## Проверить код go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Отформатировать код
	$(GO) fmt ./...

.PHONY: check
check: fmt vet test ## Отформатировать, проверить и прогнать тесты

.PHONY: clean
clean: ## Удалить bin/, dist/ и build-каталог llama.cpp
ifeq ($(IS_UNIX),1)
	rm -rf bin/ $(LLAMA_BUILD) dist/ man/
else
	powershell -ExecutionPolicy Bypass -File scripts/clean-dirs.ps1 -Dirs "bin;$(subst /,\\,$(LLAMA_BUILD));dist;man"
endif

.PHONY: gen-man
gen-man: ## Сгенерировать man-страницы в man/
	$(GO) run ./cmd/genman

.PHONY: dist-deb
dist-deb: build-linux gen-man ## Собрать .deb пакет
ifeq ($(IS_UNIX),1)
	@if command -v dpkg-deb >/dev/null 2>&1; then \
		mkdir -p dist/deb/usr/bin dist/deb/usr/share/man/man1 dist/deb/DEBIAN; \
		cp bin/dmsh-linux-amd64 dist/deb/usr/bin/dmsh; \
		cp man/* dist/deb/usr/share/man/man1/; \
		echo "Package: dmsh" > dist/deb/DEBIAN/control; \
		echo "Version: $(VERSION)" >> dist/deb/DEBIAN/control; \
		echo "Section: utils" >> dist/deb/DEBIAN/control; \
		echo "Priority: optional" >> dist/deb/DEBIAN/control; \
		echo "Architecture: amd64" >> dist/deb/DEBIAN/control; \
		echo "Maintainer: dedomorozoff <alexl@dmsh>" >> dist/deb/DEBIAN/control; \
		echo "Description: Direct Model Shell (dmsh)" >> dist/deb/DEBIAN/control; \
		dpkg-deb --build dist/deb bin/dmsh-$(VERSION)-amd64.deb; \
		rm -rf dist/deb; \
		echo "Debian package created: bin/dmsh-$(VERSION)-amd64.deb"; \
	else \
		echo "dpkg-deb not found. Skipping deb creation."; \
	fi
else
	@echo "deb package creation is only supported on Unix."
endif

.PHONY: dist-rpm
dist-rpm: build-linux gen-man ## Собрать .rpm пакет
ifeq ($(IS_UNIX),1)
	@if command -v rpmbuild >/dev/null 2>&1; then \
		mkdir -p dist/rpmbuild/{BUILD,RPMS,SOURCES,SPECS,SRPMS}; \
		cp bin/dmsh-linux-amd64 dist/rpmbuild/SOURCES/dmsh; \
		cp -r man dist/rpmbuild/SOURCES/man; \
		echo "Name:           dmsh" > dist/rpmbuild/SPECS/dmsh.spec; \
		echo "Version:        $(VERSION)" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "Release:        1%{?dist}" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "Summary: Direct Model Shell" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "License:        MIT" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "%description" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "Direct Model Shell" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "%install" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "mkdir -p %{buildroot}%{_bindir}" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "mkdir -p %{buildroot}%{_mandir}/man1" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "install -m 755 %{_sourcedir}/dmsh %{buildroot}%{_bindir}/dmsh" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "install -m 644 %{_sourcedir}/man/* %{buildroot}%{_mandir}/man1/" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "%files" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "%{_bindir}/dmsh" >> dist/rpmbuild/SPECS/dmsh.spec; \
		echo "%{_mandir}/man1/*" >> dist/rpmbuild/SPECS/dmsh.spec; \
		rpmbuild --define "_topdir $$(pwd)/dist/rpmbuild" -bb dist/rpmbuild/SPECS/dmsh.spec; \
		cp dist/rpmbuild/RPMS/*/*.rpm bin/; \
		rm -rf dist/rpmbuild; \
		echo "RPM package created in bin/"; \
	else \
		echo "rpmbuild not found. Skipping RPM creation."; \
	fi
else
	@echo "RPM package creation is only supported on Unix."
endif

.PHONY: dist-linux-tar
dist-linux-tar: build-linux gen-man ## Собрать tar.gz для Linux
ifeq ($(IS_UNIX),1)
	mkdir -p dist/linux-amd64/bin dist/linux-amd64/share/man/man1
	cp bin/dmsh-linux-amd64 dist/linux-amd64/bin/dmsh
	cp man/* dist/linux-amd64/share/man/man1/
	cp README.md dist/linux-amd64/
	tar -czf bin/dmsh-$(VERSION)-linux-amd64.tar.gz -C dist/linux-amd64 bin share README.md
	rm -rf dist
	echo "Linux tarball created: bin/dmsh-$(VERSION)-linux-amd64.tar.gz"
else
	@echo "Linux tarball packaging is only supported on Unix."
endif

.PHONY: dist-macos
dist-macos: build-macos gen-man ## Собрать tar.gz для macOS
ifeq ($(IS_UNIX),1)
	mkdir -p dist/macos-amd64/bin dist/macos-amd64/share/man/man1
	cp bin/dmsh-macos-amd64 dist/macos-amd64/bin/dmsh
	cp man/* dist/macos-amd64/share/man/man1/
	cp README.md dist/macos-amd64/
	tar -czf bin/dmsh-$(VERSION)-darwin-amd64.tar.gz -C dist/macos-amd64 bin share README.md
	mkdir -p dist/macos-arm64/bin dist/macos-arm64/share/man/man1
	cp bin/dmsh-macos-arm64 dist/macos-arm64/bin/dmsh
	cp man/* dist/macos-arm64/share/man/man1/
	cp README.md dist/macos-arm64/
	tar -czf bin/dmsh-$(VERSION)-darwin-arm64.tar.gz -C dist/macos-arm64 bin share README.md
	rm -rf dist
	echo "macOS packages created in bin/"
else
	@echo "macOS packaging is only supported on Unix."
endif

.PHONY: dist-freebsd
dist-freebsd: build-freebsd gen-man ## Собрать tar.gz для FreeBSD
ifeq ($(IS_UNIX),1)
	mkdir -p dist/freebsd-amd64/bin dist/freebsd-amd64/share/man/man1
	cp bin/dmsh-freebsd-amd64 dist/freebsd-amd64/bin/dmsh
	cp man/* dist/freebsd-amd64/share/man/man1/
	cp README.md dist/freebsd-amd64/
	tar -czf bin/dmsh-$(VERSION)-freebsd-amd64.tar.gz -C dist/freebsd-amd64 bin share README.md
	rm -rf dist
	echo "FreeBSD package created: bin/dmsh-$(VERSION)-freebsd-amd64.tar.gz"
else
	@echo "FreeBSD packaging is only supported on Unix."
endif

.PHONY: dist-arch
dist-arch: ## Собрать и установить пакет для Arch Linux (makepkg -si)
ifeq ($(IS_UNIX),1)
	@if command -v makepkg >/dev/null 2>&1; then \
		mkdir -p dist/arch; \
		sed 's|git+https://github.com/dedomorozoff/dmsh.git#tag=v$$pkgver|git+file://$(CURDIR)#commit=$(shell git rev-parse HEAD)|' PKGBUILD > dist/arch/PKGBUILD; \
		cd dist/arch && makepkg -si; \
		echo "Package built: $(CURDIR)/dist/arch/dmsh-*.pkg.tar.zst"; \
	else \
		echo "makepkg not found. Install base-devel: sudo pacman -S base-devel"; \
	fi
else
	@echo "Arch packaging is only supported on Unix."
endif

.PHONY: dist-windows
dist-windows: build-windows ## Собрать .zip для Windows
ifeq ($(IS_UNIX),1)
	@command -v zip >/dev/null 2>&1 || { echo "zip not found. Skipping Windows zip."; exit 0; }; \
	mkdir -p dist/windows-amd64; \
	cp bin/dmsh-windows-amd64.exe dist/windows-amd64/dmsh.exe; \
	cp README.md dist/windows-amd64/; \
	cd dist && zip -r ../../bin/dmsh-$(VERSION)-windows-amd64.zip windows-amd64; \
	rm -rf dist; \
	echo "Windows zip created: bin/dmsh-$(VERSION)-windows-amd64.zip"
else
	$(WIN_MKDIR_BIN)
	$(WIN_MKDIR_DIST)
	powershell -Command "Copy-Item 'bin/dmsh-windows-amd64.exe' 'dist/dmsh.exe' -Force; Copy-Item 'README.md' 'dist/README.md' -Force"
	$(WIN_ZIP) bin/dmsh-$(VERSION)-windows-amd64.zip
	$(WIN_CLEAN_DIST)
endif

.PHONY: dist-windows-bundle
dist-windows-bundle: build-windows ## Собрать Windows-инсталлятор (Inno Setup)
ifeq ($(IS_UNIX),1)
	@echo "Bundle installer requires Windows. Run: powershell -ExecutionPolicy Bypass -File build-bundle.ps1 && iscc installer-bundle.iss"
else
	@powershell -ExecutionPolicy Bypass -File build-bundle.ps1
	@powershell -Command "if (Get-Command 'iscc' -ErrorAction SilentlyContinue) { iscc installer-bundle.iss } else { Write-Host 'iscc (Inno Setup) not found. Skipping GUI installer compilation.' -ForegroundColor Yellow }"
endif

.PHONY: dist-all
dist-all: dist-deb dist-rpm dist-linux-tar dist-macos dist-freebsd dist-windows ## Собрать все дистрибутивы