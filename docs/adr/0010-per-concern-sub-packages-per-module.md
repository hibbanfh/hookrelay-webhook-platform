# ADR 0010: One Go package per concern inside each module

- **Status:** Accepted. Amends the directory layout of [ADR 0005](0005-clean-architecture-and-manual-di.md), which is partially superseded. The dependency rule (`delivery -> usecase -> domain <- data`), caller-owned cross-module ports, and the context-carried unit of work from ADR 0005 stay in force. Manual dependency injection was later replaced by [ADR 0012](0012-google-wire-and-per-dependency-libraries.md), which also makes `backend/wire` part of the composition root wherever this ADR says `cmd/server`.
- **Date:** 2026-11-08

## Context

ADR 0005 put one Go package per *layer* in each module: `domain`, `usecase`, `delivery/http`,
`data/postgres`. That works for a single slice, but each package already mixes several concerns:

- `domain` holds entities, errors and ports, and `usecase` holds the use cases together with their
  technical ports (`ports.go`).
- `data/postgres` holds the SQL strings, the pgx error handling, the row mapping and the
  translation to domain errors, so a unit test of "unique violation becomes `ErrConflict`" has to
  stub pgx.
- `delivery/http` holds DTOs, handlers, middleware and the error table.

Inside a package the compiler allows any file to use any other, so the dependency rule could only
be enforced between layers, not between concerns. We also want the module layout to be familiar
to people who know the team's other Go services (the `branch` and `customer_risk_profile`
modules of the support-system code base), which already split a module into
`domain/{entity,repository}` and `data/{model,source,repository}` per concern. We adopt that
shape without copying the parts of those services we consider anti-patterns (see Alternatives).

## Decision

Every module (`project` now, `endpoint`, `message`, `dispatch`, `demoreceiver` later) is laid out
as one Go package per concern:

```
internal/<module>/
  domain/
    entity/          entities and value objects, one file each           (package entity)
    errors/          sentinel and typed errors, one file per concern     (package domainerrors)
    repository/      storage ports, one interface per file               (package repository)
    usecase/         one use case per file + small ports (Clock, IDGenerator, ...), one per file
  data/
    model/           DB row structs, tags only                           (package model)
    source/          <x>_persistent.go: interface + pgx implementation   (package source)
    repository/      <x>_repository_impl.go: ports implemented on sources (package repositoryimpl)
  delivery/
    dto/             request/response structs, one file per use case     (package dto)
    presenter/http/  handlers, routes, middleware, error table, mapper   (package httppresenter)
```

Directories that would clash with another package name use a role-specific package name
(`domainerrors`, `repositoryimpl`, `httppresenter`), as ADR 0005 already did for `httpdelivery`.

### Allowed imports (enforced by `depguard` in `.golangci.yml` and `scripts/check-layers.sh`)

| Package | May import (besides the stdlib) |
|---|---|
| `domain/errors` | nothing |
| `domain/entity` | `domain/errors` |
| `domain/repository` | `domain/entity`, `domain/errors` |
| `domain/usecase` | `domain/entity`, `domain/errors`, `domain/repository` |
| `data/model` | nothing |
| `data/source` | `data/model`, pgx, `platform/*` |
| `data/repository` | `data/source`, `data/model`, `domain/*` |
| `delivery/dto` | `domain/entity` at most |
| `delivery/presenter/http` | `delivery/dto`, `domain/{usecase,entity,errors}`, `platform/{httpx,auth}` |

Nothing imports `data/*` or `delivery/presenter` except `cmd/server`, and no module imports
another module's `data/` or `delivery/` (the ADR 0005 cross-module ports still apply).

### Where each translation lives

- **SQL and pgx errors: `data/source`.** A source returns models or a source-level sentinel
  (`source.ErrNotFound`, `source.ErrUniqueViolation`). pgx types never leave the package.
- **Model <-> entity and source error -> domain error: `data/repository`.** It is the only
  package that sees both a model and an entity.
- **Entity -> DTO: `delivery/presenter/http/mapper.go`.** Entities are never serialized directly.

### Why entity/model mapping lives in `data/repository` and not in the entity

The reference services give entities a `FromModel(model)` function, so the entity package imports
`data/model`. That points the dependency the wrong way: `domain` would depend on `data`, and a
column rename would force a change in the innermost layer. Here `domain/entity` imports nothing
but `domain/errors`, `data/model` imports nothing, and the repository implementation (an outer
layer) owns both mapping directions. The cost is one small mapper file per module. The benefit is
that the rule "domain never imports data" can be checked mechanically and has no exceptions.

The same reasoning keeps SQL out of use cases and repositories: the reference services pass raw
SQL strings from the use case into the data source (`Raw`). Here a use case only calls a port
method, and the SQL string is a constant owned by the source.

## Alternatives considered

1. **Keep one package per layer (ADR 0005 as written).** Simplest, with fewer files and imports,
   but concerns inside a package cannot be separated by the compiler or by `depguard`, and the
   database error translation could not be unit-tested without faking pgx.
2. **Copy the reference layout exactly.** That would bring `entity.FromModel` (domain depends on
   data), raw SQL built in use cases, `CustomError` type assertions instead of `errors.Is/As`, and
   goroutine/channel boilerplate around simple calls. Rejected: they break the dependency rule and
   the clean-code rules in `.claude/references/backend-architecture.md`.
3. **Merge model and entity (one struct with both db and json tags).** Fewer mappers, but it
   couples the schema, the domain and the API, and leaks storage details into the contract.
4. **Split into separate Go modules per layer.** Strongest enforcement, but far too heavy for a
   portfolio-sized repository.

## Consequences

- More files and packages per module (the `project` module went from 4 to 10 Go packages), and
  constructors are a little longer to wire:
  `repositoryimpl.NewProjectRepository(source.NewProjectPersistent(pool))`. Wiring stays in one
  place (`cmd/server`).
- Error translation is covered by two small unit-test files (source: pgx errors -> sentinels;
  repository: sentinels -> domain errors and UTC mapping) in addition to the Testcontainers
  integration tests, which now live next to `data/repository`.
- A new module copies the layout and the `depguard` block in `.golangci.yml` (rules are written
  per module name because depguard matches import path prefixes) and adds its name to
  `DOMAINS` in `scripts/check-layers.sh`.
- The layout section of `.claude/references/backend-architecture.md` still describes the ADR 0005
  layout until it is updated to match this ADR.
- Behaviour is unchanged: HTTP responses, error mapping, the identical 401, key redaction and
  seed idempotency are covered by the same tests as before, moved with their code.
