# Maintainer: Dedo <dedo@morozoff>
# Для локальной сборки из текущего дерева используйте: make dist-arch

pkgname=nlsh
pkgver=0.2.5
pkgrel=1
pkgdesc="Natural Language Shell - run shell commands in natural language via a local LLM"
arch=('x86_64')
url="https://github.com/dedomorozoff/nlsh"
license=('MIT')
depends=('gcc-libs' 'glibc')
makedepends=('go' 'cmake' 'gcc' 'make' 'git')
source=("$pkgname::git+https://github.com/dedomorozoff/nlsh.git#tag=v$pkgver")
sha256sums=('SKIP')

pkgver() {
  cd "$srcdir/$pkgname"
  git describe --tags --always | sed 's/^llama-//;s/^v//;s/-/_/g'
}

prepare() {
  cd "$srcdir/$pkgname"
  git submodule update --init --recursive
}

build() {
  cd "$srcdir/$pkgname"
  # makepkg exports system LDFLAGS (e.g. -Wl,-flto) which the Go linker does not understand.
  unset LDFLAGS
  make build
}

package() {
  cd "$srcdir/$pkgname"
  install -Dm755 bin/nlsh "$pkgdir/usr/bin/nlsh"
  install -Dm644 man/nlsh.1 -t "$pkgdir/usr/share/man/man1"
  install -Dm644 man/nlsh-*.1 -t "$pkgdir/usr/share/man/man1"
}
