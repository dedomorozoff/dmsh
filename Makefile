# Detect environment: Unix (Linux/macOS/Cygwin) vs Windows native (choco make)
UNAME_S := $(shell uname -s 2>/dev/null)

.DEFAULT_GOAL := help

.PHONY: help
help:
	@echo "dmsh — Direct Model Shell"
	@echo ""
	@echo "Build targets:"
	@echo "  build             Build with llama.cpp (CGO) — bin/dmsh"
	@echo "  build-stub        Build without llama.cpp (stub)"
	@echo "  llama / llama-prepare  Build llama.cpp C library"
	@echo "  build-all         Build for all platforms"
	@echo ""
	@echo "Platform-specific builds:"
	@echo "  build-windows     Build Windows binary"
	@echo "  build-linux       Build Linux binary"
	@echo "  build-macos       Build macOS binary (amd64 + arm64)"
	@echo "  build-freebsd     Build FreeBSD binary"
	@echo ""
	@echo "Distribution packages:"
	@echo "  dist-deb          Build .deb package"
	@echo "  dist-rpm          Build .rpm package"
	@echo "  dist-linux-tar    Build Linux tarball"
	@echo "  dist-macos        Build macOS tarballs"
	@echo "  dist-freebsd      Build FreeBSD tarball"
	@echo "  dist-windows      Build Windows zip"
	@echo "  dist-windows-bundle  Build Windows bundle installer"
	@echo "  dist-all          Build all distribution packages"
	@echo ""
	@echo "Other:"
	@echo "  test              Run tests (go test ./...)"
	@echo "  clean             Remove build artifacts"
	@echo "  gen-man           Generate man pages"
	@echo ""
	@echo "Options:"
	@echo "  GPU=cuda|metal|vulkan  Enable GPU acceleration"
	@echo "  LLAMA_JOBS=N           Parallel build jobs (default: 2)"
	@echo ""

UNAME_S := $(shell uname -s 2>/dev/null)
IS_UNIX := $(if $(or $(findstring Linux,$(UNAME_S)),$(findstring Darwin,$(UNAME_S)),$(findstring CYGWIN,$(UNAME_S)),$(findstring MSYS,$(UNAME_S))),1,)

LLAMA_DIR := third_party/llama.cpp
LLAMA_BUILD := $(LLAMA_DIR)/build

GO := go
GOFLAGS ?=
VERSION ?= $(shell git describe --tags --always 2>/dev/null | sed 's/^llama-//;s/^v//' || echo dev)
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
LDFLAGS ?= -s -w -X github.com/dedomorozoff/dmsh/internal/cli.Version=$(VERSION) -X github.com/dedomorozoff/dmsh/internal/cli.BuildDate=$(BUILD_DATE)

# По умолчанию собираем CPU-вариант. Через GPU=1 включаются ускорители.
GPU ?= 0
# Портативная сборка: без AVX2/FMA/AVX512/BMI2, чтобы бинарник работал на старых CPU.
# При желании ускорения на конкретной машине можно переопределить: make CMAKE_ARCH_FLAGS="-DGGML_AVX2=ON -DGGML_FMA=ON -DGGML_BMI2=ON"
CMAKE_ARCH_FLAGS ?= -DGGML_AVX2=OFF -DGGML_FMA=OFF -DGGML_AVX512=OFF -DGGML_BMI2=OFF
CMAKE_FLAGS := -DBUILD_SHARED_LIBS=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_TOOLS=OFF -DLLAMA_BUILD_SERVER=OFF -DGGML_NATIVE=OFF -DGGML_CUDA=OFF $(CMAKE_ARCH_FLAGS)
ifeq ($(GPU),cuda)
CMAKE_FLAGS += -DGGML_CUDA=ON
endif
ifeq ($(GPU),metal)
CMAKE_FLAGS += -DGGML_METAL=ON
endif
ifeq ($(GPU),vulkan)
CMAKE_FLAGS += -DGGML_VULKAN=ON
endif

# Ограничение параллелизма для сборки llama.cpp (чтобы не было out of memory)
LLAMA_JOBS ?= 2

