class Dmsh < Formula
  desc "Direct Model Shell - natural language shell assistant"
  homepage "https://github.com/dedomorozoff/dmsh"
  version "0.0.0"
  license "MIT"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/dedomorozoff/dmsh/releases/download/v#{version}/dmsh-#{version}-darwin-arm64.tar.gz"
      sha256 "REPLACE_WITH_SHA256"
    else
      url "https://github.com/dedomorozoff/dmsh/releases/download/v#{version}/dmsh-#{version}-darwin-amd64.tar.gz"
      sha256 "REPLACE_WITH_SHA256"
    end
  end

  on_linux do
    url "https://github.com/dedomorozoff/dmsh/releases/download/v#{version}/dmsh-#{version}-linux-amd64.tar.gz"
    sha256 "REPLACE_WITH_SHA256"
  end

  def install
    bin.install "bin/dmsh"
    man1.install Dir["share/man/man1/*"] if Dir.exist?("share/man/man1")
  end

  test do
    assert_match "dmsh", shell_output("#{bin}/dmsh version")
  end
end
