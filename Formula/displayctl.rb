# Homebrew formula for displayctl: builds from source and installs the CLI.
#
# `brew services start displayctl` then runs the menu bar app at login, so this
# needs no app bundle and no Gatekeeper exception, unlike Casks/displayctl.rb.
# It does require the machine to have current Xcode command line tools, since
# Homebrew refuses source builds otherwise.
#
# To publish:
#   1. push this repo to GitHub
#   2. tag v0.1.0, then get the checksum with
#      curl -sL https://github.com/josesujith/displayctl/archive/refs/tags/v0.1.0.tar.gz | shasum -a 256
#   3. paste it below and put this file in the Formula directory of a tap repo
#      named homebrew-tap
#
# Users then run:
#   brew tap josesujith/tap
#   brew install displayctl
#   brew services start displayctl
class Displayctl < Formula
  desc "Menu bar app and CLI to connect, disconnect and control macOS displays"
  homepage "https://github.com/josesujith/displayctl"
  url "https://github.com/josesujith/displayctl/archive/refs/tags/v0.1.0.tar.gz"
  sha256 "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
  license "MIT"
  head "https://github.com/josesujith/displayctl.git", branch: "main"

  depends_on "go" => :build
  depends_on arch: :arm64 # display control uses Apple Silicon APIs
  depends_on :macos

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w")
  end

  # No keep_alive: Quit in the menu should stay quit until the next login.
  service do
    run [opt_bin/"displayctl", "menu"]
    run_type :immediate
    log_path var/"log/displayctl.log"
    error_log_path var/"log/displayctl.log"
  end

  def caveats
    <<~EOS
      Start the menu bar app, now and at login:
        brew services start displayctl

      Or use it from the command line:
        displayctl list
    EOS
  end

  test do
    assert_match "usage: displayctl", shell_output("#{bin}/displayctl 2>&1", 2)
  end
end
