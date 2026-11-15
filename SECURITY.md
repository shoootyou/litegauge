# Security policy

## Supported versions

The supported version is the latest release, or the latest commit on `main`. Fixes land on `main` and are not backported to older releases.

## Report a vulnerability

Do not open a public issue or pull request for a security problem.

1. Use GitHub's private vulnerability reporting for this repository: open the Security tab and choose "Report a vulnerability".
2. If that is not available to you, email `rodolfo@shoootyou.dev`.

Please include:

- the commit you ran (`git rev-parse HEAD`) and the output of `litegauge --version`;
- what you did, the output you saw and what you expected;
- the impact as you understand it, for example "a key reaches stdout";
- a reproduction against a fake gateway with fake keys. Never send a real key.

## What to expect

The goal is to acknowledge a report within a few days and to say how it will be handled after that. This is a goal for a single-maintainer project, not a service-level promise. You will be told when a fix lands, and credited if you want to be.

## What litegauge protects

A report is most useful when it shows one of these properties failing:

- A key given as a command-line argument is refused before any request.
- A key never appears in stdout, stderr, a key name or a model id.
- Nothing from a gateway response body, header or error message is printed.
- A key travels only in the `Authorization` header to the configured host, and redirects are not followed.
- The URL must be `https`, or `http` on a loopback host.

The full list and its known limits (for example the `tmux` and `/proc/<pid>/environ` notes) are in [docs/security.md](docs/security.md).
