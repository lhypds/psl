Install
-------

macOS and Linux:

```shell
curl -fsSL https://raw.githubusercontent.com/lhypds/psl/main/get.sh | sh
```

Windows, in PowerShell:

```powershell
irm https://raw.githubusercontent.com/lhypds/psl/main/get.ps1 | iex
```

Windows, in cmd.exe:

```bat
powershell -NoProfile -Command "irm https://raw.githubusercontent.com/lhypds/psl/main/get.ps1 | iex"
```

Either installer downloads the binary built for your platform from the latest release, checks it against the release's `SHA256SUMS` — a release it cannot verify is never installed — and puts it on your PATH. No Go toolchain is involved.

[get.sh](../get.sh) puts psl in `/usr/local/bin` when you can write there, and `$HOME/.local/bin` otherwise. To choose for yourself, pass the options through the pipe:

```shell
curl -fsSL https://raw.githubusercontent.com/lhypds/psl/main/get.sh | sh -s -- --prefix "$HOME/.local"
curl -fsSL https://raw.githubusercontent.com/lhypds/psl/main/get.sh | sudo sh   # system-wide
```

[get.ps1](../get.ps1) installs into `%LOCALAPPDATA%\Programs\psl` and adds it to your user PATH, so no administrator rights are needed — open a new terminal afterwards for `psl` to be found. Options need the script block spelled out, since a pipe has nowhere to put them:

```powershell
&([scriptblock]::Create((irm https://raw.githubusercontent.com/lhypds/psl/main/get.ps1))) -InstallDir C:\tools\psl
```

Both take `PSL_VERSION` to install a particular release, and `psl update` keeps it current afterwards. To build psl from source instead, see [Development](03_Development.md).
