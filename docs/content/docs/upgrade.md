---
title: Upgrade friendo
slug: upgrade
section: Guides
topic: Hosting
weight: 60
description: Replace the friendo binary with a newer one, and what happens to your site's database when you do.
---

To upgrade friendo, run `friendo upgrade` in your site folder and restart. It backs
up your database, then replaces the binary; the database updates itself on start.

```bash
friendo upgrade           # the latest release
friendo upgrade --check   # just see whether there's a newer one
```

`friendo upgrade` arrived in v0.7. On an older friendo, or one you installed with
`go install`, follow the steps below.

## 1. Check your version

```bash
friendo --version
```

## 2. Back up first

A site's data is one file, `data/friendo.db`, plus `assets/`. The database is
SQLite in WAL mode, so copy it with `sqlite3`, or stop the server first:

```bash
sqlite3 data/friendo.db ".backup data/friendo-backup.db"
```

On a network, back up the whole data volume (`/data` in the Docker image): every
site's folder and database, and `.network/accounts.db`.

## 3. Install the new binary

Use the same way you installed it. Each one replaces the old binary in place:

```bash tab="Installer" group=install
curl -fsSL https://raw.githubusercontent.com/friendo-world/friendo/main/scripts/install.sh | sh
```

```bash tab="Go" group=install
go install github.com/friendo-world/friendo/cli/cmd/friendo@latest
```

```bash tab="Download" group=install
# Download the archive for your platform from
# https://github.com/friendo-world/friendo/releases/latest
# and put `friendo` where the old one was.
```

The installer takes a version: `FRIENDO_VERSION=v0.5.0` before `sh` installs that
release instead of the latest. See [Environment variables](/docs/env#installer).

Run `friendo --version` again to confirm.

## 4. Restart

Stop `friendo serve` and start it again. When friendo opens `data/friendo.db` it
applies any schema changes the new version brings, each in its own transaction, and
records them in the database's `schema_migrations` table. There's nothing to run
by hand. If a change fails, friendo stops with `applying migrations: …` and leaves
the database as it was before that change.

Any command that opens the database does the same, `friendo import` included.

> **Common mistake:** going back to an older binary after an upgrade. Nothing
> undoes a schema change. To go back, restore the backup from step 2.

## A self-hosted site

On the server: back up, install the new binary, restart the service (for example
`sudo systemctl restart my-site`). Your laptop's binary and the server's can be
upgraded separately. See [Self-host a site](/docs/self-host-a-site).

## A network

The Docker image builds friendo from the repo. Redeploy the app in your host (on
Coolify, **Redeploy**) to pick up a new version. The `/data` volume survives
redeploys. Each site's database updates the first time the network opens it after
the restart. See [Run a network](/docs/run-a-network).

A site on friendo.world is upgraded by its operator; your part is keeping your
laptop's `friendo` current.

**Next:** [Troubleshooting](/docs/troubleshooting)
