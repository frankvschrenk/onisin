# Onisin build orchestration.
#
# Why this file exists: the repo is a hybrid of ONE Rust workspace
# (services/ + crates/, sharing ./target) and FOUR detached Tauri apps
# (apps/*/src-tauri, each with its own [workspace] and target/). No single
# cargo or bun command spans both halves, so the build steps are codified here.
#
# Build and run are deliberately separate: this Makefile only *compiles*.
# Running the compiled artifacts is oosrun's job — a small TUI supervisor
# (tools/oosrun) that reads the profiles from oosrun.yaml and runs them:
# `oosrun`, `oosrun api`, `oosrun ui`, `oosrun dev`.
#
#   make all     compile everything (services + apps + oosrun)
#   make api     compile the headless Rust services + oosrun -> ./target/release
#   make ui      compile the Tauri desktop apps into runnable binaries
#   make oosrun  compile just the oosrun supervisor
#   make bundle  build distributable .app/.dmg bundles for the apps (slow)
#   make deps    install the JS workspace dependencies (bun)
#   make clean   remove all build output

# Override on the command line when these are not on PATH, e.g.
#   make api CARGO=$(HOME)/.cargo/bin/cargo BUN=$(HOME)/.bun/bin/bun
CARGO ?= cargo
BUN   ?= bun

# Tauri desktop apps. Each compiles independently in its own detached
# workspace. bench is built here (it is a real app) but stays out of the
# oosrun run set — see the ui profile in oosrun.yaml for the reason.
UI_APPS := oos oosd ooso bench

# The ui-% / bundle-% pattern targets are intentionally NOT marked .PHONY:
# GNU make excludes phony targets from implicit/pattern-rule search, which
# would suppress their recipes. They have no same-named files, so they run
# every time regardless.
.PHONY: all api ui oosrun bundle deps clean run run-api run-ui

all: api ui

# --- compile -------------------------------------------------------------

# One workspace build emits every member binary (oosai, oosgql, oosagent,
# oosiam, oosmem, bench-nats, and the oosrun launcher) into ./target/release.
# The detached app workspaces under apps/ are not members, so cargo skips
# them here.
api:
	$(CARGO) build --release

# oosrun on its own, for a quick rebuild of just the launcher. It is already
# covered by `make api` / `make all` as a workspace member.
oosrun:
	$(CARGO) build --release -p oosrun

# Per app: the tauri beforeBuildCommand runs `bun run build` (vite) first, then
# cargo builds the release binary and bundles it. We build the .app (the
# artifact a user actually launches) but skip the slow .dmg via `--bundles
# app`. oosrun launches these bundles through `open`, matching a real user
# launch. (On Linux/Windows the user-facing artifact differs; this target
# would branch per OS once those platforms land.)
ui: $(addprefix ui-,$(UI_APPS))

ui-%:
	cd apps/$* && $(BUN) run tauri build --bundles app

# --- distributable bundles (optional, slow) ------------------------------

bundle: $(addprefix bundle-,$(UI_APPS))

bundle-%:
	cd apps/$* && $(BUN) run tauri build

# --- housekeeping --------------------------------------------------------

# A single root install resolves every app and package in the bun workspace.
deps:
	$(BUN) install

clean:
	$(CARGO) clean
	for app in $(UI_APPS); do ( cd apps/$$app/src-tauri && $(CARGO) clean ); done
	rm -rf apps/*/dist

# --- run (convenience; `oosrun <profile>` from anywhere does the same) ------

run: oosrun
	./target/release/oosrun all

run-api: oosrun
	./target/release/oosrun api

run-ui: oosrun
	./target/release/oosrun ui
