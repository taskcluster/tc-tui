# Changelog

All notable changes to `tc-tui` will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and this project follows
[Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed

- Fixed a failed artifact fetch being treated as the artifact's own content. A request that is refused or
  cannot be reached answers with an error document — Taskcluster's `InsufficientScopes` JSON, an S3
  `AccessDenied` XML page — which used to be rendered as the artifact's body and, worse, written to disk under
  the artifact's name by `s`, indistinguishable from a successful save. Any non-2xx response is now an error:
  the detail view shows it (e.g. `403 Forbidden: InsufficientScopes: …`), a live-log stream reports it instead
  of streaming the error document in as log output, and a save fails before creating a file.

## [1.1.0] - 2026-08-06

### Added

- Added masked-by-default secret details. Secret keys and structure remain visible while every scalar value is
  replaced with a fixed-width mask; press `v` to reveal values and `v` again to hide them. Reveals apply only
  to the current visit, are removed immediately when hidden, and are never written to the detail cache.

### Fixed

- Fixed the terminal being left unusable — no mouse-wheel scrollback, stray raw mode — when `tc-tui` did not
  exit through its own quit path. It now hands the terminal back before terminating on `SIGTERM` (`kill`) or
  `SIGHUP` (terminal window closed), including mid-`$EDITOR` handoff, and before a panic on any background
  goroutine takes the process down. `SIGKILL` remains unrecoverable.
- Fixed a session ended by `SIGTERM`/`SIGHUP` reporting success; it now exits `128+signo` (143/129) like any
  other signalled program, after saving navigation state as usual.

## [1.0.0] - 2026-08-05

This is the initial public release of `tc-tui`. It includes the original prototype work and the 2026 rewrite
into a general-purpose Taskcluster terminal client.

### Added

- Added a keyboard-driven terminal UI for browsing and acting on Taskcluster resources, with asynchronous
  loading, error views, contextual hints, breadcrumbs, and an `Esc`-based navigation stack.
- Added a `:` command bar with resource names and aliases, `:help`, `:quit` / `:q`, input history, and direct
  resource or ID lookup.
- Added a `Ctrl-A` command palette containing all resources, aliases, built-in commands, expected arguments,
  descriptions, and command-only actions. The palette is searchable with the normal list filter.
- Added consistent scope prompts for scoped resources. Resources with a browsable parent allow a blank value
  to open that parent; resources without one require the relevant task, task-group, worker, pool, or hook ID.
- Added command-line entry points for opening a resource directly, plus `--help` and `--version` output:
  `tc-tui <resource> [scope|id]`.
- Added resource views for:
  - roles, clients, and secrets;
  - hooks and recent hook fires;
  - worker pools, workers, recent worker tasks, launch configurations, Worker Manager errors, and cache-purge
    requests;
  - tasks, task groups, dependencies, dependents, runs, and artifacts;
  - pending and claimed task queues;
  - task-index namespaces and indexed tasks;
  - GitHub repository integration status and pull-request or commit builds;
  - navigation history and the command palette.
- Added extensive cross-resource navigation between tasks, task groups, runs, artifacts, dependencies,
  workers, worker pools, queues, launch configurations, errors, hooks, GitHub builds, and history entries.
- Added reusable authenticated-action dialogs with validation, progress, retryable API errors, destructive
  warnings, typed confirmation, cache invalidation, and post-action navigation.
- Added task lifecycle actions for canceling, scheduling, rerunning, retriggering under a new task ID, and
  changing priority. Actions are shown only when applicable to the current task state.
- Added task creation from YAML through `$VISUAL` / `$EDITOR`, followed by a read-only review and validation
  screen. Task creation supports timestamp rebasing, strict Taskcluster schema checks, definition history,
  idempotent retries, generated or supplied task IDs, and navigation to the created task.
- Added global `:createtask` / `:newtask` commands and contextual task-creation actions on task-group lists.
- Added reusable external-editor infrastructure for structured documents, including validation, re-editing,
  review, optional buffer transforms, and terminal suspension.
- Added rich task details including colored state badges, metadata, payload, timestamps, elapsed durations,
  retry/run information, dependencies, scopes, and YAML-rendered structured fields.
- Added task-run details with timestamps, state, worker navigation, artifacts, and direct live-log shortcuts.
- Added artifact browsing across all task runs with per-run facets, content type and size columns, direct web
  links, and raw downloads from either the list or detail view.
- Added artifact rendering for plain text, ANSI console output, Markdown, JSON, YAML, XML, and JavaScript,
  while detecting binary content and presenting metadata instead of terminal-corrupting bytes.
- Added progressive live-log streaming with follow mode, bounded buffering, clean cancellation on navigation,
  complete-line assembly, stream-end/error banners, and fallback to completed backing logs.
- Added safe artifact saving with a suggested filename, a configurable destination, and refusal to overwrite an
  existing file.
- Added case-insensitive filtering for lists and detail/log lines, including highlighted matches for longer
  queries and support for filtering an active live stream.
- Added sortable list columns, resource facets, persistent filters and facets, expandable columns, horizontal
  scrolling, detail word-wrap toggling, and optional line numbers that retain original log line positions while
  filtered.
- Added exact, filtered, and truncated list counts in titles, including `N+` counts for capped server results.
- Added safe initial limits for large task groups, worker lists, and queues, with `L` to load all remaining rows.
- Added short-lived list and detail caches. Returning to a detail renders its last known content immediately
  while refreshing it in the background and preserving scroll position.
- Added progressive worker-pool count augmentation for pending, claimed, and error totals, with bounded
  concurrency, filter-aware cancellation, throttled redraws, and cache-safe tracking of completed rows.
- Added persistent navigation state so the application resumes the previous view stack across restarts.
- Added web-UI links for supported resources via `o`, using the configured Taskcluster root URL.
- Added anonymous operation for public Taskcluster deployments and authenticated behavior when Taskcluster
  credentials are available in the environment.
- Added in-app and plain-terminal help generated from the same resource registry, so registered resources,
  aliases, columns, scopes, and keybindings remain synchronized.
- Added screenshots and end-user documentation for installation, configuration, navigation, resources, and
  common workflows.

### Changed

- Reworked the original UI into layered `taskcluster`, `resource`, `shell`, and `controller` packages with a
  registry-driven resource model and optional interfaces for scoped lists, facets, actions, streaming,
  downloads, web links, partial loading, and progressive augmentation.
- Upgraded to the Taskcluster v101 client and Go 1.26.5.
- Upgraded `tview` to v0.42.0 and `tcell` to v2.13.10, substantially improving rendering performance and
  terminal compatibility.
- Changed task groups to open directly as task lists, with sealed state displayed in the list title.
- Changed history and command-palette views into non-destructive peek views so dismissing them returns to the
  previously active screen.
- Changed terminal rendering to inherit the user's default background color.

### Fixed

- Fixed large text artifacts appearing to hang the application because of quadratic rendering behavior in the
  older `tview` implementation.
- Fixed large or syntax-highlighted artifacts overwhelming the UI by bounding downloads, highlighting only
  reasonably sized documents, capping rendered output, and retaining the diagnostically useful tail.
- Fixed live-log requests blocking until a task completed by switching running logs to incremental streaming.
- Fixed ANSI sequences and literal bracketed text being misinterpreted as `tview` markup while preserving real
  terminal colors.
- Fixed artifact-size sorting using lexical order instead of the rendered byte magnitude.
- Fixed empty worker assignments rendering as malformed identifiers instead of `n/a`.
- Fixed history navigation so `Esc` returns to the originating view and selecting a historical entry replaces
  the history peek rather than stacking beneath it.
- Fixed `Esc` with an active filter so it clears the filter before navigating back.
- Fixed refreshed and cached detail views resetting their scroll position or flashing an unnecessary loading
  screen.
- Fixed help output requiring `TASKCLUSTER_ROOT_URL`; `tc-tui --help` now works without constructing a live
  Taskcluster client.
- Fixed hook-fire lookups for hooks that have never fired by treating the service's not-found response as an
  empty history rather than a fatal resource error.
- Fixed scoped commands silently redirecting to unrelated parent resources or opening a parent detail instead
  of the selected scoped resource.
- Fixed the command palette omitting built-in help and quit commands, and unified command dispatch across the
  palette, command bar, and positional CLI arguments.
- Fixed command detail lookup via `:commands <name>` being shadowed by peek-view routing.
- Fixed misleading “blank to browse” prompts when a scoped resource has no genuinely browsable parent.
- Fixed stale rows from a previous resource reaching a newly selected resource's augmentation logic during an
  asynchronous load.
- Fixed multiple progressive-augmentation races and cache-poisoning cases involving filtered, rejected,
  newly-visible, requested, and settled worker-pool rows.
- Fixed duplicate or wasted augmentation work while filtering, including futile API requests for alphabetic
  queries that cannot match numeric augmented columns.
- Fixed UI starvation caused by redrawing an entire augmented list once per completed row by throttling
  redraws while always applying the final update.
- Fixed task-creation edge cases involving duplicate or unknown YAML keys, null documents, Unicode length
  validation, conflicting queue fields, explicit zero values, 64-bit payload values, schema bounds, and
  idempotent retries.
- Fixed destructive actions refreshing details for entities that had just been removed or invalidated.

### Security

- Updated the Go toolchain to include the fix for GO-2026-5856 in TLS ECH handling.
- Updated Goldmark to include the fix for GO-2026-5320. HTML output is not used by the terminal renderer, but
  the vulnerable dependency path was removed nonetheless.
- Added bounded reads and binary-content detection before displaying untrusted artifact content in the
  terminal.
