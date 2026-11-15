# Security

litegauge handles API keys, so most of its design is about keeping them from going anywhere they should not. This page lists each protection, how it is checked and where it stops. To report a vulnerability, follow [SECURITY.md](../SECURITY.md).

The protections are numbered S1 to S11 after the tests that pin them. Command output below comes from the built binary against a local fake gateway with fake keys.

## Properties

| | Property | What it means |
|---|---|---|
| S1 | The key is never a command-line argument | `--key`, `--api-key`, `--apikey`, `--token`, `--secret`, `--password`, `--auth` and any short flag starting with `-k` are refused. So is any argument shaped like a key (an `sk-` run of 8 or more characters, a bare 64-character hex string) or equal to a loaded key. The run exits 2 before any request and never echoes the argument. Cobra's hidden completion commands (`__complete`) are refused in any position, and a flag parse error prints one fixed sentence instead of the flag text. |
| S2 | Nothing from a response is printed | Output goes through typed records that have no free-text field. A response body, header or error message never reaches stdout or stderr, in any verdict and any `-o` format. The body of a failure is read only to pick the verdict. |
| S3 | The key goes to one place | The key travels in the `Authorization` header of requests to the configured URL, and nowhere else. |
| S4 | No redirects | A 3xx answer is returned as it is and becomes an `error` verdict with `Redirect not followed.`. A redirect would carry the key to another host. |
| S5 | HTTP/1.1 only | The transport offers only `http/1.1`, in the protocol setting and in the TLS negotiation. An HTTP/2 debug log (`GODEBUG=http2debug`) could print the `Authorization` header. |
| S6 | Bounded reads | An error body is read up to 64 KiB, and a model list or model info up to 1 MiB. |
| S7 | The key leaves the environment | When `LITEGAUGE_KEY` is the key source, the tool unsets it right after reading it. |
| S8 | A crash prints no trace | A panic in the main path, a worker, the key-reading goroutine or the terminal interface ends as `Error: internal error.` with exit 1. A panic value could hold text from a file or a response. |
| S9 | Names and model ids never carry a key | See [Screening](#screening-names-and-model-ids). |
| S10 | Transport rules | The URL must be `https`, or `http` only on `localhost`, `127.0.0.0/8` or `::1`, checked on the literal host with no DNS lookup. An `http` URL never goes through a proxy, so a proxy never sees a key in clear. An `https` URL honours the standard proxy variables. |
| S11 | Interrupt is quick and silent | Ctrl+C or SIGTERM ends the run with exit 130 and no output, within 2 seconds in the tests, including while the tool waits on a keys file with no writer. |

Also true, and checked by reading the code: the tool starts no subprocess of its own and writes no file.

## Key sources

- A keys file or stdin wins over `LITEGAUGE_KEY`. The file format and limits are in [configuration.md](configuration.md).
- A line that breaks a rule is reported by number only, never by its text. A missing, oversized or unreadable file gives one fixed sentence.
- A key is 8 to 512 characters from `A-Za-z0-9._~+/=-`.
- The tool does not check the mode of the keys file. Run `chmod 600` on it.
- The tool prints a key's name, never its value. A name is the file's name for it, or `env`, or `key1`, `key2`... for bare keys.

## Screening names and model ids

A name or a model id can end up on screen, so the tool refuses one that could carry a key. A value is refused when it:

- holds an `sk-` run of 8 or more characters, in any letter case;
- is, or holds, a bare run of exactly 64 hex characters, which is the shape of a sha256 key hash;
- holds a 6-character window of a loaded key's body (the key without a leading `sk-`), compared in any letter case;
- holds an 8-hex-character window of the sha256 of a loaded key, in any letter case.

It applies to the model id given to `probe` and `--models`, to the ids the gateway lists (a listed id that fails is dropped), and to an echoed `--name` value. A refused `--name` gets the generic `Error: invalid argument.` instead of the name. A name in the keys file is refused, by line number, when it holds an 8-character window of any loaded key.

```
$ litegauge models --keys-file two.txt --name Xample-Y
Error: invalid argument.
$ litegauge models --keys-file nm.txt
Error: line 1 of the keys file is not valid.
```

Each error line is followed by a blank line and the command's help (which starts with the `Litegauge <version>` line), on stderr; exit code 2. The first run asks for the name `Xample-Y`, which is not in the file and holds the window `xample` of the loaded key `FAKE-EXAMPLE-0001`, so the name is not echoed; the second has a key name `FAKE-EXAMPLE` in its first line, which holds a window of its own key. Other encodings of a key, base64 for example, or a window shorter than the ones above, are not screened.

## Keys in arguments and redirects

S1 and S4 were run with the built binary against a loopback fake gateway that answers every request with a 302 to a second path on the same server, and logs each request. The key is a fake one in `LITEGAUGE_KEY`.

```
$ litegauge probe claude-sonnet-5-5
Key     env
Model   claude-sonnet-5-5 (unknown)
Result  ? Error  HTTP 302 · 0 ms
Detail  Redirect not followed.
$ echo $?
1
```

The gateway log lists `GET /v1/model/info`, `GET /model/info` and `POST /v1/chat/completions`, each answered with the 302. No request for the redirect target arrived. `TestS4_RedirectsAreReturnedNotFollowedForEveryMethod` checks the same with a second gateway as the target.

Each of these forms printed the sentence below on stderr, then a blank line and the command's help, and exited 2: `--key sk-FAKE-EXAMPLE-0002`, `-ksk-FAKE-EXAMPLE-0002`, a bare `sk-FAKE-EXAMPLE-0002`, and a 64-character hex string as the model. The gateway log stayed empty through all of them and the two `__complete` runs below.

```
$ litegauge probe --key sk-FAKE-EXAMPLE-0002
Error: the key does not go on the command line; use LITEGAUGE_KEY or --keys-file.
```

`__complete` as an argument (`litegauge probe __complete probe`) and as the command (`litegauge __complete ""`) print `Error: invalid argument.` and exit 2 instead. The tests are `TestS1_KeyFlagsAreRejectedBeforeAnyRequestWithoutEcho`, `TestS1_ArgumentsShapedLikeKeysAreRejectedBeforeAnyRequestWithoutEcho` (both in `security_test.go`) and `TestS1_CompletionCommandsAreRefusedInAnyArgumentPosition` (`regression_test.go`).

## Threat model

### What litegauge protects against

- A key reaching a command line, and so the shell history, the process list and an agent's command log.
- A key reaching stdout or stderr, and so a terminal, a pipe, a log file or a pasted report. The output carries only the tool's words, key names, model ids, verdicts, statuses and latencies.
- A key going to a host other than the configured one through a redirect, or in clear through a proxy.
- A key appearing in a debug log of the HTTP/2 stack, in a crash trace, or in an echoed flag or name.
- A hostile or buggy gateway steering the output: its text is never printed, and its listed model ids are screened.
- A runaway response: reads are capped.

### What it does not protect against

- A process or user with access to your account. It can read the keys file, the process memory and, on Linux, `/proc/<pid>/environ`. Unsetting the variable after reading changes the process's own view but does not rewrite the block `/proc/<pid>/environ` shows.
- A terminal multiplexer. Under tmux, on a terminal, the terminal library runs `tmux info` before the tool can unset `LITEGAUGE_KEY`, and that child process sees the variable. Under tmux use `--keys-file` or stdin.
- The way you type the key. `LITEGAUGE_KEY=... litegauge ...` on one line puts the key in your shell history. Use a silent prompt, `read -rs LITEGAUGE_KEY; export LITEGAUGE_KEY`, or a keys file.
- A wrong URL. The gateway you configure receives the key, so a mistyped or malicious `LITEGAUGE_URL` that is `https` still gets it. The scheme rule protects the transport, not the destination.
- Trust in the certificate store. TLS uses the system roots.
- Spending. Every probe is a real request that costs a little budget, and nothing in the tool limits how often you run it.
- A weak keys file. The mode is not checked, and a keys file in a shared or synced folder is your risk.
- Encodings the screening does not cover (see above) and anything outside the tool, such as scrollback or a screenshot of a terminal.
- Windows. The terminal check is a Unix heuristic.

## What the tests pin

The properties above are each covered by a test in the repository: `internal/cli/security_test.go` (S1 to S4, S6 to S9 and S11), `internal/cli/regression_test.go` (completion commands, a panic in the key-reading goroutine, name screening), `internal/gateway/gateway_test.go` (S5 and the proxy rules of S10), `internal/cli/usage_test.go` (the URL scheme rules of S10), `internal/cli/loopback_test.go` (the accepted and refused loopback sets of S10) and `internal/cli/limits_test.go` (the read caps of S6 and the HTTP status boundaries of the models listing, `model_info` and the probe retry). The main leak check feeds canary forms of the key (the raw key, masked tails, the sha256, `Bearer` forms, base64 and a middle fragment) through every scenario and format and fails if any reaches an output.

## Not reproduced here

- Under tmux, the `tmux info` child: tmux is not installed in the environment where this page was checked, so the claim comes from the repository's earlier documentation and is not reproduced.
- HTTP/1.1 only and the proxy rules: checked through the unit tests, not by running the binary against a TLS server or a proxy.
- The one-second retry after a transport error or a 5xx answer: checked through the tests, not by running the binary.
- The 2-second interrupt bound on an idle keys file: checked through the tests. Running the binary against a gateway that never answers and sending SIGINT gave exit 130 and no output.
- `/proc/<pid>/environ`: reproduced. With the binary waiting on a gateway that never answers, the environ block of the process still listed `LITEGAUGE_KEY`.
- S3, the key only in the `Authorization` header to the base host: `TestS3_KeyTravelsOnlyInTheAuthorizationHeaderToTheBaseHost` records every request of a probe and checks the header, the path, the query and the body. The binary was not run to look for the key in other places.
- S7, `LITEGAUGE_KEY` unset after reading: `TestS7_KeyLeavesTheProcessEnvironmentOnceRead` checks the tool's own environment. From outside the process the unset cannot be seen, because `/proc/<pid>/environ` still lists the variable (above).
- S8, a panic ends as `Error: internal error.`: `TestS8_PanicOnTheProbePathEndsAsInternalError`, `TestS8_PanicInAMatrixWorkerEndsAsInternalError`, `TestS8_PanicInTheTUIWorkEndsAsInternalError` (all in `security_test.go`) and `TestS8_PanicInTheKeyReadingGoroutineEndsAsInternalError` (`regression_test.go`). The binary cannot be made to panic without changing the code.
- S2, no response text in any verdict and `-o` format: `TestS2_ProbeNeverPrintsCanariesOrGatewayTextInAnyScenarioAndMode` and `TestS2_MatrixAndModelsNeverPrintCanariesOrGatewayTextInAnyMode`.
- S6, the 64 KiB and 1 MiB read caps: `TestS6_ErrorBodyIsReadUpTo64KiBOnly` and `TestS6_ModelsListingIsReadUpTo1MiBOnly` in `security_test.go`. `limits_test.go` pins the exact boundaries: `TestLimits_ProbeErrorBodyCapIsExactly64KiB`, `TestLimits_ModelsListingCapIsExactly1MiB` and `TestLimits_ModelInfoCapIsExactly1MiBAndACutAnswerFallsThroughToTheNextPath`. It also pins the status rules around them: `TestLimits_ModelInfoTakesOnlyA200`, `TestLimits_ModelsListingTakesOnly2xx` and `TestLimits_ProbeRetriesOnlyFromStatus500Up`.
- S9, screening of model ids (the `--name` case is run above): `TestS9_ModelIDsCarryingAKeyWindowOrHashAreNeverPrintedOrProbed`.
- S10, the `http` exception: the rule accepts `localhost` in any letter case, any address in `127.0.0.0/8` and `::1`, with or without a port. Every other host must use `https`. The rule compares the literal host and makes no DNS lookup, so `localhost.` (trailing dot), `[::ffff:127.0.0.1]` (IPv4-mapped) and `localtest.me` are refused. `TestLoopback_HTTPOnALoopbackHostIsAcceptedAndReachesTheTransport` covers the accepted hosts (`localhost`, `LocalHost:8080`, `127.0.0.1`, `127.255.255.254`, `[::1]:8080`, among others). `TestLoopback_HostsNextToTheLoopbackSetAreRefusedBeforeAnyRequest` (both in `loopback_test.go`) covers `128.0.0.1`, `126.255.255.255`, `[::2]`, `0.0.0.0` and the three refused forms above. `usage_test.go` covers `evillocalhost`, `notlocalhost` and `localhost.evil.com`. The binary was run only against `127.0.0.1` and a refused `http://example.com`.
