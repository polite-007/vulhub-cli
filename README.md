# vulhub-cli

A command-line tool for managing the lifecycle of [vulhub](https://github.com/vulhub/vulhub) environments.

[![CI](https://github.com/polite-007/vulhub-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/polite-007/vulhub-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/polite-007/vulhub-cli)](https://github.com/polite-007/vulhub-cli/releases/latest)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

**English** · [简体中文](README.zh-CN.md)

## Overview

[vulhub](https://github.com/vulhub/vulhub) provides a large collection of pre-built vulnerable environments, each one a Docker Compose project. Working with them by hand does not scale: finding out what is available means walking the directory tree, starting one means changing into its directory and invoking `docker compose` directly, and once several are running there is no single view of what is up.

vulhub-cli maintains a local checkout of vulhub and exposes commands to list, search, start, stop and destroy environments.

An environment can be referenced either by its **path** (`activemq/CVE-2023-46604`) or by a short **number** (`68`). Numbers are assigned once and never change afterwards, so they are safe to memorize, to script against, and to pass on to a colleague.

Environments come from **two sources**, distinguished by the path prefix:

- `activemq/CVE-2023-46604` — from the [vulhub](https://github.com/vulhub/vulhub) repository
- `vulfocus/drupal-cve_2018_7600` — from the [`vulfocus`](https://hub.docker.com/u/vulfocus) namespace on Docker Hub

Both share a single numbering space, so `68` means the same thing regardless of source.

## Requirements

- **Linux, amd64.** No other platform is currently supported.
- `git`
- Docker, with either `docker compose` (Compose v2) or `docker-compose` (Compose v1)

`vulhub init` checks for all of these and reports specifically which one is missing.

vulfocus environments are pulled from Docker Hub when you start them, so those need registry access. vulhub environments do not.

## Installation

Download the binary from the [latest release](https://github.com/polite-007/vulhub-cli/releases/latest) and put it on your `PATH`:

```sh
curl -LO https://github.com/polite-007/vulhub-cli/releases/latest/download/vulhub-cli-linux-amd64
curl -LO https://github.com/polite-007/vulhub-cli/releases/latest/download/SHA256SUMS
sha256sum -c SHA256SUMS

chmod +x vulhub-cli-linux-amd64
sudo mv vulhub-cli-linux-amd64 /usr/local/bin/vulhub
```

The asset names carry no version, so these URLs are stable across releases; the version is the release tag. The binary is statically linked and has no runtime dependencies.

## Quick start

```sh
vulhub init              # clone vulhub into ~/vulhub and assign numbers
vulhub search log4j      # find an environment by title or path
vulhub up 68             # start number 68
vulhub status            # see what is currently running
vulhub stop 68           # stop it, keeping the containers
vulhub del 68            # destroy it, keeping the volumes
```

## Command reference

`<environment>` accepts either a number (`68`) or a path (`activemq/CVE-2023-46604`).

| Command | Target | Description |
| --- | --- | --- |
| `init` | — | Clone vulhub into `~/vulhub` and assign numbers |
| `update` | — | Update the local vulhub checkout |
| `ls [--json] [-time]` | — | List all environments |
| `search <keyword>… [--json] [-time]` | — | Search by vulnerability title or environment path |
| `up <environment>[,…]` | one or many | Start environments |
| `stop <environment>[,…]` | one or many | Stop environments |
| `del <environment>` | one | Destroy an environment |
| `status [--json]` | — | Show what is currently running |
| `pull <environment>[,…] \| --all` | one or many | Pull images ahead of time |
| `help` | — | Print usage |

Comma-separated lists are accepted by `pull`, `up` and `stop`. `del` is the only irreversible operation and accepts exactly one environment.

`-time` sorts `ls` and `search` by creation time, newest first. It changes only what you see — **never the numbers**, which are assigned once in path order and stay put. Environments with no known creation time (all of vulhub, since its registry carries no dates) sort last.

Options for `del`:

- `-a` — also remove the environment's volumes and the images it references
- `-y` — skip the confirmation prompt

### Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | Runtime failure — unknown environment, Docker error, `search` with no matches |
| `2` | Usage error — wrong number of arguments, unknown option |

## How it works

**The vulhub checkout** lives at `~/vulhub`. `init` creates it with a shallow clone; `update` maintains it with `git pull`. It is treated as read-only — edits you make there will conflict with `update`, which fails loudly rather than discarding your changes.

**The index** lives at `~/.local/share/vulhub-cli/index.json`, deliberately outside the checkout, which is a git repository. It maps environment paths to numbers. Numbers are assigned lexicographically at `init` time, never change afterwards, and new environments are appended. Numbers of environments that disappear are retired and never reused. If the index is lost or corrupted it is rebuilt automatically and you are warned that previously assigned numbers are no longer valid — which is why the path, not the number, is the environment's identity.

**Environment identification** uses the labels Docker Compose applies to every container it creates (`com.docker.compose.project` and `com.docker.compose.project.working_dir`), not container names. Official vulhub Compose files generally do not set `container_name`, so container names are Compose-generated and parsing them would depend on Compose's internal naming. The trade-off is that `status` only sees containers created by Compose.

**Environment metadata** is read from `environments.toml`, vulhub's own environment registry. If that file is missing or unparsable, the tool falls back to scanning the directory tree, in which case titles are unavailable and only paths can be searched.

**The vulfocus source** works differently. Its upstream ships only Docker images — no Compose file, no startup command, no documentation of any kind. So the tool synthesizes one: the first time you start a vulfocus environment, a Compose file is written to `~/.local/share/vulhub-cli/vulfocus/<image>/docker-compose.yml`. The images carry their own entrypoint, so no command is needed; every port the image exposes is published on the same host port, which means `vulhub up` will print several for images like Jenkins (50000 is an agent port, not the web UI). That synthesized file is derived data — delete it and it comes back.

The vulfocus image list and its ports are **embedded in the binary**, so `init` never contacts Docker Hub. Maintainers refresh it with `vulhub gen-vulfocus` before a release; see [ADR 0002](docs/adr/0002-embedded-vulfocus-catalog.md).

## Building from source

```sh
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o vulhub .
```

`CGO_ENABLED=0` is required, not optional: the deliverable is a static binary, and leaving cgo enabled makes the cross-compile reach for a C toolchain for the target platform.

## Releasing

Artifacts are built by GitHub Actions. Linux amd64 only, for now.

- **Pushing a branch or opening a pull request** runs `.github/workflows/ci.yml`: `gofmt`, `go vet`, `go test`, plus a linux-amd64 build uploaded as a workflow artifact.
- **Pushing a `v*` tag** runs `.github/workflows/release.yml`: builds the static binary, generates `SHA256SUMS`, and publishes a GitHub Release with both attached.

```sh
git tag v0.1.0
git push origin v0.1.0
```

Both workflows assert that the artifact is `statically linked`. Supporting another platform means adding to the `GOOS`/`GOARCH` matrix in those two files; the code itself has no platform assumptions.

### Refreshing the vulfocus catalog

The embedded list of vulfocus images goes stale as a release ages. To regenerate it:

```sh
DOCKERHUB_USERNAME=<you> DOCKERHUB_TOKEN=<pat> vulhub gen-vulfocus
git add internal/vulfocus/data/vulfocus.json && git commit
```

Credentials are required because Docker Hub rejects pagination past offset 100 for anonymous requests, and the namespace holds several hundred images. A read-only token is enough. Without them the command fails rather than writing a truncated list — a catalog that looks complete but is missing most of its entries is far harder to notice than an error.

## Development

```sh
go test ./...
```

All tests enter through a single seam, `cli.Run`: they supply command-line arguments and assert on the output streams, the external dependencies that were invoked, and the exit code. Only three dependencies are replaced by fakes — orchestration, git, and host IP discovery — because the real implementations would need a running Docker daemon, several hundred megabytes of network transfer, and a machine-specific network configuration respectively. The filesystem and the `environments.toml` parser use their real implementations against temporary directories, so the tests exercise genuine file I/O and genuine parsing.

## Documentation

- [design.md](design.md) — the full design, including what is deliberately **not** built
- [docs/spec.md](docs/spec.md) — the specification for the first release
- [CONTEXT.md](CONTEXT.md) — the domain glossary
- [docs/adr/](docs/adr/) — architecture decision records

## License

Released under the [MIT License](LICENSE).
