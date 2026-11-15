# Troubleshooting

Each entry gives what you see, why, and what to do. The sentences are the ones the binary prints, in English. Latencies in the blocks are shown as `0 ms` because they change on every run. Every exit code 2 goes with the same layout on stderr: the sentence, a blank line, then the help of the command (which starts with the `Litegauge <version>` line). Exit 1 prints the sentence alone, or a result on stdout.

## Results

### A result says `✗ key` (`invalid_key`), HTTP 401

Cause: the gateway rejected the key as invalid, expired or unknown. A 403 without the `E003` code lands here too.

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Charlie
Key     Charlie
Model   claude-sonnet-5-5 (paid)
Result  ✗ Invalid key  HTTP 401 · 0 ms
Detail  The key is not valid or has expired.
```

Exit 1. Fix: check the key with its owner. A whole `key` column in a `matrix` means that key is bad. A whole `key` row beside working rows can mean the gateway retired the model.

### A result says `⊘ policy` (`blocked`), HTTP 400 or 403 with E003

Cause: the gateway policy blocks the model, whatever the key. The gateway sends a policy error (code `E003`), or an error type that ends in `_model_access_denied`. The message is read to decide the verdict and is never printed.

```
$ litegauge probe claude-opus-5-5 --keys-file keys.txt --name Alpha
Key     Alpha
Model   claude-opus-5-5 (paid)
Result  ⊘ Blocked by policy  HTTP 400 · 0 ms
Detail  The gateway policy blocks this model.
```

Exit 1. Fix: ask the gateway owner for access to the model, or leave it out with `--models`.

### A result says `✗ No budget` (`no_budget`), HTTP 422

Cause: the gateway reports `budget_exceeded` for this key and model.

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Bravo
Key     Bravo
Model   claude-sonnet-5-5 (paid)
Result  ✗ No budget  HTTP 422 · 0 ms
Detail  The budget of this key for this model is used up.
```

Exit 1. Fix: wait for the reset or ask for more budget. Do not loop `probe` while you wait: each call spends budget. A budget error can also show on free models.

### Every success shows `ok*`

Cause: `ok*` means the key works but the model is free or its price is unknown. If every model is `unknown`, the tool could not read prices: neither `/v1/model/info` nor `/model/info` answered 200 with a readable list for that key. If a model really is free, `ok*` is the best answer the tool can give.

Exit 0. Fix: check that the key may read the model info route, or accept `ok*` as "the key works".

### A result says `? Error`

Cause: something other than the cases above, and it says nothing about budget. The `Detail` line of `probe` names the cause:

| `Detail` | Cause |
|---|---|
| `No response from the gateway.` | No answer arrived: wrong URL, no network, a timeout of 30 seconds or a refused connection. |
| `Redirect not followed.` | The gateway answered with a redirect. |
| `Gateway rate limit.` | HTTP 429. |
| `Server error.` | HTTP 500 or above, after one retry. |
| `Unexpected response.` | Any other status, for example a 422 that is not `budget_exceeded`. |

Exit 1. Fix: try again later, or run `probe` on one cell and read `Detail`. If every cell is `? Error`, check `LITEGAUGE_URL` and the network first.

## Configuration errors (exit 2)

### `Error: missing LITEGAUGE_URL.`

Cause: the variable is not set or is empty. Fix: `export LITEGAUGE_URL=https://litellm.example.com`.

### `Error: LITEGAUGE_URL must be https (or http on localhost).`

Cause: the URL is `http` on a host that is not `localhost`, an address in `127.0.0.0/8` or `::1`, or it has user info, a query or a fragment, or another scheme. The tool checks the literal host with no DNS lookup, so `http://localhost.example.com` is refused. Fix: use `https`. A key never goes in clear to a remote host.

### `Error: missing key: use LITEGAUGE_KEY or --keys-file.`

Cause: no key source. Fix: pass `--keys-file PATH`, or set `LITEGAUGE_KEY` from a silent prompt: `read -rs LITEGAUGE_KEY; export LITEGAUGE_KEY`. An empty keys file gives the same sentence.

