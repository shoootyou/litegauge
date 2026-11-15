# Getting started

This page takes you from an installed binary to your first `matrix`. The output blocks come from the built binary against a local fake gateway with fake keys, so your latencies and models will differ.

## Prerequisites

- Homebrew (see Install with Homebrew), a release archive (see Install from a release), or Go 1.26.7 or newer, the version in `go.mod`, to build from source.
- The base URL of a LiteLLM gateway and one or more API keys for it.

Each probe is a real chat request, so it spends a little of the key's budget, and the key may belong to a pool other people use. Tell the key owners before you run a full `matrix`.

## Install with Homebrew

```
brew install shoootyou/tap/litegauge
litegauge --version
```

The tap name `shoootyou/tap` resolves to the repository `shoootyou/homebrew-tap`, where `litegauge` is a cask. It installs the same binary as the release archives, taken from the darwin and linux archives. The binary is not signed or notarized, so the cask removes the macOS quarantine attribute after the install. The cask is updated a few minutes after a release appears. To update, run `brew upgrade shoootyou/tap/litegauge`.

## Install from a release

Each release publishes one archive per system and one checksums file. The files are named after the tag without its leading `v`, so the tag `v0.1.0` gives `litegauge_0.1.0_linux_amd64.tar.gz`:

| System | Archive |
|---|---|
| Linux, amd64 | `litegauge_<version>_linux_amd64.tar.gz` |
| Linux, arm64 | `litegauge_<version>_linux_arm64.tar.gz` |
| macOS, Intel | `litegauge_<version>_darwin_amd64.tar.gz` |
| macOS, Apple silicon | `litegauge_<version>_darwin_arm64.tar.gz` |
| Windows, amd64 | `litegauge_<version>_windows_amd64.zip` |
| Windows, arm64 | `litegauge_<version>_windows_arm64.zip` |

The checksums file is `litegauge_<version>_checksums.txt` (SHA-256). Each archive holds the `litegauge` binary (`litegauge.exe` on Windows), `LICENSE` and `README.md`. The binaries are built with `CGO_ENABLED=0`, so they need no other library. The archives are attached a few minutes after the release appears; until then the release page lists no files.

