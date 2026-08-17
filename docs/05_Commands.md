Commands
--------

Editing the Configuration

```shell
psl config
```

This opens the `.pslrc` psl would read here — the one in the current directory, or the one in your home directory — in `$VISUAL`, `$EDITOR`, or whichever of `vim`, `vi` and `nano` is installed. With no `.pslrc` anywhere yet, it writes the shipped example to `~/.pslrc` first, so the editor opens a file with the sections already in it and the keys left blank. When you close the editor the file is parsed, and a typo is reported there and then rather than on the next compile.

Checking What Has Been Spent

```shell
psl usage
```

Every request psl makes is recorded in `~/.psl/psl.log`; `psl usage` adds it up and prints what each model has spent, heaviest first:

```shell
$ psl usage
MODEL          REQUESTS  INPUT  OUTPUT  TOTAL
claude-opus-5        12  14203    1872  16075
gpt-5.6               3   2110     405   2515
TOTAL                15  16313    2277  18590
psl: /Users/you/.psl/psl.log (2026-08-10 to 2026-08-13)
```

The table goes to stdout and everything else to stderr, so it pipes into `column`, `awk` or a spreadsheet as it stands. An `ERRORS` column appears alongside `REQUESTS` once some request has failed — a failed request spends nothing, so it counts as a request without moving the tokens. The totals cover the whole log, which is never rotated: to count a shorter period, or to group by anything other than the model, read the log with `jq` — see [Logging](01_Logging.md).

Updating

However psl was installed, it updates itself from the GitHub releases:

```shell
psl update
```

It downloads the release built for your platform, checks it against the release's `SHA256SUMS` — a release it cannot verify is never installed — and swaps it in for the running executable. The old binary is only replaced once the new one is on disk and verified, so a failed update leaves the working psl in place. If psl lives somewhere that needs root, run `sudo psl update`.

`psl config`, `psl usage` and `psl update` are the three arguments that are not file names — `config` edits your configuration, `usage` reports what has been spent, and `update` upgrades psl itself; see [Install](00_Insttallation.md). A file genuinely called `update` still compiles as `psl ./update`.
