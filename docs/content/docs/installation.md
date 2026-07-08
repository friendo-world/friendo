---
title: Installation
slug: installation
group: Getting started
weight: 1
---

Friendo is a single command-line tool, `friendo`. There's nothing to configure
and no account required — install it and you can build a site immediately.

## macOS / Linux — one line

```bash
curl -fsSL https://raw.githubusercontent.com/friendo-world/friendo/main/scripts/install.sh | sh
```

This detects your OS and architecture, downloads the matching prebuilt binary
from the latest release, and installs it to a directory on your `PATH`
(`/usr/local/bin`, or `~/.local/bin` if that isn't writable).

## Manual download

Grab a binary for your platform from the
[latest release](https://github.com/friendo-world/friendo/releases/latest) —
`.tar.gz` for macOS/Linux, `.zip` for Windows — extract it, and put `friendo`
somewhere on your `PATH`.

## With Go

If you have Go installed:

```bash
go install github.com/friendo-world/friendo/cli/cmd/friendo@latest
```

The binary lands in `$(go env GOPATH)/bin` — make sure that's on your `PATH`.

## From source

```bash
git clone https://github.com/friendo-world/friendo
cd friendo
npm run build      # builds the admin UI, then compiles the binary to bin/friendo
```

## Verify

```bash
friendo --version
```

Once it's installed, head to [Your first site](/docs/first-site).
