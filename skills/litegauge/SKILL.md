---
name: litegauge
description: Check whether API keys on a LiteLLM gateway still have budget, list the models a gateway serves, and run a key-by-model matrix. Use when a LiteLLM call fails with budget_exceeded, 422 or 429, before rotating or handing out keys, or when asked which keys and models work.
---

# litegauge

litegauge sends one tiny request per key and model to a LiteLLM gateway and reports a verdict for each. It answers "does the budget check pass right now for this key on this model", not "how much is left". Its messages are in English and live in `internal/messages/messages.go`; changing the language is a one-file edit. The examples below were produced against a fake gateway.

## When to use it

- A gateway call failed with `budget_exceeded`, status 422 or 429, and you need to know which key or model is affected.
- You hold several keys and want to know which still work before you hand one out or rotate them.
- You want the list of text models a gateway serves.

Every probe spends a little of the key's budget, and the key may belong to a pool shared with other people. Tell the key owners before you run a full `matrix`.

## Install

```
go build -o litegauge ./cmd/litegauge
```

Go 1.26.7 or newer. Write the binary outside the repository, or keep it under the ignored name `./litegauge`, so it is never committed. `litegauge --version` prints `Litegauge dev` for a plain build, and `Litegauge v1.0.0` when built with `-ldflags "-X github.com/shoootyou/litegauge/internal/versions.Version=1.0.0"`. It reads no URL and no key.

## Give it the URL and a key

`LITEGAUGE_URL` is required: `https://litellm.example.com`, or `http` on `localhost` or a loopback address. Give the key in one of three ways, never as an argument:

1. A keys file readable only by you: `chmod 600 keys.txt`, then `--keys-file keys.txt`. One key per line, `name=key` or a bare key (named `key1`, `key2`, ... in order). Blank lines and lines starting with `#` are ignored. A name is up to 24 characters of letters, digits, `_`, `.` and `-`.
2. The environment, typed at a silent prompt so it never reaches a command line or the shell history: `read -rs LITEGAUGE_KEY; export LITEGAUGE_KEY`. The key is named `env`.
3. Stdin: `--keys-file -`.

```
# keys.txt
Alpha=sk-FAKE-EXAMPLE-0001
Bravo=sk-FAKE-EXAMPLE-0002
Charlie=sk-FAKE-EXAMPLE-0003
```

`--keys-file` wins over `LITEGAUGE_KEY`. With a file of several keys, `probe` and `models` need `--name NAME`; `matrix` uses all of them. The file may hold up to 1 MiB and 500 lines; a larger file is refused with `Error: could not read the keys file.` and exit 2. A bad line stops the run with its line number and never its text.

Under tmux, on a terminal, the terminal library runs `tmux info` before the tool can remove `LITEGAUGE_KEY` from its environment, and that child sees it. Under tmux use the keys file or stdin. When `LITEGAUGE_KEY` is the key source, the tool removes the variable after reading it (on Linux `/proc/<pid>/environ` still shows the original block).

## Commands

All commands take `-o table|json|yaml` (default `table`) and `-h`. The root also takes `--version`. Every help screen starts with `Litegauge <version>` and a blank line.

```
litegauge --version
litegauge models [--keys-file PATH] [--name NAME]
litegauge probe MODEL [--keys-file PATH] [--name NAME]
litegauge matrix [--keys-file PATH] [--models a,b,c]
```

`models` lists the text models, grouped, with their price class (`free`, `paid`, `unknown`, the same words in json). If the gateway lists no text models (an empty list, or only non-text entries), `models` prints `Error: could not list the models (no models).` on stderr, nothing on stdout, and exits 1, like `matrix`. `litegauge -h` and `litegauge COMMAND -h` print the help in English.

```
$ litegauge models --keys-file keys.txt --name Alpha
claude
Model              Price
claude-haiku-5-5   free
claude-sonnet-5-5  paid
claude-opus-5-5    paid

gpt
Model        Price
gpt-example  unknown
```

