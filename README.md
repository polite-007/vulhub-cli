# vulhub-cli

A command-line tool for managing the lifecycle of [vulhub](https://github.com/vulhub/vulhub) environments.

[![CI](https://github.com/polite-007/vulhub-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/polite-007/vulhub-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/polite-007/vulhub-cli)](https://github.com/polite-007/vulhub-cli/releases/latest)

**English** · [简体中文](README.zh-CN.md)

## Overview

[vulhub](https://github.com/vulhub/vulhub) provides a large collection of pre-built vulnerable environments, each one a Docker Compose project. Working with them by hand does not scale: finding out what is available means walking the directory tree, starting one means changing into its directory and invoking `docker compose` directly, and once several are running there is no single view of what is up.

vulhub-cli maintains a local checkout of vulhub and exposes commands to list, search, start, stop and destroy environments.

An environment can be referenced either by its **path** (`activemq/CVE-2023-46604`) or by a short **number** (`68`). Numbers are assigned once and never change afterwards, so they are safe to memorize, to script against, and to pass on to a colleague.

## Requirements

- **Linux, amd64.** No other platform is currently supported.
- `git`
- Docker, with either `docker compose` (Compose v2) or `docker-compose` (Compose v1)

`vulhub init` checks for all of these and reports specifically which one is missing.

## Installation

Download the binary from the [latest release](https://github.com/polite-007/vulhub-cli/releases/latest) and put it on your `PATH`:

```sh
# Set this to the latest tag shown on the releases page.
VERSION=v0.1.0

curl -LO "https://github.com/polite-007/vulhub-cli/releases/download/${VERSION}/vulhub-cli-${VERSION}-linux-amd64"
curl -LO "https://github.com/polite-007/vulhub-cli/releases/download/${VERSION}/SHA256SUMS"
sha256sum -c SHA256SUMS

chmod +x "vulhub-cli-${VERSION}-linux-amd64"
sudo mv "vulhub-cli-${VERSION}-linux-amd64" /usr/local/bin/vulhub
```

The binary is statically linked and has no runtime dependencies.

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
| `ls [--json]` | — | List all environments |
| `search <keyword>… [--json]` | — | Search by vulnerability title or environment path |
| `up <environment>[,…]` | one or many | Start environments |
| `stop <environment>[,…]` | one or many | Stop environments |
| `del <environment>` | one | Destroy an environment |
| `status [--json]` | — | Show what is currently running |
| `pull <environment>[,…] \| --all` | one or many | Pull images ahead of time |
| `help` | — | Print usage |

Comma-separated lists are accepted by `pull`, `up` and `stop`. `del` is the only irreversible operation and accepts exactly one environment.

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

No license has been selected for this project yet. Until one is added, the default is that all rights are reserved — which is not the intent for a project published here, but it is the legal reality. If you intend to use or contribute to this software, please open an issue to get that resolved first.
