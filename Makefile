# Detect environment: Unix (Linux/macOS/Cygwin) vs Windows native (choco make)
UNAME_S := $(shell uname -s 2>/dev/null)
IS_UNIX := $(if $(or $(findstring Linux,$(UNAME_S)),$(findstring Darwin,$(UNAME_S)),$(findstring CYGWIN,$(UNAME_S)),$(findstring MSYS,$(UNAME_S))),1,)

LLAMA_DIR := third_party/llama.cpp
LLAMA_BUILD := $(LLAMA_DIR)/build

GO := go
GOFLAGS ?=
LDFLAGS ?= -s -w -X github.com/dedomorozoff/nlsh/internal/cli.Version=$(shell git describe --always --dirty 2>/dev/null || echo dev)

# По умолчанию собираем CPU-вариант. Через GPU=1 включаются ускорители.
GPU ?= 0
CMAKE_FLAGS := -DBUILD_SHARED_LIBS=OFF -DLLAMA_BUILD_TESTS=OFF -DLLAMA_BUILD_EXAMPLES=OFF -DLLAMA_BUILD_SERVER=OFF -DGGML_NATIVE=OFF -DGGML_CUDA=OFF
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
WINDOWS_DLLS := $(MINGW_BIN)/libstdc++-6.dll $(MINGW_BIN)/libgcc_s_seh-1.dll $(MINGW_BIN)/libgomp-1.dll $(MINGW_BIN)/libwinpthread-1.dll

llama-prepare: submodule
ifeq ($(IS_UNIX),1)
	cmake -S $(LLAMA_DIR) -B $(LLAMA_BUILD) $(CMAKE_FLAGS)
	cmake --build $(LLAMA_BUILD) --config Release --parallel $(LLAMA_JOBS)
else
	powershell -Command "if (-not (Test-Path 'third_party/llama.cpp/build')) { New-Item -ItemType Directory -Path 'third_party/llama.cpp/build' }"
	cmake -G "MinGW Makefiles" -S $(LLAMA_DIR) -B $(LLAMA_BUILD) $(CMAKE_FLAGS)
	cmake --build $(LLAMA_BUILD) --config Release --parallel $(LLAMA_JOBS)
endif

llama: llama-prepare

.PHONY: build
build:
ifeq ($(IS_UNIX),1)
	$(GO) build $(GOFLAGS) -tags llama -ldflags "$(LDFLAGS)" -o bin/nlsh ./cmd/nlsh
else
	powershell -Command "if (-not (Test-Path bin)) { New-Item -ItemType Directory -Path bin }"
	powershell -Command "go build -tags llama -ldflags '$(LDFLAGS)' -o bin/nlsh.exe ./cmd/nlsh"
	powershell -Command "if (Test-Path '$(MINGW_BIN)/libstdc++-6.dll') { Copy-Item '$(MINGW_BIN)/libstdc++-6.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libgcc_s_seh-1.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libgomp-1.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libwinpthread-1.dll' bin/ -Force }"
endif

