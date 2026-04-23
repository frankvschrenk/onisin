# onisin OS (OOS)

An AI-first enterprise data system. Business contexts are described as XML,
translated into a GraphQL schema at runtime, and rendered as UIs through a
declarative DSL. A desktop assistant lets end users query and edit data in
natural language while keeping the system grounded in the schema — no
hallucinated field names, no invented queries.

## What it is, in practice

- **Contexts** are the unit of data. Each context is either a *collection*
  (a list view) or an *entity* (a detail record). Contexts declare their
  fields, permissions, filters, relations and navigation. They live in
  PostgreSQL, as XML, and can be changed at runtime.
- **GraphQL** is built from the context AST. Queries, filters and
  mutations (create, update, delete) are generated automatically —
  the schema is whatever the contexts say it is, nothing hardcoded.
- **DSL screens** describe how a context is rendered on the desktop
  client. Forms, tabs, tables, dropdowns and validations live in XML
  too, so the visual layout stays in version control alongside the
  data model.
- **AI assistant** uses a ReAct agent (eino) with a small set of tools
  (`oos_query`, `oos_save`, `oos_delete`, schema search, …). The
  assistant's system prompt is assembled from global prompts and the
  live schema — changing an admin prompt in `global.conf.xml` changes
  LLM behaviour without a client rebuild.
- **Permissions** are declared per context per role (read / write /
  delete) and surfaced both to the LLM (as a resolved matrix in the
  system prompt) and — on the roadmap — to the server handlers for
  authoritative enforcement.

## Components

| Module        | What it does                                                     |
| ------------- | ---------------------------------------------------------------- |
| `oos`         | Fyne desktop client — chat, board, DSL renderer                  |
| `oosp`        | REST/GraphQL backend, schema store, meta/dropdown resolution     |
| `oos-common`  | Shared AST, GraphQL builder, plugin transport                    |
| `oos-dsl`     | DSL runtime: state, widgets, builder                             |
| `oos-dsl-base`| DSL parser and node types                                        |
| `oos-demo`    | Local demo orchestrator — installs binaries, seeds DB, boots OOS |

## Status

Active development. The public API, wire formats and DSL are
still shifting; we keep breaking changes concentrated rather than
constant, but this is not 1.0 yet.

## License

Source-available under a Business Source License 1.1 — see
[LICENSE.md](./LICENSE.md) for the full terms. In short: use it for
anything inside your own company, including commercial work.
Reselling it or offering it as a hosted service to third parties
requires a separate agreement with the Licensor. The license
converts to Apache 2.0 on the Change Date.