1. Open the [latest release](https://github.com/shoootyou/litegauge/releases/latest) and pick the archive for your system. The download URL is `https://github.com/shoootyou/litegauge/releases/download/v<version>/<file>`.
2. Download the archive and the checksums file into one directory:

```
VERSION=0.1.0
BASE=https://github.com/shoootyou/litegauge/releases/download/v$VERSION
curl -fLO $BASE/litegauge_${VERSION}_linux_amd64.tar.gz
curl -fLO $BASE/litegauge_${VERSION}_checksums.txt
```

3. Check the archive against the checksums file. `--ignore-missing` skips the archives you did not download. On macOS, use `shasum -a 256 -c` in place of `sha256sum -c`. On Windows, run `Get-FileHash -Algorithm SHA256 <file>` and compare the hash with the line for that file.

```
$ sha256sum -c --ignore-missing litegauge_${VERSION}_checksums.txt
litegauge_0.1.0_linux_amd64.tar.gz: OK
```

4. Unpack it and put `litegauge` in a directory on your `PATH`:

```
tar -xzf litegauge_${VERSION}_linux_amd64.tar.gz litegauge
mkdir -p ~/.local/bin && mv litegauge ~/.local/bin/
```

5. Check the install:

```
$ litegauge --version
Litegauge v0.1.0
```

A released binary prints `Litegauge v<version>` and sends the raw `<version>` in its `User-Agent` header. The examples below start the binary as `./litegauge`, the name a source build gets in the current directory. With the binary on your `PATH`, drop the `./`.

## Build from source

```
git clone https://github.com/shoootyou/litegauge.git
cd litegauge
go build -o litegauge ./cmd/litegauge
```

Check the build:

```
$ ./litegauge --version
Litegauge dev
```

`--version` works on the root command only, prints `Litegauge <version>` to stdout, exits 0 and reads neither a URL nor a key. The version is `dev` unless you build with `-ldflags "-X github.com/shoootyou/litegauge/internal/versions.Version=1.0.0"`, which prints `Litegauge v1.0.0`. The raw value (`dev`, `1.0.0`) also goes into the `User-Agent` header of its requests. Every help screen starts with the same `Litegauge <version>` line, followed by a blank line. Keep the binary as `./litegauge`, which `.gitignore` excludes.

## Set the gateway URL

```
export LITEGAUGE_URL=https://litellm.example.com
```

The URL must be `https`, or `http` on `localhost` or another loopback address. See [configuration.md](configuration.md) for the exact rules.

## Make a keys file

Write one key per line as `name=key`, and keep the file private:

```
Alpha=sk-FAKE-EXAMPLE-0001
Bravo=sk-FAKE-EXAMPLE-0002
Charlie=sk-FAKE-EXAMPLE-0003
```

```
chmod 600 keys.txt
```

The tool does not check the file mode, so `chmod 600` is your protection. Files named `*keys*.txt` are ignored by git in this repository. With a single key you can skip the file and set `LITEGAUGE_KEY` instead.

## Run the commands

`models` lists the text models the gateway serves. `probe` and `models` use one key, so with a file of several keys pick one with `--name`:

```
$ ./litegauge models --keys-file keys.txt --name Alpha
claude
Model              Price
claude-haiku-5-5   free
claude-sonnet-5-5  paid
claude-opus-5-5    paid

gpt
Model        Price
gpt-example  unknown
```

`probe` sends one request and shows one result. It exits 1 here because this key has no budget left on that model:

```
$ ./litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Bravo
Key     Bravo
Model   claude-sonnet-5-5 (paid)
Result  ✗ No budget  HTTP 422 · 1 ms
Detail  The budget of this key for this model is used up.
```

`matrix` probes every model with every key in the file. It starts about two requests at a time, one every half second, so a large matrix takes minutes:

```
$ ./litegauge matrix --keys-file keys.txt
claude
Model              Price  Alpha     Bravo     Charlie
claude-haiku-5-5   free   ✓ ok*     ✓ ok*     ✗ key
claude-sonnet-5-5  paid   ✓ ok      ✗ budget  ✗ key
claude-opus-5-5    paid   ⊘ policy  ⊘ policy  ✗ key

gpt
Model        Price    Alpha  Bravo  Charlie
gpt-example  unknown  ✓ ok*  ✓ ok*  ✗ key
ok* = valid key, free model (budget not checked)
```

Here the output is a pipe, so it is plain text. On a terminal `matrix` shows a progress bar and then bordered tables.

## What each verdict means

| Table | JSON verdict | Meaning |
|---|---|---|
| `✓ ok` | `ok` | The request passed on a paid model: the key has budget now. |
| `✓ ok*` | `key_valid` | The key works, but the model is free or its price is unknown, so budget is not verified. A cached reply also lands here. |
| `✗ budget` | `no_budget` | The key's budget is exhausted. |
| `⊘ policy` | `blocked` | The gateway policy blocks the model for this key. |
| `✗ key` | `invalid_key` | The gateway rejected the key: 401, 403, expired or unknown. |
| `? model` | `model_not_found` | The gateway does not serve the model. |
| `? error` | `error` | Anything else: rate limit, server error, timeout, redirect or an unreadable reply. |

The exit code is `0` only when every result is `ok` or `key_valid`.

## Where next

- [configuration.md](configuration.md): every variable and flag.
- [models.md](models.md), [probe.md](probe.md) and [matrix.md](matrix.md): one page per command.
- [output.md](output.md): json, yaml and exit codes.
- [troubleshooting.md](troubleshooting.md): what an error sentence means.
