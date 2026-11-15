# probe

`probe` sends a minimal chat request with one key to one model and prints one verdict. It answers "does the budget check pass right now for this key on this model", not "how much is left". The output blocks below come from the built binary against a local fake gateway with fake keys.

## Synopsis

```
litegauge probe MODEL [-o table|json|yaml] [--keys-file PATH] [--name NAME]
```

`probe` takes exactly one positional argument, the model id. It needs `LITEGAUGE_URL` and one key; see [configuration.md](configuration.md).

| Flag | Default | Meaning |
|---|---|---|
| `-o`, `--output string` | `table` | Output format: `table`, `json` or `yaml`. |
| `--keys-file string` | none | Keys file (`name=key` per line), or `-` for stdin. |
| `--name string` | none | Name of the key to use from the file. |
| `-h`, `--help` | | Prints the help. |

Every help screen starts with a `Litegauge <version>` line and a blank line. Run `litegauge probe -h` for the full help.

## Cost

Each probe is a real request to `POST /v1/chat/completions` with `max_tokens` set to 16 and a short `ping lg-<random>` message, so it spends a little of the key's budget, and the key may belong to a pool other people use. The random part changes on every request, so a gateway cache cannot answer for another key. Tell the key owners before you run probes in a loop, and do not poll `probe` to wait for a budget reset: every call spends budget.

## What it sends

- One `GET /v1/model/info` (then `GET /model/info` if that route fails) to learn the model's price class. This is a read and does not spend budget. If neither route answers, the price is `unknown`.
- One chat request. A second request follows one second later only when the first ended in a transport error, a timeout or a 5xx answer. The tool never retries any other status.
- A request times out after 30 seconds. A redirect is not followed.

`probe` does not check the model id against the gateway's model list. An id the gateway does not serve comes back as `model_not_found`.

## Rules for the arguments

- With one key in the source, `--name` is optional. With a file of several keys, `--name` picks one; without it the run stops with `Error: the file has 3 keys; pick one with --name.` and exit 2. An unknown name prints `Error: no key named "Zed".`.
- The model id is 1 to 128 characters of letters, digits and `. _ : / @ + -`, starting with a letter or digit, and must not carry key material. Anything else prints `Error: invalid argument.` and exits 2 before any request.
- A missing model prints `Error: missing model.` and the help, exit 2, when `LITEGAUGE_URL` or `LITEGAUGE_KEY` is set. With neither set, `litegauge probe` alone prints the help and exits 0.

## Output

Table. Off a terminal, or with `NO_COLOR`, it is plain text; on a terminal a spinner shows while the request is in flight and the result is colored. The latency is shown here as `0 ms` because it changes on every run.

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Alpha
Key     Alpha
Model   claude-sonnet-5-5 (paid)
Result  ✓ Ok  HTTP 200 · 0 ms
Detail  The key has budget for this model.
```

The `Result` line shows the verdict, the HTTP status and the latency. When no response arrived there is no status: `Result  ? Error  0 ms`, and `Detail` says `No response from the gateway.`.

A failing verdict:

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Bravo
Key     Bravo
Model   claude-sonnet-5-5 (paid)
Result  ✗ No budget  HTTP 422 · 0 ms
Detail  The budget of this key for this model is used up.
```

A free or unpriced model can only reach `Valid key`, the table label of `ok*`, because its budget is not verified:

```
$ litegauge probe claude-haiku-5-5 --keys-file keys.txt --name Alpha
Key     Alpha
Model   claude-haiku-5-5 (free)
Result  ✓ Valid key  HTTP 200 · 0 ms
Detail  Valid key; free or unpriced model, budget not checked.
```

JSON, one compact line (the field reference is in [output.md](output.md)):

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Bravo -o json
{"results":[{"key":"Bravo","model":"claude-sonnet-5-5","group":"claude","price":"paid","verdict":"no_budget","http_status":422,"latency_ms":0}]}
```

YAML:

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Alpha -o yaml
results:
  - key: "Alpha"
    model: "claude-sonnet-5-5"
    group: "claude"
    price: "paid"
    verdict: "ok"
    http_status: 200
    latency_ms: 0
```

## Verdicts

`probe` can print any of the seven verdicts: `ok`, `key_valid`, `no_budget`, `blocked`, `invalid_key`, `model_not_found` and `error`. Their meaning and the rules that pick one are in [output.md](output.md).

## Exit codes

| Code | When |
|---|---|
| 0 | The verdict is `ok` or `key_valid` (`ok*`). |
| 1 | Any other verdict. The result is still printed on stdout. |
| 2 | Usage or configuration error. The sentence goes to stderr, followed by the help. |
| 130 | Interrupted. Nothing is printed. |

See also: [models.md](models.md), [matrix.md](matrix.md), [output.md](output.md), [troubleshooting.md](troubleshooting.md).
