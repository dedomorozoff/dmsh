# Maintainer: Dedo <dedo@morozoff>
# Для локальной сборки из текущего дерева используйте: make dist-arch

pkgname=nlsh
pkgver=1.0.0
pkgrel=1
pkgdesc="Natural Language Shell - run shell commands in natural language via a local LLM"
arch=('x86_64')
url="https://github.com/dedomorozoff/nlsh"
license=('MIT')
depends=('gcc-libs' 'glibc')
makedepends=('go' 'cmake' 'gcc' 'make' 'git')
source=("$pkgname::git+https://github.com/dedomorozoff/nlsh.git#tag=v1.0.0")
sha256sums=('SKIP')

prepare() {
  cd "$srcdir/$pkgname"
  git submodule update --init --recursive
}

build() {
  cd "$srcdir/$pkgname"
  make build
}

package() {
  cd "$srcdir/$pkgname"
  install -Dm755 bin/nlsh "$pkgdir/usr/bin/nlsh"
  install -Dm644 man/nlsh.1 -t "$pkgdir/usr/share/man/man1"
  install -Dm644 man/nlsh-*.1 -t "$pkgdir/usr/share/man/man1"
}
