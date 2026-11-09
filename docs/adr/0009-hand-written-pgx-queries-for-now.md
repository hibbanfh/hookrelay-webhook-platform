# ADR 0009: Hand-written pgx queries for M0/US-001, sqlc later

- **Status:** Accepted (temporary deviation from the default stack in `CLAUDE.md`)
- **Date:** 2026-11-08

## Context

The default stack is `sqlc` + `pgx`. The `project` repository (`internal/project/data/source`)
instead contains three small hand-written SQL strings executed through a narrow `DB` interface
(`Exec`, `QueryRow`). The reason is practical: the development machine used for M0 could not
run the `sqlc` generator (no Docker or cgo toolchain available), and the slice had to be
finished and tested. Writing the deviation down is the point of this ADR.

The domain port (`repository.ProjectRepository`) does not change whether the adapter uses sqlc or hand
written pgx, so the layers above are unaffected.

## Decision

Keep the hand-written queries for US-001 only. They are limited to the `data/source` package,
use `$n` placeholders exclusively, and are covered by stub-based unit tests (arguments and error
translation) plus Testcontainers integration tests (`-tags integration`).

## Exit criteria (move to sqlc when any of these is true)

1. `sqlc generate` runs in CI (or on a developer machine) and a `sqlc diff` check can be added to CI.
2. The second domain (`endpoint`) needs repository code, so the query count is above about ten.
3. A query needs more than trivial scanning (joins returning many columns, `RETURNING`, batch).

The migration is mechanical: move each SQL string into `queries/*.sql`, replace the `model.Project`
scanning with the generated `Queries`, and keep the repository methods and their tests as they are.

## Consequences

- Column lists and `Scan` order are checked by the compiler only through the integration tests,
  not by generated types. CI now runs those tests (`-tags integration`, strict about Docker).
- Reviewers should reject any further hand-written SQL outside `data/source`.