`probe` sends one request and shows one result:

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Bravo
Key     Bravo
Model   claude-sonnet-5-5 (paid)
Result  ✗ No budget  HTTP 422 · 1 ms
Detail  The budget of this key for this model is used up.
```

`matrix` probes every model with every key, about 2 requests in flight and one start every 0.5 s, so a large matrix takes minutes. Without `--models` it uses every text model. Pass `--models` with the paid models you care about to keep it short and cheap.

```
$ litegauge matrix --keys-file keys.txt --models claude-haiku-5-5,claude-sonnet-5-5
claude
Model              Price  Alpha  Bravo     Charlie
claude-haiku-5-5   free   ✓ ok*  ✓ ok*     ✗ key
claude-sonnet-5-5  paid   ✓ ok   ✗ budget  ✗ key
ok* = valid key, free model (budget not checked)
```

This is the pipe form. On a terminal you get a spinner or progress bar and then the same tables with borders and color. `NO_COLOR` turns color and borders off.

## Read the result

| Table | JSON `verdict` | Meaning |
|---|---|---|
| `✓ ok` | `ok` | Paid model, request passed: the key has budget now. |
| `✓ ok*` | `key_valid` | The key works, but the model is free or its price is unknown, so budget is not verified. Never report it as "has budget". |
| `✗ budget` | `no_budget` | Budget exhausted. |
| `⊘ policy` | `blocked` | The gateway policy blocks this model (some gateways answer this for some models, whatever the key). |
| `✗ key` | `invalid_key` | Key rejected. A whole `key` column means a bad key. A whole `key` row next to working rows can mean the model was retired. |
| `? model` | `model_not_found` | The gateway does not serve that model. |
| `? error` | `error` | Rate limit, server error, timeout, redirect or unreadable reply. Says nothing about budget; try again later. |

`ok` is a statement about this moment and one model. A budget error shows on any model, free ones included.

## Use it in scripts

`-o json` prints one object, no color, English values. Fields of each record in `results`, in this order: `key`, `model`, `group`, `price` (`free`, `paid`, `unknown`), `verdict`, `http_status` (0 when no response arrived), `latency_ms`. `matrix` adds `keys`, the key names. `models` prints `{"models":[{"model","group","price"}]}`. `-o yaml` is the same data.

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Alpha -o json
{"results":[{"key":"Alpha","model":"claude-sonnet-5-5","group":"claude","price":"paid","verdict":"ok","http_status":200,"latency_ms":0}]}
```

Exit codes: `0` every result is `ok` or `key_valid`; `1` at least one result is something else, the model list failed, or the tool hit an internal failure (`Error: internal error.`); `2` usage or configuration error (the sentence, then the command's help, on stderr); `130` interrupted.

## What the output never contains

The output holds only the tool's own words, key names, model ids, group names, price class, verdicts, HTTP status and latency. It never contains a key, a response body, a header or a gateway error message. Key names, model ids and an echoed `--name` value never carry the key. The tool refuses a model id or `--name` value that holds a window of a loaded key or of its sha256 (6 characters of the key body, or 8 hex characters of the hash), in any letter case, or an `sk-` string. A name in the keys file is refused if it holds an 8-character window of a loaded key. Other encodings, base64 for example, are not screened. Nobody needs to look through it for secrets, and nobody should add any to it.

## Troubleshooting

- `Error: missing LITEGAUGE_URL.` or `Error: missing key: use LITEGAUGE_KEY or --keys-file.`: set `LITEGAUGE_URL`, and give a key as described above.
- `Error: the file has 3 keys; pick one with --name.`: add `--name` to `probe` or `models`.
- `Error: line 2 of the keys file is not valid.`: fix that line (a bad name or a bad key).
- `Error: could not list the models (HTTP 401).`: the key was rejected on the listing call. Exit 1.
- Every success shows `ok*`: the tool could not read prices. It tries `/v1/model/info` and then `/model/info`; if both fail, every price is unknown.
- Every result is `error`: run `probe` on one model; if its Detail says `No response from the gateway.`, check `LITEGAUGE_URL` and the network.

## Rules for agents and people

- Never put a key in a command line. Agent tooling logs commands in full. The tool rejects `--key`, `--token`, similar flags and any argument that holds a key.
- Never print, paste, log or commit a key, part of one, a hash, or a raw gateway response.
- An agent must not read the owner's keys file (no `cat`, `head`, `grep` or editor on it). It may run `litegauge --keys-file PATH` against it once the owner agreed to the spend, and report the verdicts and key names.
- Keep keys files out of git. The repository's `.gitignore` covers `*.keys`, `*keys*.json`, `*keys*.txt`, `.env*`, `*.env` and `secrets*`; any other file name needs its own entry.
- Write examples with fake values only, such as `sk-FAKE-EXAMPLE-0001`, and neutral names such as Alpha, Bravo and Charlie.
- Do not loop `probe` to wait for a budget reset: every call spends budget.

## Status and limits

The test suite runs against a fake LiteLLM gateway. Not verified against a real deployment: a full `matrix` run with the current output and the `/model/info` route of real gateways.
