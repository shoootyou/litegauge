# Configuration

litegauge reads the gateway URL and the keys from the environment, a keys file or stdin. The key is never a command-line argument: the tool refuses `--key`, `--token` and similar flags, and any argument shaped like a key, with exit 2 before it sends a request.

## `LITEGAUGE_URL`

The base URL of the gateway. Required for every command that talks to it.

```
export LITEGAUGE_URL=https://litellm.example.com
```

- The scheme must be `https`. `http` is accepted only when the host is `localhost`, an address in `127.0.0.0/8` or `::1`. The check is on the literal host with no DNS lookup, so `localhost.example.com` is refused.
- A URL with user info, a query string or a fragment is refused.
- A path prefix is kept. Trailing slashes and one trailing `/v1` are removed, so `https://host/v1/` and `https://host` reach the same routes, and `https://host/litellm/v1` uses `/litellm` as the prefix.
- A missing or refused URL prints `Error: missing LITEGAUGE_URL.` or `Error: LITEGAUGE_URL must be https (or http on localhost).` and exits 2.
- No proxy is used for an `http` URL, so a proxy never sees a key in clear.

## Keys

The tool takes keys from one source. A keys file or stdin wins over `LITEGAUGE_KEY`.

### `LITEGAUGE_KEY`

A single key, used when `--keys-file` is absent. It is named `env` in the output. The tool removes the variable from its own process environment right after reading it. A key shorter than 8 characters, longer than 512, or with characters outside `A-Za-z0-9._~+/=-` prints `Error: the key in LITEGAUGE_KEY is not valid.` and exits 2. With neither source set, the sentence is `Error: missing key: use LITEGAUGE_KEY or --keys-file.`

`/proc/<pid>/environ` on Linux still shows the original environment block of the process. Under tmux, on a terminal, the terminal library runs `tmux info` before the tool can remove the key, and that child process sees `LITEGAUGE_KEY`. Under tmux use a keys file or stdin.

### `--keys-file PATH`

A text file with one key per line.

```
# keys.txt
Alpha=sk-FAKE-EXAMPLE-0001
Bravo=sk-FAKE-EXAMPLE-0002
sk-FAKE-EXAMPLE-0003
```

- `name=key` names the key. The text before the first `=` is the name, with surrounding spaces trimmed.
- A line with no `=` is a bare key. Bare keys are named `key1`, `key2` and so on, counted among bare keys only. In the file above the third line is `key1`. A bare key that holds an `=` is read as `name=key` and is usually refused.
- Blank lines and lines whose first non-space character is `#` are ignored. A leading byte order mark and CRLF line endings are accepted.
- A name is 1 to 24 characters of letters, digits, `_`, `.` and `-`, starting with a letter or digit. Names must be unique, and a name may not contain 8 characters of any loaded key.
- A key is 8 to 512 characters from `A-Za-z0-9._~+/=-`.
- The file may hold up to 1 MiB and 500 lines. A file over either limit, a missing file and an unreadable one print `Error: could not read the keys file.` and exit 2.
- A line that breaks a rule is reported by number only, never by its text: `Error: line 2 of the keys file is not valid.` and exit 2.
- The tool does not check the file mode. Run `chmod 600` on it yourself.

### Stdin

`--keys-file -` reads the same format from stdin:

```
$ echo 'sk-FAKE-EXAMPLE-0001' | ./litegauge probe claude-sonnet-5-5 --keys-file -
Key     key1
Model   claude-sonnet-5-5 (paid)
Result  ✓ Ok  HTTP 200 · 1 ms
Detail  The key has budget for this model.
```

A single bare key on stdin is named `key1`. With stdin as the key source, `matrix` shows no progress bar even on a terminal, because the terminal interface needs stdin for itself.

### `--name NAME`

`probe` and `models` work with one key. With a file of several keys, `--name` picks one. Without it, a file of more than one key prints `Error: the file has 3 keys; pick one with --name.` and exits 2. An unknown name prints `Error: no key named "Zed".`. If the name could carry key material, the sentence is the generic `Error: invalid argument.` instead. `matrix` uses every key and has no `--name`.

## Output format: `-o`

`-o table|json|yaml`, default `table`. Any other value prints `Error: unknown format; use table, json or yaml.` and exits 2. See [output.md](output.md) for the formats.

## Flag reference

Every command takes `-h, --help`. The shared flags come first in each command's help.

| Flag | Commands | Default | Meaning |
|---|---|---|---|
| `-o`, `--output string` | `models`, `probe`, `matrix` | `table` | Output format: `table`, `json` or `yaml`. |
| `--keys-file string` | `models`, `probe`, `matrix` | none | Keys file (`name=key` per line), or `-` for stdin. |
| `--name string` | `models`, `probe` | none | Name of the key to use from the file. |
| `--models string` | `matrix` | all text models | Comma-separated model ids to probe. |
| `-h`, `--help` | root, `models`, `probe`, `matrix` | | Prints the help. |
| `--version` | root only | | Prints `Litegauge <version>` to stdout and exits 0. It reads neither the URL nor a key. |

Positional arguments: `probe` takes exactly one, the model id (`probe MODEL`). `models` and `matrix` take none. The root command `litegauge` with no arguments prints its help and exits 0. A command alone, such as `litegauge probe`, prints only its help and exits 0 when neither `LITEGAUGE_URL` nor `LITEGAUGE_KEY` is set. With either one set, a missing model prints `Error: missing model.` followed by the help, and exits 2.

A model id passed to `probe` or `--models` must be 1 to 128 characters of letters, digits and `. _ : / @ + -`, starting with a letter or digit, and must not carry key material. Any other value prints `Error: invalid argument.` and exits 2.

The help text is in English, as the binary prints it, and every help screen starts with the line `Litegauge <version>` and a blank line. Run `litegauge -h` and `litegauge COMMAND -h` for the exact text. The messages live in `internal/messages/messages.go`, so changing the language is a one-file edit.

`--version` exists on the root command only. `litegauge probe --version` is an unknown flag: it prints `Error: invalid argument.` followed by the help of `probe`, and exits 2. `litegauge --version -h` prints the root help. The version is `dev` for a plain build, or the value set at build time with `-ldflags "-X github.com/shoootyou/litegauge/internal/versions.Version=<v>"`; a value that starts with a digit is shown with a leading `v` (`1.2.3` prints `Litegauge v1.2.3`).