.PHONY: build-stub
build-stub:
	$(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o bin/nlsh ./cmd/nlsh

# Сборка для всех платформ (для локального создания релизов)
.PHONY: build-all
build-all: build-windows build-linux build-macos build-freebsd

.PHONY: build-windows
build-windows:
ifeq ($(IS_UNIX),1)
ifneq ($(findstring CYGWIN,$(UNAME_S))$(findstring MSYS,$(UNAME_S)),)
	mkdir -p bin
	$(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/nlsh-windows-amd64.exe ./cmd/nlsh
	@if [ -d "$(MINGW_BIN)" ]; then \
		cp -f "$(MINGW_BIN)"/libstdc++-6.dll "$(MINGW_BIN)"/libgcc_s_seh-1.dll "$(MINGW_BIN)"/libgomp-1.dll "$(MINGW_BIN)"/libwinpthread-1.dll bin/ 2>/dev/null || true; \
	fi
else
	@echo "Note: Cross-compiling Windows binary from standard Unix. Building stub."
	GOOS=windows GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/nlsh-windows-amd64.exe ./cmd/nlsh
endif
else
	powershell -Command "if (-not (Test-Path bin)) { New-Item -ItemType Directory -Path bin }"
	powershell -Command "go build -tags llama -ldflags '$(LDFLAGS)' -o bin/nlsh-windows-amd64.exe ./cmd/nlsh"
	powershell -Command "if (Test-Path '$(MINGW_BIN)/libstdc++-6.dll') { Copy-Item '$(MINGW_BIN)/libstdc++-6.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libgcc_s_seh-1.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libgomp-1.dll' bin/ -Force; Copy-Item '$(MINGW_BIN)/libwinpthread-1.dll' bin/ -Force }"
endif

.PHONY: build-linux
build-linux:
ifeq ($(shell uname -s 2>/dev/null),Linux)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=1 CGO_CFLAGS="-I$(LLAMA_BUILD)/include" CGO_LDFLAGS="-L$(LLAMA_BUILD)/lib" $(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/nlsh-linux-amd64 ./cmd/nlsh
else
	@echo "Note: CGO cross-compilation to Linux is only supported when building on Linux. Building stub instead."
ifeq ($(IS_UNIX),1)
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/nlsh-linux-amd64 ./cmd/nlsh
else
	powershell -Command '$$env:GOOS="linux"; $$env:GOARCH="amd64"; $$env:CGO_ENABLED="0"; go build -ldflags "$(LDFLAGS)" -o bin/nlsh-linux-amd64 ./cmd/nlsh'
endif
endif

.PHONY: build-macos
build-macos:
ifeq ($(shell uname -s 2>/dev/null),Darwin)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=1 CGO_CFLAGS="-I$(LLAMA_BUILD)/include" CGO_LDFLAGS="-L$(LLAMA_BUILD)/lib" $(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/nlsh-macos-amd64 ./cmd/nlsh
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=1 CGO_CFLAGS="-I$(LLAMA_BUILD)/include" CGO_LDFLAGS="-L$(LLAMA_BUILD)/lib" $(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/nlsh-macos-arm64 ./cmd/nlsh
else
	@echo "Note: CGO cross-compilation to macOS is only supported when building on macOS. Building stubs instead."
ifeq ($(IS_UNIX),1)
	GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/nlsh-macos-amd64 ./cmd/nlsh
	GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/nlsh-macos-arm64 ./cmd/nlsh
else
	powershell -Command '$$env:GOOS="darwin"; $$env:GOARCH="amd64"; $$env:CGO_ENABLED="0"; go build -ldflags "$(LDFLAGS)" -o bin/nlsh-macos-amd64 ./cmd/nlsh'
	powershell -Command '$$env:GOOS="darwin"; $$env:GOARCH="arm64"; $$env:CGO_ENABLED="0"; go build -ldflags "$(LDFLAGS)" -o bin/nlsh-macos-arm64 ./cmd/nlsh'
endif
endif

.PHONY: build-freebsd
build-freebsd:
ifeq ($(shell uname -s 2>/dev/null),FreeBSD)
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=1 CGO_CFLAGS="-I$(LLAMA_BUILD)/include" CGO_LDFLAGS="-L$(LLAMA_BUILD)/lib" $(GO) build -tags llama -ldflags "$(LDFLAGS)" -o bin/nlsh-freebsd-amd64 ./cmd/nlsh
else
	@echo "Note: CGO cross-compilation to FreeBSD is only supported when building on FreeBSD. Building stub instead."
ifeq ($(IS_UNIX),1)
	GOOS=freebsd GOARCH=amd64 CGO_ENABLED=0 $(GO) build -ldflags "$(LDFLAGS)" -o bin/nlsh-freebsd-amd64 ./cmd/nlsh
else
	powershell -Command '$$env:GOOS="freebsd"; $$env:GOARCH="amd64"; $$env:CGO_ENABLED="0"; go build -ldflags "$(LDFLAGS)" -o bin/nlsh-freebsd-amd64 ./cmd/nlsh'
endif
endif

.PHONY: test
test:
	$(GO) test ./...

.PHONY: clean
clean:
ifeq ($(IS_UNIX),1)
	rm -rf bin/ $(LLAMA_BUILD) dist/
else
	powershell -Command "if (Test-Path bin) { Remove-Item -Recurse -Force bin }"
	powershell -Command "if (Test-Path '$(LLAMA_BUILD)') { Remove-Item -Recurse -Force '$(LLAMA_BUILD)' }"
	powershell -Command "if (Test-Path dist) { Remove-Item -Recurse -Force dist }"
endif

.PHONY: gen-man
gen-man:
	$(GO) run ./cmd/genman

.PHONY: dist-deb
dist-deb: build-linux gen-man
ifeq ($(IS_UNIX),1)
	@if command -v dpkg-deb >/dev/null 2>&1; then \
		mkdir -p dist/deb/usr/bin; \
		mkdir -p dist/deb/usr/share/man/man1; \
		mkdir -p dist/deb/DEBIAN; \
		cp bin/nlsh-linux-amd64 dist/deb/usr/bin/nlsh; \
		cp man/* dist/deb/usr/share/man/man1/; \
		echo "Package: nlsh" > dist/deb/DEBIAN/control; \
		echo "Version: 1.0.0" >> dist/deb/DEBIAN/control; \
		echo "Section: utils" >> dist/deb/DEBIAN/control; \
		echo "Priority: optional" >> dist/deb/DEBIAN/control; \
		echo "Architecture: amd64" >> dist/deb/DEBIAN/control; \
		echo "Maintainer: dedomorozoff <alexl@nlsh>" >> dist/deb/DEBIAN/control; \
		echo "Description: Natural Language Shell (nlsh)" >> dist/deb/DEBIAN/control; \
		dpkg-deb --build dist/deb bin/nlsh-1.0.0-amd64.deb; \
		rm -rf dist; \
		echo "Debian package created: bin/nlsh-1.0.0-amd64.deb"; \
	else \
		echo "dpkg-deb not found. Skipping deb creation."; \
	fi
else
	@echo "deb package creation is only supported on Unix."
endif

.PHONY: dist-rpm
dist-rpm: build-linux gen-man
ifeq ($(IS_UNIX),1)
	@if command -v rpmbuild >/dev/null 2>&1; then \
		mkdir -p dist/rpmbuild/BUILD dist/rpmbuild/RPMS dist/rpmbuild/SOURCES dist/rpmbuild/SPECS dist/rpmbuild/SRPMS; \
		cp bin/nlsh-linux-amd64 dist/rpmbuild/SOURCES/nlsh; \
		cp -r man dist/rpmbuild/SOURCES/man; \
		echo "Name:           nlsh" > dist/rpmbuild/SPECS/nlsh.spec; \
		echo "Version:        1.0.0" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "Release:        1%{?dist}" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "Summary: Natural Language Shell" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "License:        MIT" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "%description" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "Natural Language Shell" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "%install" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "mkdir -p %{buildroot}%{_bindir}" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "mkdir -p %{buildroot}%{_mandir}/man1" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "install -m 755 %{_sourcedir}/nlsh %{buildroot}%{_bindir}/nlsh" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "install -m 644 %{_sourcedir}/man/* %{buildroot}%{_mandir}/man1/" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "%files" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "%{_bindir}/nlsh" >> dist/rpmbuild/SPECS/nlsh.spec; \
		echo "%{_mandir}/man1/*" >> dist/rpmbuild/SPECS/nlsh.spec; \
		rpmbuild --define "_topdir $$(pwd)/dist/rpmbuild" -bb dist/rpmbuild/SPECS/nlsh.spec; \
		cp dist/rpmbuild/RPMS/*/*.rpm bin/; \
		rm -rf dist; \
		echo "RPM package created in bin/"; \
	else \
		echo "rpmbuild not found. Skipping RPM creation."; \
	fi
else
	@echo "RPM package creation is only supported on Unix."
endif

.PHONY: dist-macos
dist-macos: build-macos gen-man
ifeq ($(IS_UNIX),1)
	# Package for amd64
	mkdir -p dist/macos-amd64/bin dist/macos-amd64/share/man/man1
	cp bin/nlsh-macos-amd64 dist/macos-amd64/bin/nlsh
	cp man/* dist/macos-amd64/share/man/man1/
	cp README.md dist/macos-amd64/
	tar -czf bin/nlsh-1.0.0-darwin-amd64.tar.gz -C dist/macos-amd64 bin share README.md
	# Package for arm64
	mkdir -p dist/macos-arm64/bin dist/macos-arm64/share/man/man1
	cp bin/nlsh-macos-arm64 dist/macos-arm64/bin/nlsh
	cp man/* dist/macos-arm64/share/man/man1/
	cp README.md dist/macos-arm64/
	tar -czf bin/nlsh-1.0.0-darwin-arm64.tar.gz -C dist/macos-arm64 bin share README.md
	rm -rf dist
	echo "macOS packages created in bin/"
else
	@echo "macOS packaging is only supported on Unix."
endif

.PHONY: dist-freebsd
dist-freebsd: build-freebsd gen-man
ifeq ($(IS_UNIX),1)
	mkdir -p dist/freebsd-amd64/bin dist/freebsd-amd64/share/man/man1
	cp bin/nlsh-freebsd-amd64 dist/freebsd-amd64/bin/nlsh
	cp man/* dist/freebsd-amd64/share/man/man1/
	cp README.md dist/freebsd-amd64/
	tar -czf bin/nlsh-1.0.0-freebsd-amd64.tar.gz -C dist/freebsd-amd64 bin share README.md
	rm -rf dist
	echo "FreeBSD package created: bin/nlsh-1.0.0-freebsd-amd64.tar.gz"
else
	@echo "FreeBSD packaging is only supported on Unix."
endif

.PHONY: dist-linux-tar
dist-linux-tar: build-linux gen-man
ifeq ($(IS_UNIX),1)
	mkdir -p dist/linux-amd64/bin dist/linux-amd64/share/man/man1
	cp bin/nlsh-linux-amd64 dist/linux-amd64/bin/nlsh
	cp man/* dist/linux-amd64/share/man/man1/
	cp README.md dist/linux-amd64/
	tar -czf bin/nlsh-1.0.0-linux-amd64.tar.gz -C dist/linux-amd64 bin share README.md
	rm -rf dist
	echo "Linux tarball created: bin/nlsh-1.0.0-linux-amd64.tar.gz"
else
	@echo "Linux tarball packaging is only supported on Unix."
endif

.PHONY: dist-windows
dist-windows: build-windows
ifeq ($(IS_UNIX),1)
	# Package as zip
	mkdir -p dist/windows-amd64
	cp bin/nlsh-windows-amd64.exe dist/windows-amd64/nlsh.exe
	cp README.md dist/windows-amd64/
	zip -r bin/nlsh-1.0.0-windows-amd64.zip dist/windows-amd64
	rm -rf dist
	@if command -v iscc >/dev/null 2>&1; then \
		iscc installer.iss; \
	else \
		echo "iscc (Inno Setup) not found. Skipping GUI installer compilation."; \
	fi
else
	powershell -Command "if (-not (Test-Path dist)) { New-Item -ItemType Directory -Path dist }"
	powershell -Command "Copy-Item bin/nlsh-windows-amd64.exe dist/nlsh.exe -Force"
	powershell -Command "Copy-Item README.md dist/README.md -Force"
	powershell -Command "Compress-Archive -Path dist/* -DestinationPath bin/nlsh-1.0.0-windows-amd64.zip -Force"
	powershell -Command "Remove-Item -Recurse -Force dist"
	powershell -Command "if (Get-Command 'iscc' -ErrorAction SilentlyContinue) { iscc installer.iss } else { Write-Host 'iscc (Inno Setup) not found. Skipping GUI installer compilation.' -ForegroundColor Yellow }"
endif

.PHONY: dist-windows-bundle
dist-windows-bundle: build-windows
ifeq ($(IS_UNIX),1)
	@echo "Bundle installer requires Windows. Use: powershell -Command '.\build-bundle.ps1' && iscc installer-bundle.iss"
else
	powershell -Command ".\build-bundle.ps1"
	powershell -Command "if (Get-Command 'iscc' -ErrorAction SilentlyContinue) { iscc installer-bundle.iss } else { Write-Host 'iscc (Inno Setup) not found. Skipping GUI installer compilation.' -ForegroundColor Yellow }"
endif

.PHONY: dist-all
dist-all: dist-deb dist-rpm dist-linux-tar dist-macos dist-freebsd dist-windows
