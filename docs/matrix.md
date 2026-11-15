# matrix

`matrix` probes every model with every key and prints one verdict per cell. Rows are models, columns are keys. Use it before you hand out or rotate keys, or to find out which keys and models are affected after a budget error. The output blocks below come from the built binary against a local fake gateway with fake keys. In the json and yaml blocks the latency is shown as `0`, because it changes on every run.

## Synopsis

```
litegauge matrix [-o table|json|yaml] [--keys-file PATH] [--models a,b,c]
```

`matrix` takes no positional argument. It needs `LITEGAUGE_URL` and at least one key; see [configuration.md](configuration.md).

| Flag | Default | Meaning |
|---|---|---|
| `-o`, `--output string` | `table` | Output format: `table`, `json` or `yaml`. |
| `--keys-file string` | none | Keys file (`name=key` per line), or `-` for stdin. |
| `--models string` | all text models | Comma-separated model ids to probe. |
| `-h`, `--help` | | Prints the help. |

`matrix` has no `--name`: it uses every key in the source. Every help screen starts with a `Litegauge <version>` line and a blank line. Run `litegauge matrix -h` for the full help.

## Cost and pace

A matrix sends one chat request per key and model, so a file of 3 keys and 4 models sends 12 requests, and each one spends a little of the key's budget. Keys may belong to a pool other people use: tell the key owners before a full run, and pass `--models` with the models you care about to keep it short and cheap.

The tool starts one request every 500 ms and keeps at most 2 in flight. Those 12 requests took about 5.5 seconds against the fake gateway, which answers at once; a real gateway takes longer, and a large matrix takes minutes. A request that fails with a transport error, a timeout or a 5xx answer is sent once more after one second. A request times out after 30 seconds.

## Which models are probed

- Without `--models`, the tool lists the models with the first key whose `GET /v1/models` call succeeds, keeps the text models (the same set `models` prints, see [models.md](models.md)) and reads their prices from `/v1/model/info` or `/model/info` with that key. If every key fails the listing, the run exits 1 with the error of the last one.
- With `--models`, the tool probes exactly those ids, in the order given, without asking the gateway for a list, and reads prices with the first key. A repeated id counts once. Every id must follow the model id rule in [configuration.md](configuration.md), or the run exits 2 with `Error: invalid argument.` before any request.
- The price of a model is one value for the whole row, taken from the key that read the model info.

## Output

Table, in a pipe: one table per group, columns joined by two spaces, no border, then the legend. This keeps the output easy to feed to `awk` or `column`.

```
$ litegauge matrix --keys-file keys.txt
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

The price column shows `free`, `paid` or `unknown`. The cell labels are listed in [output.md](output.md). A group is the part of the model id before `/`, or else before the first `-`, in lower case.

On a terminal, a progress bar with a `done/total` counter shows while the requests run, and then each group is a bordered table, with color unless `NO_COLOR` is set. `NO_COLOR` makes the tables plain but keeps the bar. The bar is not shown when `-o` is `json` or `yaml`, when `--keys-file -` reads the keys from stdin, or when `TERM` is `dumb`.

```
claude
╭───────────────────┬───────┬──────────┬──────────┬─────────╮
│ Model             │ Price │ Alpha    │ Bravo    │ Charlie │
├───────────────────┼───────┼──────────┼──────────┼─────────┤
│ claude-haiku-5-5  │ free  │ ✓ ok*    │ ✓ ok*    │ ✗ key   │
│ claude-sonnet-5-5 │ paid  │ ✓ ok     │ ✗ budget │ ✗ key   │
│ claude-opus-5-5   │ paid  │ ⊘ policy │ ⊘ policy │ ✗ key   │
╰───────────────────┴───────┴──────────┴──────────┴─────────╯
ok* = valid key, free model (budget not checked)
```

JSON and YAML print one flat `results` list with one record per cell, grouped by model in the order of the tables and, inside a model, in key order. After the list comes `keys`, the key names in file order. The field reference is in [output.md](output.md). JSON, here for one model:

```
$ litegauge matrix --keys-file keys.txt --models claude-sonnet-5-5 -o json
{"results":[{"key":"Alpha","model":"claude-sonnet-5-5","group":"claude","price":"paid","verdict":"ok","http_status":200,"latency_ms":0},{"key":"Bravo","model":"claude-sonnet-5-5","group":"claude","price":"paid","verdict":"no_budget","http_status":422,"latency_ms":0},{"key":"Charlie","model":"claude-sonnet-5-5","group":"claude","price":"paid","verdict":"invalid_key","http_status":401,"latency_ms":0}],"keys":["Alpha","Bravo","Charlie"]}
```

YAML:

```
$ litegauge matrix --keys-file keys.txt --models claude-sonnet-5-5 -o yaml
results:
  - key: "Alpha"
    model: "claude-sonnet-5-5"
    group: "claude"
    price: "paid"
    verdict: "ok"
    http_status: 200
    latency_ms: 0
  - key: "Bravo"
    model: "claude-sonnet-5-5"
    group: "claude"
    price: "paid"
    verdict: "no_budget"
    http_status: 422
    latency_ms: 0
  - key: "Charlie"
    model: "claude-sonnet-5-5"
    group: "claude"
    price: "paid"
    verdict: "invalid_key"
    http_status: 401
    latency_ms: 0
keys:
  - "Alpha"
  - "Bravo"
  - "Charlie"
```

## Exit codes

| Code | When |
|---|---|
| 0 | Every cell is `ok` or `key_valid` (`ok*`). |
| 1 | At least one cell is something else, or the model list failed or held no text model. A failed cell does not stop the run: the full matrix is printed first. |
| 2 | Usage or configuration error. The sentence goes to stderr, followed by the help. |
| 130 | Interrupted. Nothing is printed. |

## Reading the result

- A whole column of `✗ key` means that key is bad or expired.
- A whole row of `✗ key` next to working rows can mean the model was retired.
- `ok*` is not "has budget". It means the key works but the model is free or its price is unknown, so the tool did not verify budget.
- `? error` says nothing about budget: it is a rate limit, a server error, a timeout, a redirect or an unreadable reply. Try again later, or run [probe.md](probe.md) on that cell to see the `Detail`.

When the model list is empty the error is the same as for `models`: `Error: could not list the models (no models).` with exit 1 and nothing on stdout.

See also: [models.md](models.md), [probe.md](probe.md), [output.md](output.md), [troubleshooting.md](troubleshooting.md).
