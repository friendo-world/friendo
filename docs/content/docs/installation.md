---
title: Install
slug: installation
section: Tutorials
weight: 10
description: Get the friendo command on your machine. Two minutes.
---

Friendo is a single command-line tool, `friendo`. There's nothing to configure
and no account required: install it and you can build a site immediately.

## Get the command

Pick the way that suits your machine. Each one ends with the same `friendo` on
your `PATH`.

```bash tab="macOS / Linux" group=install
curl -fsSL https://raw.githubusercontent.com/friendo-world/friendo/main/scripts/install.sh | sh
```

```bash tab="Windows" group=install
# Download the .zip for Windows from the latest release:
#   https://github.com/friendo-world/friendo/releases/latest
# Unzip it, then put friendo.exe somewhere on your PATH, for example:
mkdir %USERPROFILE%\bin
move friendo.exe %USERPROFILE%\bin\
setx PATH "%PATH%;%USERPROFILE%\bin"
```

```bash tab="With Go" group=install
go install github.com/friendo-world/friendo/cli/cmd/friendo@latest
# The binary lands in $(go env GOPATH)/bin; make sure that's on your PATH.
```

```bash tab="From source" group=install
git clone https://github.com/friendo-world/friendo
cd friendo
npm run build      # builds the admin, then compiles the binary to bin/friendo
```

The one-line installer detects your OS and architecture, downloads the matching
prebuilt binary from the [latest release](https://github.com/friendo-world/friendo/releases/latest),
and installs it to `/usr/local/bin`, or `~/.local/bin` if that isn't writable.
You can also grab the `.tar.gz` for macOS or Linux from the same release page
and put `friendo` on your `PATH` yourself.

## Check it

```bash
friendo --version
```

> **Common mistake:** if the shell says `command not found`, the folder friendo
> was installed to isn't on your `PATH` yet. Open a new terminal window first;
> if that doesn't do it, see [Troubleshooting](/docs/troubleshooting#command-not-found).

> **What you have.** One binary that scaffolds sites, runs them, and syncs them
> with any server. Everything else in these docs uses it.

**Next:** [Your first site](/docs/first-site).