# Windows-specific DLLs from MinGW
MINGW_BIN := /c/ProgramData/mingw64/mingw64/bin

.DEFAULT_GOAL := help
.PHONY: help
help: ## Показать список доступных целей
	@echo "dmsh targets:"
	@echo
	@grep -E '^[a-zA-Z_-]+:.*##' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*##"} {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'
	@echo
	@echo "Useful variables: GOFLAGS, LDFLAGS, GPU (0|cuda|metal|vulkan), LLAMA_JOBS, CMAKE_ARCH_FLAGS"

.PHONY: submodule
submodule: ## Инициализировать и обновить вложенный модуль llama.cpp
	git submodule update --init --recursive

.PHONY: llama-prepare llama
llama-prepare: submodule ## Собрать llama.cpp (CGO) для текущей платформы
ifeq ($(IS_UNIX),1)
	cmake -S $(LLAMA_DIR) -B $(LLAMA_BUILD) $(CMAKE_FLAGS)
	cmake --build $(LLAMA_BUILD) --config Release --parallel $(LLAMA_JOBS)
else
	powershell -Command "if (-not (Test-Path 'third_party/llama.cpp/build')) { New-Item -ItemType Directory -Path 'third_party/llama.cpp/build' }"
	PATH="$(MINGW_BIN):$$PATH" cmake -G "MinGW Makefiles" -S $(LLAMA_DIR) -B $(LLAMA_BUILD) $(CMAKE_FLAGS)
	PATH="$(MINGW_BIN):$$PATH" cmake --build $(LLAMA_BUILD) --config Release --parallel $(LLAMA_JOBS)
endif

llama: llama-prepare

.PHONY: build
build: llama-prepare ## Собрать релизный бинарник (llama, CGO)
ifeq ($(IS_UNIX),1)
	$(GO) build $(GOFLAGS) -tags llama -ldflags "$(LDFLAGS)" -o bin/dmsh ./cmd/dmsh
else
	powershell -Command "if (-not (Test-Path bin)) { New-Item -ItemType Directory -Path bin }"
	go build -tags llama -ldflags "$(LDFLAGS)" -o bin/dmsh.exe ./cmd/dmsh
	powershell -Command "if (Test-Path '$(MINGW_BIN)/libstdc++-6.dll') { Copy-Item '$(MINGW_BIN)/libstdc++-6.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libgcc_s_seh-1.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libgomp-1.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libwinpthread-1.dll' bin/ -Force }"
endif

.PHONY: build-stub
build-stub: ## Собрать бинарник без CGO/llama
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/dmsh ./cmd/dmsh

# Сборка для всех платформ (для локального создания релизов)
.PHONY: build-all
build-all: build-windows build-linux build-macos build-freebsd ## Собрать бинарники для всех платформ