### `Error: the key in LITEGAUGE_KEY is not valid.`

Cause: the key is shorter than 8 characters, longer than 512, or holds a character outside `A-Za-z0-9._~+/=-`. Fix: check for a stray space, quote or newline when you set the variable.

### `Error: line 2 of the keys file is not valid.`

Cause: line 2 of the keys file breaks a rule: a name over 24 characters or with a character outside `A-Za-z0-9_.-`, a key that is too short or has a bad character, a duplicate name, or a name that holds 8 characters of a key. The sentence gives the number and never the text. Fix: open the file and correct that line. Blank lines and lines starting with `#` are ignored.

### `Error: could not read the keys file.`

Cause: the file is missing, unreadable, over 1 MiB or over 500 lines. Fix: check the path and the size.

### `Error: the file has 3 keys; pick one with --name.`

Cause: `probe` and `models` use one key and the file holds several. Fix: add `--name Alpha`. `matrix` uses every key and has no `--name`. An unknown name prints `Error: no key named "Zed".`.

### `Error: the key does not go on the command line; use LITEGAUGE_KEY or --keys-file.`

Cause: an argument was a key flag (`--key`, `--token`, `-k...` and similar) or shaped like a key, or equal to a loaded key. The tool refuses it before any request and does not echo it. Fix: give the key through `--keys-file` or `LITEGAUGE_KEY`. If you meant a model id and it looks like a key, it is refused on purpose.

### `Error: invalid argument.`

Cause: a model id with characters outside `A-Za-z0-9._:/@+-`, over 128 characters, or one that holds key material; a `--name` that holds key material; an unknown flag; or an unknown command. The tool does not say which, because echoing the value could print a key. Fix: check the arguments against `litegauge COMMAND -h`.

### `Error: unknown format; use table, json or yaml.`

Cause: `-o` has another value. Fix: use `table`, `json` or `yaml`.

### `Error: missing model.`

Cause: `probe` without a model. Fix: `litegauge probe MODEL`. With neither `LITEGAUGE_URL` nor `LITEGAUGE_KEY` set, `litegauge probe` alone prints only the help and exits 0.

## Listing errors (exit 1)

### `Error: could not list the models (HTTP 401).`

Cause: the call that lists the models answered with that status. A 401 or 403 means the key was rejected on the listing route. Fix: check the key as in the first entry. The sentence names the status only.

### `Error: could not list the models (no response).`

Cause: no answer arrived: wrong host or port, no network, a refused connection or a timeout. `probe` shows the same cause as `No response from the gateway.`. Fix: check `LITEGAUGE_URL` and that you can reach the host.

### `Error: could not list the models (no models).`

Cause: the gateway listed no text model for this key: an empty list, or only embedding and other non-text models. It prints nothing on stdout, in every `-o` format, in `models` and in `matrix`. Fix: ask the gateway owner which models the key may use. `probe MODEL` and `matrix --models a,b` work without a list, so you can still test a model you know.

## Environment

### Under tmux, the key is visible to another process

Cause: on a terminal under tmux, the terminal library runs `tmux info` before the tool can unset `LITEGAUGE_KEY`, and that child process sees the variable. Fix: use `--keys-file` with mode 600, or stdin with `--keys-file -`. The key is also visible in `/proc/<pid>/environ` on Linux while the run lasts, for any key given through the environment.

### No progress bar on a terminal

Cause: the bar and the spinner need a terminal on both stdin and stdout, a `TERM` that is not `dumb`, the table output and keys not read from stdin. Otherwise the tool runs without them and prints the result at the end. Fix: none needed. The result is the same.

### `matrix` takes minutes

Cause: the tool starts one request every 500 ms and keeps at most 2 in flight, so a matrix of N cells takes at least N/2 seconds. Fix: pass `--models` with the models you care about.

## Interrupted

Ctrl+C or SIGTERM ends the run with exit 130 and prints nothing.

See also: [configuration.md](configuration.md), [output.md](output.md), [security.md](security.md).
