Development
-----------

The compiler is written in Go and has no dependencies outside the standard library.

```shell
./build.sh               # build ./psl
./install.sh             # build and install into /usr/local/bin (sudo if needed)
./uninstall.sh           # remove it again
```

Install somewhere you own, and no root is involved:

```shell
./install.sh --prefix "$HOME/.local"
./uninstall.sh --prefix "$HOME/.local"
```

`build.sh` stamps the version from `git describe` and honours `GOOS`/`GOARCH`:

```shell
./build.sh -o dist/psl                                      # somewhere else
GOOS=linux GOARCH=amd64 ./build.sh -o dist/psl-linux-amd64  # cross-compile
```

The Makefile owns none of that logic; it just calls the scripts:

```shell
make test       # go test ./...
make vet        # gofmt check plus go vet
make build      # ./build.sh
make install    # ./install.sh
make uninstall  # ./uninstall.sh
make release    # ./release.sh
```

The compiler is a thin pipeline: [internal/lang](../internal/lang) holds one folder per language saying which `::` are that language's own syntax, how its generated file is executed, and any runtime translation peculiar to it; [internal/executor](../internal/executor) holds the language-independent process machinery used by `psl run`; [internal/slot](../internal/slot) finds and rewrites slots by asking the language about each `::`; [internal/pslrc](../internal/pslrc) reads the configuration; [internal/llm](../internal/llm) speaks the OpenAI chat completions protocol, which is the only one; [internal/psllog](../internal/psllog) records each request; [internal/updater](../internal/updater) handles `psl update`; and [internal/compiler](../internal/compiler) ties compilation together and writes the file back atomically. A runtime translator records the original PSL path and slot offset in its generated call; the internal `resolve` command reads that source and gives the compiler the complete file context without embedding another copy of the file in generated code.

Adding support for a language means adding one folder to `internal/lang` and importing it — see [Languages](04_Languages.md).


Releasing

The released version lives in the `VERSION` file, and is embedded in the binary — `psl --version` reports it however psl was built, adding the exact build when it differs.

```shell
./release.sh --dry-run   # cross-compile and package into dist/, publish nothing
./release.sh             # the real thing
```

`release.sh` refuses to run on a dirty tree, runs the tests, cross-compiles for macOS, Linux and Windows, packages each target with the README and `.pslrc.example` plus a `SHA256SUMS` file, then tags `v$VERSION` and publishes the GitHub release with `gh`. Set `TARGETS` to build a different matrix.


Installers

[get.sh](../get.sh) and [get.ps1](../get.ps1) are the `curl`/`irm` installers advertised in the README. GitHub serves them from the `main` branch itself:

```shell
curl -fsSL https://raw.githubusercontent.com/lhypds/psl/main/get.sh | sh
irm https://raw.githubusercontent.com/lhypds/psl/main/get.ps1 | iex
```

So there is no host to keep running and no certificate to renew — but a change to either script only reaches users once it is pushed to `main`, and GitHub caches raw files for about five minutes. Their paths are part of the published install command: moving or renaming one breaks the line every user copies, so it has to keep working at the old URL or change with the README.

Neither installer needs Go: they resolve the newest release, download the archive named `psl-<version>-<goos>-<goarch>`, verify it against the release's `SHA256SUMS`, and unpack the binary onto the PATH. That naming is the contract between `release.sh`, the two installers, and [internal/updater](../internal/updater) — renaming an asset breaks all three.

`get.sh` is POSIX `sh` (tested under `dash`, `bash` and `zsh`) and works with either `curl` or `wget`. It reads the latest version off the redirect that `/releases/latest` serves, falling back to the GitHub API, so an ordinary install spends no API request. `get.ps1` targets Windows PowerShell 5.1 and PowerShell 7, installs per-user into `%LOCALAPPDATA%\Programs\psl`, and falls back to the amd64 build on an arm64 machine when a release has no native arm64 asset.
