# Output, verdicts and exit codes

Every command prints through one printer, in `table`, `json` or `yaml`. This page lists the verdicts, how a response becomes one, the exit codes, and the fields of the json and yaml output. The blocks come from the built binary against a local fake gateway with fake keys.

## Verdicts

Each probe ends in exactly one of seven verdicts. The id is what json and yaml carry. The label is what a table shows: `probe` prints the long word, `matrix` prints the short cell.

| Id | `probe` label | `matrix` cell | Meaning | Exit |
|---|---|---|---|---|
| `ok` | `✓ Ok` | `✓ ok` | The request passed on a paid model: the key has budget now. | 0 |
| `key_valid` | `✓ Valid key` | `✓ ok*` | The key works, but the model is free or its price is unknown, so budget is not verified. A cached reply also lands here. | 0 |
| `no_budget` | `✗ No budget` | `✗ budget` | The key's budget for this model is exhausted. | 1 |
| `blocked` | `⊘ Blocked by policy` | `⊘ policy` | The gateway policy blocks the model for this key. | 1 |
| `invalid_key` | `✗ Invalid key` | `✗ key` | The gateway rejected the key: 401, 403, expired or unknown. | 1 |
| `model_not_found` | `? Model not found` | `? model` | The gateway does not serve the model. | 1 |
| `error` | `? Error` | `? error` | Anything else: rate limit, server error, timeout, redirect or an unreadable reply. | 1 |

`ok` and `key_valid` are the only two that count as success. A matrix exits 1 when any cell is not one of them.

`ok*` is a statement about this moment and one model, and it is weaker than `ok`: do not report it as "has budget". A budget error can also show on free models.

### How a response becomes a verdict

The first matching row wins, and the `error.type` and `error.code` fields of the body take precedence over the HTTP status. The body is read only to decide the verdict and is never printed.

| Response | Verdict |
|---|---|
| Status 3xx | `error` |
| Status 2xx, model priced `paid`, no `X-Litellm-Cache-Key` header | `ok` |
| Any other 2xx | `key_valid` |
| `error.type` is `budget_exceeded` | `no_budget` |
| `error.type` ends in `_model_access_denied` | `blocked` |
| Status 400 or 403, `error.type` empty or `invalid_request_error`, and the message holds the gateway policy code `E003` as a separate token | `blocked` |
| `error.type` is `auth_error`, `authentication_error`, `expired_key` or `token_not_found_in_db`, or the status is 401 or 403 | `invalid_key` |
| Status 404 with a JSON `error` object, or `error.type` is `not_found_error`, or `error.code` is `model_not_found` | `model_not_found` |
| Anything else, or no response | `error` |

For an `error` verdict, the `Detail` line of `probe` names the cause by the HTTP status:

| Status | `Detail` |
|---|---|
| none (no response) | `No response from the gateway.` |
| 3xx | `Redirect not followed.` |
| 429 | `Gateway rate limit.` |
| 500 and above | `Server error.` |
| anything else | `Unexpected response.` |

## Exit codes

| Code | Meaning |
|---|---|
| 0 | The command finished and every result is `ok` or `key_valid`. `models` exits 0 when it printed its list. A command alone (`litegauge probe`) with neither `LITEGAUGE_URL` nor `LITEGAUGE_KEY` set prints its help and exits 0. |
| 1 | At least one result is another verdict, the model list failed or held no text model, or the tool hit an internal failure (`Error: internal error.`). When a result is printed, it is printed in full before the exit. |
| 2 | Usage or configuration error. stderr gets the sentence, a blank line and the command's help. Nothing was sent before the error. |
| 130 | Interrupted (Ctrl+C or SIGTERM). No result and no error sentence are printed. |

## Formats

`-o table|json|yaml`, default `table`. Any other value prints `Error: unknown format; use table, json or yaml.` and exits 2 before any request. The check runs before the URL and the keys are read.

- `table` is bordered and colored on a terminal. In a pipe, or with `NO_COLOR` set, it is plain text: columns padded to the widest cell and joined by two spaces, no border, no trailing spaces. The words are English.
- `json` is one compact line ending in a newline, no color. The values are the ids on this page.
- `yaml` is block style, no color, with the same values. Every string is double-quoted.

Each command prints in one piece, after the work finishes. A command that fails before it has a result prints nothing on stdout.

## What is never printed

The output holds only the tool's own words, key names, model ids, group names, price classes, verdicts, HTTP statuses and latencies. It never contains a key, a response body, a response header or a gateway error message. Printing goes through typed records with no free-text field, so nothing from a response has a path to the screen. See [security.md](security.md).

## JSON and YAML fields

`probe` and `matrix` print `results`. `models` prints `models`.

### `results` record

| Field | Type | Values |
|---|---|---|
| `key` | string | The key name: `env` for `LITEGAUGE_KEY`, otherwise the name from the file (`key1`, `key2` for bare keys). |
| `model` | string | The model id. |
| `group` | string | The part of the id before `/`, or else before the first `-`, in lower case. |
| `price` | string | `free`, `paid` or `unknown`. |
| `verdict` | string | One of the seven ids in the verdicts table. |
| `http_status` | integer | The HTTP status, or `0` when no response arrived. |
| `latency_ms` | integer | Milliseconds until the response or the failure. It changes from run to run. |

`probe` prints one record. `matrix` prints one record per cell, grouped by model in the order of the tables and in key order inside a model, followed by a `keys` list with the key names in file order.

```
$ litegauge probe claude-sonnet-5-5 --keys-file keys.txt --name Alpha -o json
{"results":[{"key":"Alpha","model":"claude-sonnet-5-5","group":"claude","price":"paid","verdict":"ok","http_status":200,"latency_ms":0}]}
```

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

### `models` record

| Field | Type | Values |
|---|---|---|
| `model` | string | The model id. |
| `group` | string | As above. |
| `price` | string | `free`, `paid` or `unknown`. |

```
$ litegauge models --keys-file keys.txt --name Alpha -o json
{"models":[{"model":"claude-haiku-5-5","group":"claude","price":"free"},{"model":"claude-sonnet-5-5","group":"claude","price":"paid"},{"model":"claude-opus-5-5","group":"claude","price":"paid"},{"model":"gpt-example","group":"gpt","price":"unknown"}]}
```

Field order is fixed as listed. The full per-command examples are in [models.md](models.md), [probe.md](probe.md) and [matrix.md](matrix.md).
