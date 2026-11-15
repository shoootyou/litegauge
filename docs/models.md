# models

`models` lists the text models the gateway serves to a key, grouped by family, with a price class for each. It sends no chat request, so it spends no budget. The output blocks below come from the built binary against a local fake gateway with fake keys.

## Synopsis

```
litegauge models [-o table|json|yaml] [--keys-file PATH] [--name NAME]
```

`models` takes no positional argument. It needs `LITEGAUGE_URL` and one key; see [configuration.md](configuration.md).

| Flag | Default | Meaning |
|---|---|---|
| `-o`, `--output string` | `table` | Output format: `table`, `json` or `yaml`. |
| `--keys-file string` | none | Keys file (`name=key` per line), or `-` for stdin. |
| `--name string` | none | Name of the key to use from the file. Required when the file holds more than one key. |
| `-h`, `--help` | | Prints the help. |

Every help screen starts with a `Litegauge <version>` line and a blank line. Run `litegauge models -h` for the full help.

## What it asks the gateway

1. `GET /v1/models` for the list of served ids.
2. `GET /v1/model/info`, and `GET /model/info` if the first does not answer 200 with a readable list, for each model's mode and prices.

Both calls carry the one key in the `Authorization` header. A model id that fails the model id rule in [configuration.md](configuration.md) (for example one that holds key material) is dropped from the list without a message.

## Which models are listed

A model is listed when its `model_info.mode` is `chat`, `completion` or `responses`, or when the gateway states no mode for it. Embedding and other modes are left out. Duplicate ids appear once, in the order the gateway lists them.

## The price column

The price comes from `model_info.input_cost_per_token` and `output_cost_per_token`:

| Table | JSON and YAML | When |
|---|---|---|
| `free` | `free` | Both prices are known and both are 0. |
| `paid` | `paid` | Both prices are known and at least one is above 0. |
| `unknown` | `unknown` | A price is missing, negative or not a number, or the model info could not be read. |

When several entries share a model name, the tool keeps a field only if every entry agrees on it. Otherwise the field counts as missing, so a disagreement on price gives `unknown`. If neither info route works, every model is `unknown`.

## Output

Table, in a pipe (columns joined by two spaces, no border):

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

On a terminal each group is a bordered table, and `NO_COLOR` turns the border and color off. The group is the part of the id before `/`, or else before the first `-`, in lower case. Groups appear in order of first appearance.

JSON, one compact line, a flat list. The field reference is in [output.md](output.md).

```
$ litegauge models --keys-file keys.txt --name Alpha -o json
{"models":[{"model":"claude-haiku-5-5","group":"claude","price":"free"},{"model":"claude-sonnet-5-5","group":"claude","price":"paid"},{"model":"claude-opus-5-5","group":"claude","price":"paid"},{"model":"gpt-example","group":"gpt","price":"unknown"}]}
```

YAML, block style, with the same data:

```
$ litegauge models --keys-file keys.txt --name Alpha -o yaml
models:
  - model: "claude-haiku-5-5"
    group: "claude"
    price: "free"
  - model: "claude-sonnet-5-5"
    group: "claude"
    price: "paid"
  - model: "claude-opus-5-5"
    group: "claude"
    price: "paid"
  - model: "gpt-example"
    group: "gpt"
    price: "unknown"
```

## Exit codes

| Code | When |
|---|---|
| 0 | The list was printed. |
| 1 | The listing failed or held no text model. |
| 2 | Usage or configuration error: missing URL or key, bad `-o`, a keys file with several keys and no `--name`, and so on. The sentence goes to stderr, followed by the help. |
| 130 | Interrupted. Nothing is printed. |

## Edge cases

- No text models. An empty list, or a list with only non-text entries, prints nothing on stdout and exits 1, in every output format:

  ```
  $ litegauge models --keys-file keys.txt --name Alpha
  Error: could not list the models (no models).
  ```

- The listing call fails. A non-2xx status or an unreadable body names the status, and no answer at all says `no response`. Both exit 1:

  ```
  Error: could not list the models (HTTP 401).
  Error: could not list the models (no response).
  ```

- The price class says nothing about budget. `models` never tells you whether a key can use a model; use [probe.md](probe.md) or [matrix.md](matrix.md).

See also: [probe.md](probe.md), [matrix.md](matrix.md), [output.md](output.md), [troubleshooting.md](troubleshooting.md).
