#!/bin/sh
# Enforces the Clean Architecture dependency rule with plain greps (ADR 0005 + ADR 0010).
# Run from backend/ (make lint-layers). Prints violations and exits 1 if any exist.
# Mirrors the depguard rules in .golangci.yml and docs/architecture.md section 4.
#
# Module layout (internal/<module>/):
#   domain/{entity,errors,repository,usecase}  data/{model,source,repository}
#   delivery/{dto,presenter/http}
cd "$(dirname "$0")/.." || exit 2

fail=0
DOMAINS="project endpoint message dispatch demoreceiver"
MOD="hookrelay/internal"

# check DESCRIPTION REGEX DIR...  (non-test Go files only; missing dirs are ignored)
check() {
	desc=$1
	re=$2
	shift 2
	dirs=""
	for d in "$@"; do
		[ -d "$d" ] && dirs="$dirs $d"
	done
	[ -z "$dirs" ] && return 0
	# shellcheck disable=SC2086
	out=$(grep -rnE --include='*.go' --exclude='*_test.go' "$re" $dirs 2>/dev/null)
	if [ -n "$out" ]; then
		echo "LAYER VIOLATION: $desc"
		echo "$out"
		fail=1
	fi
}

# only DESCRIPTION ALLOW_REGEX DIR...  flags every import of this repo or of a third party
# (anything whose first path element has a dot) that ALLOW_REGEX does not match.
# Standard-library imports are not looked at here; see the "stdlib" check below.
only() {
	desc=$1
	allow=$2
	shift 2
	dirs=""
	for d in "$@"; do
		[ -d "$d" ] && dirs="$dirs $d"
	done
	[ -z "$dirs" ] && return 0
	# shellcheck disable=SC2086
	out=$(grep -rnE --include='*.go' --exclude='*_test.go' '^[[:space:]]*([A-Za-z_]+ )?"(hookrelay/|[a-z0-9-]+\.[a-z]+/)' $dirs 2>/dev/null |
		grep -vE "\"($allow)(/[^\"]*)?\"")
	if [ -n "$out" ]; then
		echo "LAYER VIOLATION: $desc"
		echo "$out"
		fail=1
	fi
}

for m in $DOMAINS; do
	M="internal/$m"
	P="$MOD/$m"

	# 1. domain/*: no net/http, slog or database/sql; imports restricted per sub-package
	check "$m: domain must not import net/http, log/slog or database/sql" \
		'"(net/http|log/slog|database/sql)"' \
		"$M/domain"
	only "$m: domain/errors imports the stdlib only" \
		'$^' \
		"$M/domain/errors"
	only "$m: domain/entity imports the stdlib and domain/errors only" \
		"$P/domain/errors" \
		"$M/domain/entity"
	only "$m: domain/repository imports domain/entity and domain/errors only" \
		"$P/domain/(entity|errors)" \
		"$M/domain/repository"
	only "$m: domain/usecase imports domain/* only" \
		"$P/domain/(entity|errors|repository)" \
		"$M/domain/usecase"

	# 2. data/*
	only "$m: data/model imports the stdlib only" \
		'$^' \
		"$M/data/model"
	only "$m: data/source imports data/model, pgx and platform only" \
		"$P/data/model|github.com/jackc/pgx|$MOD/platform" \
		"$M/data/source"
	only "$m: data/repository imports data/source, data/model and domain/{entity,errors,repository} only" \
		"$P/data/(source|model)|$P/domain/(entity|errors|repository)" \
		"$M/data/repository"
	check "$m: data/repository must not contain SQL" \
		'(SELECT .* FROM|INSERT INTO|UPDATE .* SET|DELETE FROM)' \
		"$M/data/repository"

	only "$m: data/neonauth imports domain/{entity,errors,repository} and x/sync only" \
		"$P/domain/(entity|errors|repository)|golang.org/x/sync" \
		"$M/data/neonauth"

	# 3. delivery/*
	only "$m: delivery/dto imports domain/entity at most" \
		"$P/domain/entity" \
		"$M/delivery/dto"
	only "$m: delivery/presenter imports delivery/dto, domain/{usecase,entity,errors} and platform/{httpx,auth} only" \
		"$P/delivery/dto|$P/domain/(usecase|entity|errors)|$MOD/platform/(httpx|auth)" \
		"$M/delivery/presenter"
done

# 4. only cmd/server wiring imports data or presenters (a module's data may import itself).
# --exclude-dir drops every directory with that name from the search.
check "nothing but cmd/server and data itself may import a data package" \
	"$MOD/[a-z_]+/data" \
	internal/platform $(for d in $DOMAINS; do echo "internal/$d/domain internal/$d/delivery"; done)
check "nothing but cmd/server may import a presenter package" \
	"$MOD/[a-z_]+/delivery/presenter" \
	internal/platform $(for d in $DOMAINS; do echo "internal/$d/domain internal/$d/data internal/$d/delivery/dto"; done)

# 5. a domain never imports another domain; platform never imports a domain
for d in $DOMAINS; do
	for o in $DOMAINS; do
		[ "$d" = "$o" ] && continue
		check "domain $d must not import domain $o (use a caller-declared port + cmd/server/adapters.go)" \
			"hookrelay/internal/$o(/|\")" \
			"internal/$d"
	done
	check "platform must not import domain $d" \
		"hookrelay/internal/$d(/|\")" \
		internal/platform
done

# 6. go list based double check on the innermost layer, when domain packages exist:
# transitive deps may only be stdlib or other domain/* packages of the same module.
for pkg in $(go list ./internal/*/domain/... 2>/dev/null); do
	bad=$(go list -deps "$pkg" 2>/dev/null | grep -E '^(net/http|github\.com/jackc|hookrelay/)' | grep -vxF "$pkg" | grep -vE '^hookrelay/internal/[a-z_]+/domain/')
	if [ -n "$bad" ]; then
		echo "LAYER VIOLATION: $pkg transitively depends on:"
		echo "$bad"
		fail=1
	fi
done

if [ "$fail" -eq 0 ]; then
	echo "layer rules: OK"
fi
exit "$fail"