.PHONY: build-windows
build-windows: ## Собрать Windows-бинарник
ifeq ($(IS_UNIX),1)
ifneq ($(findstring CYGWIN,$(UNAME_S))$(findstring MSYS,$(UNAME_S)),)
	mkdir -p bin
	$(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/dmsh-windows-amd64.exe ./cmd/dmsh
	@if [ -d "$(MINGW_BIN)" ]; then \
		cp -f "$(MINGW_BIN)"/libstdc++-6.dll "$(MINGW_BIN)"/libgcc_s_seh-1.dll "$(MINGW_BIN)"/libgomp-1.dll "$(MINGW_BIN)"/libwinpthread-1.dll bin/ 2>/dev/null || true; \
	fi
else
	@echo "Note: Cross-compiling Windows binary from standard Unix. Building stub."
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-windows-amd64.exe ./cmd/dmsh
endif
else
	powershell -Command "if (-not (Test-Path bin)) { New-Item -ItemType Directory -Path bin }"
	powershell -Command "go build -tags llama -ldflags '$(LDFLAGS)' -o bin/dmsh-windows-amd64.exe ./cmd/dmsh"
	powershell -Command "if (Test-Path '$(MINGW_BIN)/libstdc++-6.dll') { Copy-Item '$(MINGW_BIN)/libstdc++-6.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libgcc_s_seh-1.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libgomp-1.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libwinpthread-1.dll' bin/ -Force }"
endif

.PHONY: build-linux
build-linux: ## Собрать Linux-бинарник
ifeq ($(shell uname -s 2>/dev/null),Linux)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CGO_CFLAGS="-I$(LLAMA_BUILD)/include" CGO_LDFLAGS="-L$(LLAMA_BUILD)/lib" $(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/dmsh-linux-amd64 ./cmd/dmsh
else
	@echo "Note: CGO cross-compilation to Linux is only supported when building on Linux. Building stub instead."
ifeq ($(IS_UNIX),1)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-linux-amd64 ./cmd/dmsh
else
	powershell -Command '$$env:GOOS="linux"; $$env:GOARCH="amd64"; $$env:CGO_ENABLED="0"; go build -ldflags "$(LDFLAGS)" -o bin/dmsh-linux-amd64 ./cmd/dmsh'
endif
endif

.PHONY: build-macos
build-macos: ## Собрать macOS-бинарники (amd64 + arm64)
ifeq ($(shell uname -s 2>/dev/null),Darwin)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 CGO_CFLAGS="-I$(LLAMA_BUILD)/include" CGO_LDFLAGS="-L$(LLAMA_BUILD)/lib" $(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/dmsh-macos-amd64 ./cmd/dmsh
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 CGO_CFLAGS="-I$(LLAMA_BUILD)/include" CGO_LDFLAGS="-L$(LLAMA_BUILD)/lib" $(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/dmsh-macos-arm64 ./cmd/dmsh
else
	@echo "Note: CGO cross-compilation to macOS is only supported when building on macOS. Building stubs instead."
ifeq ($(IS_UNIX),1)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-macos-amd64 ./cmd/dmsh
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-macos-arm64 ./cmd/dmsh
else
	powershell -Command '$$env:GOOS="darwin"; $$env:GOARCH="amd64"; $$env:CGO_ENABLED="0"; go build -ldflags "$(LDFLAGS)" -o bin/dmsh-macos-amd64 ./cmd/dmsh'
	powershell -Command '$$env:GOOS="darwin"; $$env:GOARCH="arm64"; $$env:CGO_ENABLED="0"; go build -ldflags "$(LDFLAGS)" -o bin/dmsh-macos-arm64 ./cmd/dmsh'
endif
endif

.PHONY: build-freebsd
build-freebsd: ## Собрать FreeBSD-бинарник
ifeq ($(shell uname -s 2>/dev/null),FreeBSD)
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=1 CGO_CFLAGS="-I$(LLAMA_BUILD)/include" CGO_LDFLAGS="-L$(LLAMA_BUILD)/lib" $(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/dmsh-freebsd-amd64 ./cmd/dmsh
else
	@echo "Note: CGO cross-compilation to FreeBSD is only supported when building on FreeBSD. Building stub instead."
ifeq ($(IS_UNIX),1)
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/dmsh-freebsd-amd64 ./cmd/dmsh
else
	powershell -Command '$$env:GOOS="freebsd"; $$env:GOARCH="amd64"; $$env:CGO_ENABLED="0"; go build -ldflags "$(LDFLAGS)" -o bin/dmsh-freebsd-amd64 ./cmd/dmsh'
endif
endif

.PHONY: test
test: ## Запустить все тесты
	$(GO) test ./...

.PHONY: clean
clean: ## Удалить bin/, dist/ и build-каталог llama.cpp
ifeq ($(IS_UNIX),1)
	rm -rf bin/ $(LLAMA_BUILD) dist/
else
	powershell -Command "if (Test-Path bin) { Remove-Item -Recurse -Force bin }"
	powershell -Command "if (Test-Path '$(LLAMA_BUILD)') { Remove-Item -Recurse -Force '$(LLAMA_BUILD)' }"
	powershell -Command "if (Test-Path dist) { Remove-Item -Recurse -Force dist }"
endif

.PHONY: gen-man
gen-man: ## Сгенерировать man-страницы
	$(GO) run ./cmd/genman

.PHONY: dist-deb
dist-deb: build-linux gen-man ## Собрать .deb пакет
ifeq ($(IS_UNIX),1)
	@if command -v dpkg-deb >/dev/null 2>&1; then \
		mkdir -p dist/deb/usr/bin; \
		mkdir -p dist/deb/usr/share/man/man1; \
		mkdir -p dist/deb/DEBIAN; \
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
		mkdir -p dist/rpmbuild/BUILD dist/rpmbuild/RPMS dist/rpmbuild/SOURCES dist/rpmbuild/SPECS dist/rpmbuild/SRPMS; \
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

.PHONY: dist-macos
dist-macos: build-macos gen-man ## Собрать tar.gz для macOS
ifeq ($(IS_UNIX),1)
	# Package for amd64
	mkdir -p dist/macos-amd64/bin dist/macos-amd64/share/man/man1
	cp bin/dmsh-macos-amd64 dist/macos-amd64/bin/dmsh
	cp man/* dist/macos-amd64/share/man/man1/
	cp README.md dist/macos-amd64/
	tar -czf bin/dmsh-$(VERSION)-darwin-amd64.tar.gz -C dist/macos-amd64 bin share README.md
	# Package for arm64
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

.PHONY: dist-arch
dist-arch: ## Собрать и установить пакет для Arch Linux (makepkg -si)
	@if command -v makepkg >/dev/null 2>&1; then \
		mkdir -p dist/arch; \
		sed 's|git+https://github.com/dedomorozoff/dmsh.git#tag=v$$pkgver|git+file://$(CURDIR)#commit=$(shell git rev-parse HEAD)|' PKGBUILD > dist/arch/PKGBUILD; \
		cd dist/arch && makepkg -si; \
		echo "Пакет собран: $(CURDIR)/dist/arch/dmsh-*.pkg.tar.zst"; \
	else \
		echo "makepkg not found. Install base-devel: sudo pacman -S base-devel"; \
	fi

.PHONY: dist-windows
dist-windows: build-windows ## Собрать .zip для Windows
ifeq ($(IS_UNIX),1)
	# Package as zip
	mkdir -p dist/windows-amd64
	cp bin/dmsh-windows-amd64.exe dist/windows-amd64/dmsh.exe
	cp README.md dist/windows-amd64/
		zip -r bin/dmsh-$(VERSION)-windows-amd64.zip dist/windows-amd64
	rm -rf dist
	@if command -v iscc >/dev/null 2>&1; then \
		iscc installer.iss; \
	else \
		echo "iscc (Inno Setup) not found. Skipping GUI installer compilation."; \
	fi
else
	powershell -Command "if (-not (Test-Path dist)) { New-Item -ItemType Directory -Path dist }"
	powershell -Command "Copy-Item bin/dmsh-windows-amd64.exe dist/dmsh.exe -Force"
	powershell -Command "Copy-Item README.md dist/README.md -Force"
	powershell -Command "Remove-Item 'bin/dmsh-$(VERSION)-windows-amd64.zip' -Force -ErrorAction SilentlyContinue; tar -a -c -f bin/dmsh-$(VERSION)-windows-amd64.zip -C dist ."
	powershell -Command "Remove-Item -Recurse -Force dist"
	powershell -Command "if (Get-Command 'iscc' -ErrorAction SilentlyContinue) { iscc installer.iss } else { Write-Host 'iscc (Inno Setup) not found. Skipping GUI installer compilation.' -ForegroundColor Yellow }"
endif

.PHONY: dist-windows-bundle
dist-windows-bundle: build-windows ## Собрать Windows-инсталлятор (Inno Setup)
ifeq ($(IS_UNIX),1)
	@echo "Bundle installer requires Windows. Use: powershell -ExecutionPolicy Bypass -File build-bundle.ps1 && iscc installer-bundle.iss"
else
	powershell -ExecutionPolicy Bypass -File build-bundle.ps1
	powershell -Command "if (Get-Command 'iscc' -ErrorAction SilentlyContinue) { iscc installer-bundle.iss } else { Write-Host 'iscc (Inno Setup) not found. Skipping GUI installer compilation.' -ForegroundColor Yellow }"
endif

.PHONY: dist-all
dist-all: dist-deb dist-rpm dist-linux-tar dist-macos dist-freebsd dist-windows ## Собрать все дистрибутивы
