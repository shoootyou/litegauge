# Contributing to litegauge

Thanks for helping. This page covers how to build and test, how the code is laid out, how to add a command, and how to submit a change. To report a security problem, do not open an issue: follow [SECURITY.md](SECURITY.md).

## Prerequisites

Go 1.26.7 or newer (the version in `go.mod`) and git. The only dependencies are Go modules, so there is nothing else to install.

## Build and test

```
go build -o litegauge ./cmd/litegauge
./litegauge --version
gofmt -s -l .
go vet ./...
go test -race -count=1 ./...
```

CI runs the same steps on every pull request and on pushes to `main` (`.github/workflows/ci.yml`): `gofmt -s -l .` must print nothing, then `go vet ./...`, then `go test -race -count=1 ./...`, then `go build -o /dev/null ./cmd/litegauge`. Run them locally before you push. No test calls a live gateway: every test runs against a fake one on a loopback port.

The built binary is `./litegauge`, which `.gitignore` excludes. A plain build prints `Litegauge dev` for `--version`. To stamp a version, build with `-ldflags "-X github.com/shoootyou/litegauge/internal/versions.Version=1.0.0"`, which prints `Litegauge v1.0.0`. Keys files matching `*keys*.txt`, `*keys*.json` or `*.keys`, and `.env*` files, are ignored too. Never commit a real key. Use obvious fakes such as `sk-FAKE-EXAMPLE-0001` in tests and docs.

## Layout

The layout follows [golang-standards/project-layout](https://github.com/golang-standards/project-layout). The root holds `go.mod`, `go.sum`, `README.md`, `LICENSE`, the community files and CI config. There is no `pkg/` and no `.go` file at the root.

- `cmd/litegauge/main.go`: entry point, `os.Exit(cli.Main())`.
- `internal/cli/`: root command, run loop and help; every CLI test and the `testdata/golden` and `testdata/keys` fixtures.
- `internal/cli/models/`, `internal/cli/probe/`, `internal/cli/matrix/`: one package per subcommand.
- `internal/session/`: environment and terminal state, shared flags, connecting to the gateway.
- `internal/apperr/`: error kinds and the one error-to-exit-code mapping.
- `internal/messages/messages.go`: every user-facing string, all in English. Changing the language edits this one file.
- `internal/keys/`: key loading and the rules that keep keys out of names and output.
- `internal/gateway/`: HTTP client, probe, model listing and prices.
- `internal/versions/`: the build version, the one variable the `-ldflags -X` flag sets.
- `internal/verdict/`: response to verdict.
- `internal/output/`: table, json and yaml printers.
- `internal/tui/`: spinner and progress bar.
- `skills/litegauge/SKILL.md`: the agent skill that tells an agent how to run the tool safely.

## Add a subcommand

1. Copy a sibling package, for example `internal/cli/probe/probe.go`, to `internal/cli/<cmd>/<cmd>.go`. It exports `NewCmd(s *session.Session) *cobra.Command` and calls `session.AddCommon` for `-o` and `--keys-file`.
2. In `internal/messages/messages.go`, add a `messages.Command` for it (`Use`, `Short`, a `Long` of 2 to 4 lines and an `Example` of 2 to 3 lines, each starting with `litegauge `), plus a usage string in `messages.Flags` for any new flag.
3. Register `<cmd>.NewCmd(s)` in the command list inside `newRoot` in `internal/cli/root.go`. The help lists commands in that order.
4. In `internal/cli/help_test.go`, raise the hard-coded count: `TestHelp_EveryCommandAndFlagCarriesItsDescriptions` expects exactly four names (the root and three commands), so it fails when you add a fifth. The test walks the command tree for everything else, so a new command is checked for its texts and examples with no other edit.
5. In `internal/cli/usage_test.go`, update `wantRootHelp`: the root help lists every command, so its pinned text changes. The text keeps its version line and the `--version` flags row.
6. Add tests for the new command in `internal/cli`, golden files under `internal/cli/testdata/golden/` for any new output, a page under `docs/`, a row in the `README.md` table and a row in `docs/README.md`.

The full steps are in [docs/development.md](docs/development.md).

## Rules the tests enforce

- All user-facing text lives in `internal/messages/messages.go`, the only non-test `.go` file in that package. A test scans every other production `.go` file and fails on a string literal that holds two words.
- Every command and flag has help text, and every example line in a command's help parses and resolves to that command.
- The output, the exit codes and the key-handling rules are pinned by golden files and security tests. If you change a message or an output, update the matching golden file in `internal/cli/testdata/golden/` and the docs in the same pull request.

By convention, and not by a test, the code has no `init()` function, and `versions.Version` is the only variable set from outside (with `-ldflags -X`).

## Releases

A release is made by merging the release PR that release-please keeps open; nobody creates a tag by hand, and pushing a tag starts nothing. One workflow, `.github/workflows/release-please.yml`, creates the tag and the release, attaches the archives and updates the Homebrew cask. Whoever can merge the release PR publishes. The steps are in the Releasing section of [docs/development.md](docs/development.md#releasing). CI also runs a `snapshot` job that builds the release archives on every pull request. Keep it green: if you change `.goreleaser.yaml`, the build, or the files in the archives, run the local snapshot described there.

## Commits and pull requests

Use [Conventional Commits](https://www.conventionalcommits.org/): `feat`, `fix`, `docs`, `test`, `refactor`, `ci`, `chore` and so on, for example `fix: refuse a keys file with duplicate names`. This applies to the PR title as well as to commit subjects: PRs are squash merged, so the PR title (for a one-commit PR, the commit subject) becomes the commit on `main` and the line in the changelog. The title becomes a changelog line and part of the release body, so review it like content before you merge. Only `feat`, `fix`, `perf` and `revert` appear in the release notes and open a release PR, unless the commit is a breaking change (a `!` after the type, or a `BREAKING CHANGE:` footer), which enters the notes and opens a release PR whatever its type; `feat` raises the minor version and `fix` the patch, and while the version is below 1.0.0 a breaking change raises the minor too; other `docs`, `test`, `chore`, `ci`, `build`, `refactor` and `style` commits do not. Do not edit `CHANGELOG.md`: the release PR generates it. Branch from `main` with a descriptive kebab-case name such as `feat/yaml-output`. Keep a pull request small and about one thing. Say what changed and why, and say how you checked it. A change in behavior comes with a test and a docs update.
