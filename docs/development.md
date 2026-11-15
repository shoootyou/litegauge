# Development

This page is for people who change the code. It covers the layout, how a command is built, how to add one, how the tests are organized, and how a release is made. How to submit a change is in [CONTRIBUTING.md](../CONTRIBUTING.md). Everything here describes what the code and the tests do today. Where a rule is a habit and no test enforces it, the page says so.

## Layout

The layout follows [golang-standards/project-layout](https://github.com/golang-standards/project-layout): the entry point is under `cmd/`, the code is under `internal/`, there is no `pkg/` and no `.go` file at the repository root. Build with `go build ./cmd/litegauge`.

```
cmd/litegauge/main.go        entry point, one line: os.Exit(cli.Main())
internal/apperr/             error kinds and the one mapping from an error to an exit code
internal/verdict/            one gateway response to a verdict, and the price type
internal/versions/           versions.go: the build version (Version, String), set with -ldflags -X
internal/messages/           messages.go: every user-facing string, the only non-test .go file in the package
internal/keys/               key loading, key-name and model-id rules, key-argument refusal
internal/tui/                spinner and progress bar shown while a run lasts
internal/gateway/            HTTP client, probe, model listing and prices, pacing
internal/output/             table, json and yaml printers
internal/session/            one run's inputs: environment, terminal state, shared flags, connecting
internal/cli/models/         the models command
internal/cli/probe/          the probe command
internal/cli/matrix/         the matrix command
internal/cli/                root command, run loop, the help template, and every CLI test
internal/cli/testdata/       golden/ (expected outputs) and keys/ (keys-file fixtures)
skills/litegauge/SKILL.md    the agent skill that tells an agent how to run the tool safely
.github/workflows/ci.yml     CI: checks on every pull request, plus the release snapshot
.github/workflows/release-please.yml  the one release workflow: release PR, tag, release, tests, then GoReleaser
release-please-config.json   release-please settings: Go release type, first version, pre-1.0 bump rule
.release-please-manifest.json  the last released version (empty until the first release)
.goreleaser.yaml             the release build: targets, archives, checksums, the Homebrew cask
```

### Layers

Each package imports only packages in a lower layer. The import graph follows this today, and the compiler forbids cycles, but no test checks that a package skips no layer or stays below its own.

| Layer | Packages |
|---|---|
| L0 | `apperr`, `verdict`, `versions` |
| L1 | `messages` |
| L2 | `keys`, `tui` |
| L3 | `gateway`, `output` |
| L4 | `session` |
| L5 | `cli/models`, `cli/probe`, `cli/matrix` |
| L6 | `cli` |

`session` is the only reader of the environment and of the terminal state. `output` is the only code that prints a result, and its methods take typed records, which is what keeps response text out of an output (see [security.md](security.md)).

## The subcommand template

Each command is a package that exports `NewCmd(s *session.Session) *cobra.Command`, with a private `options` struct and a `RunE` that calls a private `run`. This is `internal/cli/models/models.go`, shortened:

```go
type options struct {
	session.Common // -o and --keys-file
	name string
}

func NewCmd(s *session.Session) *cobra.Command {
	var o options
	t := messages.Models
	c := &cobra.Command{Use: t.Use, Short: t.Short, Long: t.Long, Example: t.Example, Args: cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error { return run(s, &o) }}
	session.AddCommon(c, &o.Common)
	c.Flags().StringVar(&o.name, "name", "", messages.Flags.Name)
	return c
}

func run(s *session.Session, o *options) error {
	gw, ks, key, err := s.Pick(o.Common, o.name) // validates -o, the URL and the keys, then picks one key
	if err != nil {
		return err
	}
	// ... talk to the gateway, then s.Printer(o.Output).Models(...)
	return nil
}
```

- The command text comes from a `messages.Command`, never from literals in the package.
- `s.Connect` (every key) or `s.Pick` (one key) checks the output format, the URL and the keys before anything is sent.
- `run` returns an error and never calls `os.Exit`. The only `os.Exit` in the code is in `cmd/litegauge/main.go`.
- `probe` and `matrix` run their requests through `s.Work`, which shows the spinner or the bar on a terminal and runs plain otherwise.

## Add a subcommand

1. Copy a sibling, for example `internal/cli/probe/probe.go`, to `internal/cli/<cmd>/<cmd>.go`, and adjust the options and `run`.
2. In `internal/messages/messages.go`, add a `messages.Command` for it: `Use`, a one-line `Short`, a `Long` of 2 to 4 lines and an `Example` of 2 to 3 lines, each example line starting with `litegauge `. Add a usage string to `messages.Flags` for every new flag.
3. Register `<cmd>.NewCmd(s)` in the command list inside `newRoot` in `internal/cli/root.go`. The root help lists commands in that order.
4. In `internal/cli/help_test.go`, raise the hard-coded count: `TestHelp_EveryCommandAndFlagCarriesItsDescriptions` expects exactly four names (the root and three commands), so it fails when you add a fifth. The test walks the command tree for everything else, so a new command is checked for its texts and examples with no other edit.
5. In `internal/cli/usage_test.go`, update `wantRootHelp`: the root help lists every command, so its pinned text changes. The constant starts with the `Litegauge dev` header line and has the `-h, --help` and `--version` rows; keep them.
6. In `internal/cli/version_test.go`, add the command name to the two hard-coded lists: `TestVersion_FlagPrintsTheHeaderWithoutURLOrKey` checks that each command refuses `--version`, and `TestHelp_EveryScreenStartsWithTheVersionLine` checks that each command's help starts with the version line.
7. Add tests for the new command in `internal/cli`, golden files under `internal/cli/testdata/golden/` for any new output, a page under `docs/`, a row in the `README.md` table and a row in `docs/README.md`.
8. Run the checks in the Testing section below.

## The help system

- One `messages.Command` per command in `internal/messages/messages.go`, one help template for all of them: the `help` function in `internal/cli/root.go`. The template prints the version line and a blank line, the long text, `Usage:`, the command list (root only), `Flags:`, `Examples:` and the footer (root only).
- Cobra's own help and usage output are off: `SilenceUsage`, `SilenceErrors`, no completion command and a hidden `help` command. The template is also the help that follows a usage error.
- The version line is `Litegauge <version>`, built by `header()` in `internal/cli/root.go` from the `messages.Help.Header` text and `versions.String()`. It opens every help screen, including the help that follows a usage error.
- `--version` exists on the root only. `newRoot` declares it as a flag, so cobra adds none of its own, and sets the version template to print the header line. `litegauge --version` prints `Litegauge <version>` on stdout and exits 0, without reading `LITEGAUGE_URL` or a key. On a command, `--version` is an unknown flag: exit 2 with `Error: invalid argument.`.
- `session.AddCommon` adds `-o` and `--keys-file` first, and `newRoot` adds `-h` last (and `--version` after it on the root), so the flags show in that order.
- A usage error is printed as the sentence, a blank line, then the command's help, on stderr, exit 2.
- What enforces it: `internal/cli/help_test.go` walks the tree and checks that every command has a one-line `Short`, a `Long` of 2 to 4 lines and an `Example` of 2 to 3 lines, that every flag has usage text, and that every example line starts with `litegauge `, resolves to its command and parses. `internal/cli/usage_test.go` pins the full help of the root and of `probe`, and every usage sentence. `internal/cli/version_test.go` pins `--version` and the header on every help screen. `internal/messages/messages_test.go` fails when a production `.go` file outside `internal/messages` holds a string made of two words, which keeps sentences in one file. All tool text is English, and changing the language is an edit to that one file, `internal/messages/messages.go`.

## Error handling

`apperr.Code(err)` is the only mapping from an error to an exit code and a stderr sentence. `run` in `internal/cli/run.go` calls it once.

| Error | Exit | stderr |
|---|---|---|
| `nil` | 0 | none |
| `apperr.ErrNotOK` | 1 | none: the result was already printed |
| `apperr.Fatal(sentence)` | 1 | the sentence |
| `apperr.Usage(sentence)` | 2 | the sentence, a blank line, the help |
| `apperr.ErrInterrupted` | 130 | none |
| any other error | 2 | `Error: invalid argument.` and the help |

Return one of these kinds from `run`; do not print an error or choose an exit code yourself. A panic anywhere is recovered in `run`, printed as `Error: internal error.` and exits 1, so it can never print a trace that holds a key.

## Testing

Run everything with the race detector, as CI does:

```
gofmt -s -l .
go vet ./...
go test -race -count=1 ./...
go build -o /dev/null ./cmd/litegauge
```

No test calls a live gateway. The suite has 90 top-level tests (`go test -list '.*' ./...` counts them). They are in these places:

- `internal/cli/*_test.go` drive the whole program through `run(args, session.Env, gateway.Timing)`, with no process and no real environment.
- The fake gateway harness is `internal/cli/gateway_fake_test.go`. `newFakeGateway(t, rules)` starts an `httptest` server whose `fakeRule`s (path, key, model, status, headers, body, delay, a use count) are matched in order. It records every call and the peak number of requests in flight. `runCLI(t, cliOpts, args...)` runs the command with an environment map, stdin, terminal flags and `testTiming` (no pacing, a short retry) and returns the exit code, stdout, stderr and the variables the run asked for.
- Golden files in `internal/cli/testdata/golden/` hold the expected table, json and yaml output of each command. They are written by hand: there is no update flag, so edit them with the change that alters the output. The test normalizes latency before it compares.
- `internal/cli/testdata/keys/` holds keys-file fixtures (plain, CRLF, byte order mark).
- `security_test.go` and `regression_test.go` carry the leak checks: `canaryForms` builds raw, masked, hashed, base64 and fragment forms of a fake key, and `assertNoLeak` fails if any shows up in stdout or stderr.
- `internal/gateway/gateway_test.go` tests the transport itself: HTTP/1.1 only, proxy use and the production timing values.
- `internal/versions/versions_test.go` tests `String()` (a leading `v` is added once) and builds `./cmd/litegauge` with `-ldflags -X` to check that the target names the real variable, then runs the binary with `--version`. It needs the `go` tool on `PATH`. `internal/cli/version_test.go` covers `--version` and the help header.
- Use fake keys such as `sk-FAKE-EXAMPLE-0001` and neutral names. Never put a real key in a test, a fixture or a doc.

### The mutation habit

Each guard in the code should have a test that fails when the guard is removed. When you add or change a guard, remove it, run the test, and confirm that a specific test goes red, then restore it. This is a habit of the project's tests and not an automated check.

### Conventions with no test behind them

- Package-level variables are read-only once the program starts: the sentence tables in `messages`, compiled regular expressions, the `apperr` sentinels, `gateway.ProdTiming` and `versions.Version`. No test checks that nothing writes to one.
- No `init()` function.
- `versions.Version` is the only variable set from outside the code, with `-ldflags -X`.

## Build and version

```
go build -o litegauge ./cmd/litegauge
go build -ldflags "-X github.com/shoootyou/litegauge/internal/versions.Version=1.0.0" -o litegauge ./cmd/litegauge
./litegauge --version
```

The package that owns the variable is `internal/versions`, so that is the `-X` target. `Version` is `dev` unless you set it. The raw value goes in the `User-Agent` header of the requests (`litegauge/<version>`). People see it through `String()`, which adds one leading `v` to a value that starts with a digit: the second build prints `Litegauge v1.0.0`, the first prints `Litegauge dev`. `./litegauge --version` and every help screen print that line. The built binary `./litegauge` is excluded by `.gitignore`.

## CI

`.github/workflows/ci.yml` runs on every pull request and on pushes to `main`, with read-only permissions, as two jobs. The `test` job checks out the code, sets up Go from the version in `go.mod`, and runs these steps in order: `gofmt -s -l .` (it fails if it prints anything), `go vet ./...`, `go test -race -count=1 ./...` and `go build -o /dev/null ./cmd/litegauge`. Actions are pinned to a commit SHA. The `snapshot` job builds the release archives without publishing them (see Releasing). Run the first four steps locally before you push.

## Releasing

A release happens when someone merges a release PR. Nobody creates a tag by hand, and pushing a tag starts nothing: the only workflow that publishes is `.github/workflows/release-please.yml`, and it runs on a push to `main`. [release-please](https://github.com/googleapis/release-please) picks the version and writes the changelog and the release notes. [GoReleaser](https://goreleaser.com/) v2.20.2, the version pinned in `release-please.yml` and `ci.yml`, builds the archives, attaches them to the release and updates the Homebrew cask. The build is described in `.goreleaser.yaml`.

### How a release happens

1. Changes reach `main` by squash merge.
2. The `release-please` job runs on every push to `main`. It opens a release PR titled `chore(main): release X.Y.Z` and keeps it updated as more commits land. The PR updates `CHANGELOG.md` and `.release-please-manifest.json`.
3. Merging that PR makes the same job create the tag `vX.Y.Z` and the GitHub release, titled `vX.Y.Z`. The release body is the new `CHANGELOG.md` section, `## [X.Y.Z](compare link) (date)`, with `### Features` and `### Bug Fixes` and one line per commit, linked to its PR and commit.
4. The `test` job of the same run starts when a release was created. It checks out the tag and runs `go test -count=1 ./...`. It has read-only access and no secret.
5. The `goreleaser` job starts when `test` has passed. In order, it checks out the tag, checks that the tag commit belongs to the history of `main` (see Who can publish), sets up Go, loads the tap token from 1Password and runs GoReleaser. Both jobs check out the tag by its qualified ref, `refs/tags/<tag>`, because a plain name resolves to a branch first. GoReleaser attaches the six archives and the checksums file to the existing release and keeps its title and body (`changelog: disable: true` in `.goreleaser.yaml`, and `release.mode` left at its default, `keep-existing`). It also pushes the cask `Casks/litegauge.rb` to the tap `shoootyou/homebrew-tap`. A tag with a suffix is marked as a prerelease (`prerelease: auto`) and gets no cask (`skip_upload: auto`).

The archives appear a few minutes after the release does. In between, `/releases/latest` exists and has no assets.

The three jobs live in one workflow because a tag or a release created with `GITHUB_TOKEN` starts no other workflow. A separate tag-triggered workflow would never run.

Only `feat`, `fix`, `perf` and `revert` commits appear in the notes and open a release PR, unless the commit is a breaking change: a breaking change of any type, hidden ones such as `docs!:` or `chore!:` included, enters the notes and opens a release PR. Other `docs`, `test`, `chore`, `ci`, `build`, `refactor` and `style` commits do not, so a docs-only change on `main` releases nothing. `feat` raises the minor version and `fix` the patch. While the version is below 1.0.0, a breaking change (a `!` after the type, or a `BREAKING CHANGE:` footer, on a commit of any type) also raises the minor version, because `release-please-config.json` sets `bump-minor-pre-major`: from 0.1.0 a breaking change proposes 0.2.0, not 1.0.0. No `feat`, `fix` or breaking change reaches 1.0.0. To force it, put the footer `Release-As: 1.0.0` on a commit.

`CHANGELOG.md` is generated by the release PR. Do not edit it by hand.

### The first release

The first release is `v0.1.0`. `initial-version` in `release-please-config.json` and an empty manifest (`{}`) in `.release-please-manifest.json` make release-please propose `0.1.0`; without them it would propose `1.0.0`. There is no earlier tag, so the first release body has no compare link, and `CHANGELOG.md` does not exist until the first release PR creates it.

### What `release-please.yml` does

The workflow sets `permissions: {}`, and each job asks for its own permissions.

- `release-please` uses `GITHUB_TOKEN` with Contents, Pull requests and Issues write (issues for the `autorelease:` labels). It has a 10-minute timeout.
- `test` runs only when a release was created, with `contents: read` and no secret. It has a 15-minute timeout. It is a separate job because jobs share neither `GITHUB_ENV` nor `PATH`: test code cannot change the environment of the job that holds the 1Password token. If it fails, `goreleaser` is skipped.
- `goreleaser` needs `release-please` and `test` and has Contents write. The tap token is not a GitHub secret. The only secret the workflow needs besides `GITHUB_TOKEN` is `OP_SERVICE_ACCOUNT_TOKEN`, a repository secret holding a 1Password service account that can read the vault with the tap token. After the checkout, the guard and the Go setup, the job loads the token with `1password/load-secrets-action` from `op://github-apps/homebrew-tap/token`, then runs GoReleaser. The action has `export-env` off, so the token is a masked step output, and only the GoReleaser step receives it, as the environment variable `TAP_GITHUB_TOKEN` that `.goreleaser.yaml` reads. The action downloads the `op` CLI without a checksum check, so the workflow pins the CLI version (2.42.0). The job restores no Go cache (`cache: false`), because it holds these tokens and runs on `main` can write the cache.

If the `test` job fails, `goreleaser` does not run: no archive is attached and the cask is not pushed. The release exists, with its notes and no assets, until you re-run the failed jobs.

The `release-please` and `goreleaser` jobs each sit in a concurrency group that never cancels a running run (`release-please` and `release`). A run that is still waiting is another matter: GitHub keeps one waiting run per group, so a newer one replaces it. For `release-please` that is harmless, because the newer run computes the release PR again from `main`. For `goreleaser` the replaced run is cancelled and its release gets no archives and no cask, so re-run it from the Actions tab. That takes two releases in quick succession.

A release PR opened with `GITHUB_TOKEN` is opened by `github-actions[bot]`, so a maintainer can approve it. GitHub's documentation ("Trigger a workflow") says that events triggered by `GITHUB_TOKEN` do not create a new workflow run, except `workflow_dispatch` and `repository_dispatch`, and `pull_request` events of type `opened`, `synchronize` or `reopened`. Those create runs in an approval-required state, which a user with write access starts with "Approve workflows to run"; other `pull_request` types, such as `labeled` or `edited`, create none. The release PR only changes `CHANGELOG.md` and `.release-please-manifest.json`, and the repository rules do not require its checks, so you can merge it without running them. After any squash merge to `main`, the bot updates its open release PR, after which an earlier approval may no longer apply if the repository dismisses stale approvals. Re-check the head of the release PR before you approve and merge it.

If the `test` or the `goreleaser` job fails, use "Re-run failed jobs" on that workflow run. The tag must still be on `main` when you do. Two failures of `goreleaser` are known:

- 1Password is unreachable, so the token is not loaded. The release already exists without archives or cask. Re-run the failed jobs once 1Password is back.
- The tap token expired, so the push of the cask fails after the archives were attached. Rotate the token in 1Password, then re-run the failed jobs. `replace_existing_artifacts: true` lets GoReleaser replace the files already on the release. After any re-run, compare the new `litegauge_<version>_checksums.txt` with the first one. If a line changed, anyone who downloaded that archive earlier holds a different file.

Keep GitHub's "immutable releases" setting off. This flow is not compatible with it: release-please publishes the release first and GoReleaser attaches the archives afterwards, and GoReleaser stops when the release is immutable.

### One-time repository setup

These are the settings the repository needs before the first merge of the workflow to `main`.

- [ ] Settings, Actions, General, Workflow permissions: turn on "Allow GitHub Actions to create and approve pull requests". Without it `GITHUB_TOKEN` cannot open the release PR.
- [ ] Create the repository secret `OP_SERVICE_ACCOUNT_TOKEN` (Settings, Secrets and variables, Actions): the token of a 1Password service account of this repository. Give it read access only (`read_items`, no write or share permission) and an expiry for its token if your plan allows one. Its access is per vault and cannot be limited to one item, so it reads every item of the vault it can access.
- [ ] Store the tap token in 1Password at `op://github-apps/homebrew-tap/token`: a fine-grained personal access token limited to the repository `shoootyou/homebrew-tap`, Contents read and write, with a short expiry. It can write every cask of the tap, so use a token made only for this and put its rotation date in your calendar. Rotate it before it expires.
- [ ] The tap repository `shoootyou/homebrew-tap` is public and has a `main` branch.
- [ ] Immutable releases are off.
- [ ] The ruleset on `main` (signed commits, an approval, squash merges only) is in place before the workflow reaches `main`. It is what protects publication.

### Commit titles

The release notes come from the commits on `main`. PRs are squash merged, so the PR title becomes the commit on `main` and then the changelog line (for a one-commit PR, the commit subject). It must be a [Conventional Commit](https://www.conventionalcommits.org/), such as `fix: refuse a keys file with duplicate names`. Review the title like content before you merge: it becomes a changelog line and part of the release body.

### How the version is set

GoReleaser injects the tag without its leading `v` as `-X github.com/shoootyou/litegauge/internal/versions.Version={{ .Version }}`. The target is `internal/versions` because that package owns the variable. `versions.String()` adds the one `v` that people see, so the tag `v0.1.0` gives `Litegauge v0.1.0` and a raw `0.1.0` in the `User-Agent` header.

### Who can publish

Whoever can merge the release PR publishes a release, because only `main` publishes. The ruleset on `main` (signed commits, squash merge only, an approval) is what protects publication. A tag pushed by hand starts nothing, so no tag ruleset is needed. The `goreleaser` job also runs `git merge-base --is-ancestor HEAD origin/main` right after the checkout: the commit of the tag must be an ancestor of, or equal to, `origin/main`, the `main` that the checkout fetched. It is not compared with the commit that triggered the run, so a release PR merged while an earlier run was still working passes. A tag pushed by hand to a commit that is not in the history of `main` makes the job fail before it publishes anything. Rulesets are repository settings and are not in the tree.

### What a release holds

Six binaries (linux, darwin and windows, each on amd64 and arm64), built with `CGO_ENABLED=0` and `-trimpath`. Each goes into `litegauge_<version>_<os>_<arch>.tar.gz` (`.zip` on windows) with `LICENSE` and `README.md`, next to `litegauge_<version>_checksums.txt` with a SHA-256 line per archive. The cask `litegauge` in the tap installs the binary from the darwin and linux archives; the binary is unsigned, so the cask removes the macOS quarantine attribute. Releases carry no signatures or SBOM yet. Users install from them as described in [getting-started.md](getting-started.md).

### The snapshot job

The `snapshot` job in `ci.yml` runs on every pull request and on pushes to `main`, with `contents: read`. It runs `goreleaser release --snapshot --clean --skip=publish`, which builds the same archives and publishes nothing, so a broken `.goreleaser.yaml` fails in the pull request and not at release time. A snapshot is versioned as the next patch with a `-devel` suffix. It must stay green.

### Try a build locally

With GoReleaser v2.20.2 installed, from the repository root:

```
goreleaser check
goreleaser release --snapshot --clean --skip=publish
```

`goreleaser check` validates the config. The second command writes the archives and the checksums file to `dist/`, which `.gitignore` excludes. Unpack the archive for your system and run `litegauge --version`: on a checkout whose latest tag is `v0.1.0` it prints `Litegauge v0.1.1-devel`. Run the commands from a clean tree, because GoReleaser refuses a dirty one.

## Commits

Use [Conventional Commits](https://www.conventionalcommits.org/): `feat`, `fix`, `docs`, `test`, `refactor`, `ci`, `chore` and so on, for example `docs: describe the matrix pacing`. The PR title is the line that reaches the changelog (see Commit titles under Releasing). A change in behavior comes with a test and a docs update. The rest of the process is in [CONTRIBUTING.md](../CONTRIBUTING.md).
