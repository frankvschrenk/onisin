# Running the OOS demo

This is the shortest path from a fresh machine to a running OOS demo.
It covers macOS (Homebrew) and Debian/Ubuntu. Windows is not supported
for the desktop client; the backend binaries cross-compile but the demo
orchestrator is only smoke-tested on Unix.

All credentials in this document are **demo credentials**. The Postgres
superuser password in `demo.toml` is literally `demo`. Do not reuse any
of it outside a local development machine.


## Prerequisites

You need four things:

1. **Go 1.26 or newer** — to build the binaries.
2. **PostgreSQL 16 or newer with pgvector** — data, vectors, event pipeline.
3. **Ollama** — local LLM inference, with `gemma4:latest` and
   `granite-embedding:latest` pulled.
4. **A clone of this repository.**

Everything below walks through each of these in turn.


## 1. Go

    # macOS
    brew install go

    # Debian / Ubuntu
    sudo apt install golang-go      # only if it ships 1.26+
    # otherwise grab the tarball from https://go.dev/dl/

Check: `go version` should print 1.26 or higher.


## 2. PostgreSQL + pgvector

The demo needs a superuser login so that `oos-demo --seed-internal` can
`CREATE DATABASE`, `CREATE EXTENSION`, and `CREATE ROLE`. A local
Homebrew or APT install of Postgres gives you exactly that.

### macOS (Homebrew)

    brew install postgresql@16
    brew install pgvector
    brew services start postgresql@16

Homebrew creates a superuser role with your macOS username and no
password by default, but `demo.toml` expects the classic `postgres`
role. Create one and give it the demo password:

    psql -d postgres -c "CREATE ROLE postgres LOGIN SUPERUSER PASSWORD 'demo';"

Verify:

    psql -h localhost -U postgres -d postgres -c '\dx'

You should see an empty extension list — pgvector is installed on the
filesystem, but not enabled in any database yet. `oos-demo --seed-internal`
will enable it.

### Debian / Ubuntu

The distro packages are usually one major version behind. Use the
official PostgreSQL APT repository for current Postgres and a matching
pgvector package:

    # follow https://www.postgresql.org/download/linux/ubuntu/ to add the repo, then:
    sudo apt install postgresql-16 postgresql-16-pgvector

Set the `postgres` role password:

    sudo -u postgres psql -c "ALTER ROLE postgres PASSWORD 'demo';"

Make sure local connections accept passwords. In
`/etc/postgresql/16/main/pg_hba.conf`, the `host ... 127.0.0.1/32` line
should use `scram-sha-256` or `md5` (not `peer`). Reload after changes:

    sudo systemctl reload postgresql


## 3. Ollama

Install and start Ollama, then pull the two models OOS uses:

    # macOS
    brew install ollama
    brew services start ollama

    # Debian / Ubuntu — see https://ollama.com/download/linux
    curl -fsSL https://ollama.com/install.sh | sh
    sudo systemctl enable --now ollama

Pull the models:

    ollama pull gemma4:latest
    ollama pull granite-embedding:latest

`gemma4:latest` is the chat model (≈10 GB). If you have a larger
machine, `gemma4:26b` is more capable; edit the Ollama model name in
`oos.toml` via the OOS desktop client's Settings dialog. The embedding
model is fixed at `granite-embedding:latest` (384 dimensions — the
vector column geometry depends on it, do not swap it for a different
embedder without a schema migration).

Verify:

    ollama list

You should see both models listed.


## 4. Clone and build

    git clone git@github.com:frankvschrenk/onisin.git
    cd onisin
    make compile

This produces binaries under `dist/`:

- `dist/oos_macos` — Fyne desktop client
- `dist/oosp_macos` — plugin server
- `dist/ooso_macos` — importer / designer
- `dist/oos-demo_macos` — demo orchestrator

On Linux the suffix is `_linux_amd64`.


## 5. Seed the database

The demo separates schema bootstrap from demo data. This matters: `oosp`
has a Postgres listener that discovers event mappings at boot, and if
the mappings table is empty at that moment the listener idles. Seeding
before the services start avoids the whole race.

Run both seeding phases **from the repository root** (the demo reads
`demo.toml` from the current working directory):

    ./dist/oos-demo_macos --seed-internal
    ./dist/oos-demo_macos --seed-demo

`--seed-internal` creates the `onisin` database if it is missing,
enables pgvector and hstore, and installs the internal `oos.*` schema
plus the `oosp` and `ooso` application roles.

`--seed-demo` installs the `public.*` application tables (persons,
notes, reference data) and the police/support demo event streams, and
wires them into `oos.event_mappings`.

Both phases are idempotent — re-running them on an already-seeded
database is a no-op. If you want to start over:

    psql -h localhost -U postgres -d postgres -c 'DROP DATABASE IF EXISTS onisin;'
    ./dist/oos-demo_macos --seed-internal
    ./dist/oos-demo_macos --seed-demo


## 6. Run

    ./dist/oos-demo_macos

This starts the backend services (currently just `oosp`; the embedded
IAM runs in-process). Logs land in `~/.oos/logs/`. Hit Ctrl+C to stop,
or from another terminal:

    ./dist/oos-demo_macos --stop

Then start the desktop client:

    ./dist/oos_macos

The first launch opens a login window. Use one of the demo accounts
from `demo.toml`:

- `admin@oos.local` / `admin`
- `user@oos.local` / `user`


## Troubleshooting

**`pq: database "onisin" does not exist`** — the `--seed-internal`
phase was skipped. Run it. It will create the database.

**`pq: no schema has been selected to create in`** during
`--seed-internal` — someone dropped the `public` schema.
`--seed-internal` recreates it before enabling pgvector, so just
re-run the command. If it still fails, check that the `postgres` role
is a superuser (`\du` in psql).

**`pq: connection refused`** — PostgreSQL is not running.
`brew services start postgresql@16` or
`sudo systemctl start postgresql`.

**`password authentication failed for user "postgres"`** — the
password in `demo.toml` does not match what is set in Postgres. The
default in `demo.toml` is `demo`. Either change the role password
(`ALTER ROLE postgres PASSWORD 'demo'`) or change `demo.toml` to match
your local setup.

**Ollama errors in the chat** — check `ollama list` shows both models,
and that `ollama serve` is running (Homebrew does this via
`brew services`). The default Ollama URL in `demo.toml` is
`http://localhost:11434`.

**Empty AI responses but no errors** — the embedding model may be
missing. `ollama pull granite-embedding:latest`. Vector lookups fall
back to empty results silently when the model is unreachable.


## Cleanup

To reset everything — database, local state, logs:

    ./dist/oos-demo_macos --stop
    psql -h localhost -U postgres -d postgres -c 'DROP DATABASE IF EXISTS onisin;'
    rm -rf ~/.oos

Binaries under `dist/` are safe to keep; re-running `make compile`
overwrites them.
