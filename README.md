# litegauge

Check whether the API keys you hold on a LiteLLM gateway still have budget.

[![CI](https://github.com/shoootyou/litegauge/actions/workflows/ci.yml/badge.svg)](https://github.com/shoootyou/litegauge/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

litegauge sends one tiny request per key and model to a LiteLLM gateway and prints a verdict for each. It reports whether the budget check passes right now; it does not report how much budget is left. Each probe spends a little of the key's budget, so tell the key owners before you run a full `matrix`. The project is pre-1.0 and its interface may change. The tool's messages are in English, and the docs quote them as the binary prints them. The language lives in one file, `internal/messages/messages.go`.

| Command | What it does | Docs |
|---|---|---|
| `models` | Lists the text models the gateway serves, with their price class. | [docs/models.md](docs/models.md) |
| `probe MODEL` | Tests one key on one model. | [docs/probe.md](docs/probe.md) |
| `matrix` | Tests every key on every model. | [docs/matrix.md](docs/matrix.md) |

## Install

### With Homebrew

```
brew install shoootyou/tap/litegauge
```

`shoootyou/tap` is the tap in the repository `shoootyou/homebrew-tap`, and `litegauge` is a cask in it. The cask installs the same binary as the release archives, built from the darwin and linux archives. The binary is not signed or notarized, so the cask removes the macOS quarantine attribute after the install. To update, run `brew upgrade shoootyou/tap/litegauge`. The cask is updated a few minutes after a release appears.

### From a release

Each [release](https://github.com/shoootyou/litegauge/releases/latest) has one archive per system, named `litegauge_<version>_<os>_<arch>.tar.gz` (`.zip` on Windows), and a `litegauge_<version>_checksums.txt` file. `<version>` has no leading `v`: the tag `v0.1.0` gives `litegauge_0.1.0_linux_amd64.tar.gz`. The archives are attached a few minutes after the release appears. For Linux on amd64:

```
VERSION=0.1.0
BASE=https://github.com/shoootyou/litegauge/releases/download/v$VERSION
curl -fLO $BASE/litegauge_${VERSION}_linux_amd64.tar.gz
curl -fLO $BASE/litegauge_${VERSION}_checksums.txt
sha256sum -c --ignore-missing litegauge_${VERSION}_checksums.txt
tar -xzf litegauge_${VERSION}_linux_amd64.tar.gz litegauge
mkdir -p ~/.local/bin && mv litegauge ~/.local/bin/
litegauge --version
```

On macOS, check with `shasum -a 256 -c --ignore-missing litegauge_${VERSION}_checksums.txt` and use `darwin` and `arm64` or `amd64` in the file name. Make sure `~/.local/bin` is on your `PATH`. `litegauge --version` prints `Litegauge v0.1.0`. [docs/getting-started.md](docs/getting-started.md) lists all six archives.

### From source

You need Go 1.26.7 or newer (see `go.mod`).

```
git clone https://github.com/shoootyou/litegauge.git
cd litegauge
go build -o litegauge ./cmd/litegauge
./litegauge --version
```

`--version` prints `Litegauge dev` for a plain build. To stamp a version, build with `-ldflags "-X github.com/shoootyou/litegauge/internal/versions.Version=1.0.0"`; `--version` then prints `Litegauge v1.0.0`. Every help screen starts with the same line.

## Quick start

1. Set the gateway URL, write a keys file (`name=key`, one per line) and list the models:

```
export LITEGAUGE_URL=https://litellm.example.com
printf 'Alpha=sk-FAKE-EXAMPLE-0001\nBravo=sk-FAKE-EXAMPLE-0002\nCharlie=sk-FAKE-EXAMPLE-0003\n' > keys.txt
chmod 600 keys.txt
./litegauge models --keys-file keys.txt --name Alpha
```
```
claude
Model              Price
claude-haiku-5-5   free
claude-sonnet-5-5  paid
claude-opus-5-5    paid

gpt
Model        Price
gpt-example  unknown
```

With Homebrew or a release binary on your `PATH`, drop the `./` in front of `litegauge` in these commands.

2. Probe one key on one model:

```
$ ./litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Bravo
Key     Bravo
Model   claude-sonnet-5-5 (paid)
Result  ✗ No budget  HTTP 422 · 1 ms
Detail  The budget of this key for this model is used up.
```

3. Probe every key on every model:

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

These blocks come from the built binary against a local fake gateway with fake keys. Latency varies from run to run.

## Usage

```
litegauge --version
litegauge models [--keys-file PATH] [--name NAME] [-o table|json|yaml]
litegauge probe MODEL [--keys-file PATH] [--name NAME] [-o table|json|yaml]
litegauge matrix [--keys-file PATH] [--models a,b,c] [-o table|json|yaml]
```

`litegauge --version` works on the root command only. It prints `Litegauge <version>` on stdout and exits 0, without reading the URL or a key. `litegauge -h` and `litegauge COMMAND -h` print the help, and every help screen starts with the version line and a blank line:

```
$ ./litegauge --version
Litegauge dev
```

## Output and exit codes

`-o table` (default) is bordered and colored on a terminal and plain text in a pipe; `-o json` and `-o yaml` print one flat list of records with English values.
Exit `0`: every result is `ok` or `key_valid` (`ok*`). Exit `1`: some result is something else, the model list failed, or an internal failure occurred.
Exit `2`: usage or configuration error; the sentence goes to stderr, followed by the command's help. Exit `130`: interrupted.
Details are in [docs/output.md](docs/output.md).

## Security

- The key is never a command-line argument: key-shaped arguments and flags such as `--key` exit 2 before any request.
- Nothing from a response body, header or error message is printed, only the tool's own words, the HTTP status and the latency.
- Key names and model ids that carry key material are refused, so a key cannot reach an output through them.
- Redirects are not followed, and the URL must be `https` (or `http` on a loopback host).
- `LITEGAUGE_KEY` is removed from the process environment after it is read; under tmux, prefer `--keys-file` or stdin.
- The tool starts no subprocess of its own and writes no file.

Details and limits are in [docs/security.md](docs/security.md). To report a vulnerability, read [SECURITY.md](SECURITY.md).

## More

The full documentation is indexed in [docs/README.md](docs/README.md). To build, test and submit changes, read [CONTRIBUTING.md](CONTRIBUTING.md).

## License

[MIT](LICENSE), Copyright (c) 2026 Rodolfo Castelo.
