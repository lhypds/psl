Development
-----------

```shell
make test       # go test ./...
make vet        # gofmt check plus go vet
make build      # ./build.sh
make install    # ./install.sh
make uninstall  # ./uninstall.sh
make release    # ./release.sh
```

The compiler is a thin pipeline: [internal/slot](../internal/slot) finds and rewrites slots, [internal/pslrc](../internal/pslrc) reads the configuration, [internal/llm](../internal/llm) speaks the Anthropic and OpenAI chat APIs, [internal/psllog](../internal/psllog) records each request, [internal/updater](../internal/updater) handles `psl update`, and [internal/compiler](../internal/compiler) ties them together and writes the file back atomically.


Releasing

The released version lives in the `VERSION` file, and is embedded in the binary — `psl --version` reports it however psl was built, adding the exact build when it differs.

```shell
./release.sh --dry-run   # cross-compile and package into dist/, publish nothing
./release.sh             # the real thing
```

`release.sh` refuses to run on a dirty tree, runs the tests, cross-compiles for macOS, Linux and Windows, packages each target with the README and `.pslrc.example` plus a `SHA256SUMS` file, then tags `v$VERSION` and publishes the GitHub release with `gh`. Set `TARGETS` to build a different matrix.
