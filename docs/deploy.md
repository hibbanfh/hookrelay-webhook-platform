# First deploy to Fly.io

Two Fly apps from one repo: `hookrelay` (Go API + worker, `backend/fly.toml`) and `hookrelay-web`
(Next.js dashboard and BFF, `frontend/fly.toml`). CI deploys both on every push to `main` once the
`FLY_API_TOKEN` GitHub secret exists. Until then the deploy jobs skip and CI stays green.

Prerequisites: a Fly.io account with a payment method, `flyctl` ([install](https://fly.io/docs/flyctl/install/)),
`openssl`, a Neon project with Neon Auth enabled (its base URL is `NEON_AUTH_BASE_URL`).

```sh
fly auth login
```

## 1. Create the apps

```sh
fly apps create hookrelay
fly apps create hookrelay-web
```

App names are global on `fly.dev`. If one is taken, pick new names (for example `hookrelay-yourname` and
`hookrelay-yourname-web`) and update every place that holds the old one:

- `app = ...` in `backend/fly.toml` and `frontend/fly.toml`
- `PUBLIC_BASE_URL` and `DASHBOARD_ORIGIN` in `backend/fly.toml`
- `DASHBOARD_ORIGIN`, `API_INTERNAL_URL` (`api.process.<backend-app>.internal`) and
  `NEXT_PUBLIC_API_BASE_URL` in `frontend/fly.toml`
- the URLs in the README badges and in section 8 below

Both apps must live in the same Fly organization: the web app reaches the API over Fly private
networking (`http://api.process.hookrelay.internal:8080`, the `api` process group only, because the worker does not
listen on 8080). In different orgs, set `API_INTERNAL_URL = "https://hookrelay.fly.dev"` instead.

## 2. Postgres (pick one, open question Q3)

**Option A: Neon Postgres** (managed, free tier, same vendor as Neon Auth). Create a database in the Neon
console and copy the **direct** (non-pooled) connection string. The pooled one goes through PgBouncer in
transaction mode, which does not suit the worker's long-lived pgx connections.

```sh
fly secrets set -a hookrelay DATABASE_URL='postgres://USER:PASSWORD@ep-xxxx.REGION.aws.neon.tech/neondb?sslmode=require' --stage
```

**Option B: Fly Postgres** (unmanaged: you own backups and upgrades).

```sh
fly postgres create --name hookrelay-db --region iad
fly postgres attach hookrelay-db -a hookrelay   # sets the DATABASE_URL secret on hookrelay
```

## 3. Backend secrets

`hookrelay migrate up` (the release command) loads the full production config, so every value below must exist
before the first `fly deploy`. Non-secret values (`DASHBOARD_ORIGIN`, `OUTBOUND_ALLOWLIST_ONLY=true`, ...) are in
`backend/fly.toml` `[env]`.

```sh
# 32 random bytes, base64 (the app requires exactly 32 decoded bytes). Keep a copy: losing it makes stored
# signing secrets unreadable.
SECRET_ENCRYPTION_KEY=$(openssl rand -base64 32)
# "hr_" + 43 base64url characters. It is the shared demo key, so it is public once you document it.
SEED_DEMO_API_KEY="hr_$(openssl rand -base64 32 | tr '+/' '-_' | tr -d '=')"
echo "$SEED_DEMO_API_KEY"   # note it down

fly secrets set -a hookrelay --stage \
  SECRET_ENCRYPTION_KEY="$SECRET_ENCRYPTION_KEY" \
  SEED_DEMO_API_KEY="$SEED_DEMO_API_KEY" \
  NEON_AUTH_BASE_URL='https://ep-xxxx.neonauth.REGION.aws.neon.tech/neondb/auth'
# DATABASE_URL was set in step 2 (Option A) or by `fly postgres attach` (Option B).
fly secrets list -a hookrelay   # expect all four names
```

## 4. Web secrets

```sh
fly secrets set -a hookrelay-web --stage \
  SESSION_SECRET="$(openssl rand -base64 48)" \
  NEON_AUTH_BASE_URL='https://ep-xxxx.neonauth.REGION.aws.neon.tech/neondb/auth'
```

`--stage` stores secrets without redeploying (the apps have no machines yet).

## 5. Neon Auth trusted origins

In the Neon console, open the project's Auth settings and add the dashboard origin to the trusted origins
(domains) list: `https://hookrelay-web.fly.dev` (no path, no trailing slash). The BFF sends it as the `Origin`
header, and Neon rejects sign-up and sign-in from origins that are not listed. It must equal `DASHBOARD_ORIGIN`
exactly. Add `http://localhost:3000` too if you want local sign-in against the same Neon project.

## 6. First deploy (order matters)

Backend first: its release command migrates the database.

```sh
cd backend && fly deploy
fly scale count api=1 worker=1 -a hookrelay     # one machine per process group
fly ssh console -a hookrelay -C "/hookrelay seed"   # idempotent: demo project and two demo endpoints
cd ../frontend && fly deploy
```

If the release command fails, read `fly logs -a hookrelay`: the error names the missing or invalid variable.

## 7. GitHub Actions deploys

```sh
fly tokens create deploy -a hookrelay        # copy the token
```

Repository Settings, Secrets and variables, Actions, new secret `FLY_API_TOKEN`. A deploy token is scoped to one
app, so create a second one for `hookrelay-web` or use an org token instead (`fly tokens create org`) and store
that single token. From the next push to `main`, CI deploys the backend, then the dashboard.

## 8. Verify

```sh
curl -fsS https://hookrelay.fly.dev/healthz
curl -fsS https://hookrelay.fly.dev/readyz            # 200 only when Postgres is reachable
curl -fsS https://hookrelay-web.fly.dev/api/healthz   # {"status":"ok"}
fly status -a hookrelay        # api and worker machines passing checks
fly status -a hookrelay-web
```

Then open `https://hookrelay-web.fly.dev`, sign up, and publish a message to the demo endpoint. With
`OUTBOUND_ALLOWLIST_ONLY=true` only the built-in demo receiver can be registered as a destination.

## 9. Rollback

```sh
fly releases -a hookrelay                       # list versions
fly deploy -a hookrelay --image registry.fly.io/hookrelay:deployment-<id>   # redeploy an older image
```

Migrations run forward only in the release command and are not undone by a rollback. Old code must tolerate the
new schema (a rule of this repo). To undo a schema change, run `fly ssh console -a hookrelay -C "/hookrelay migrate down"`
once per migration. Same commands with `-a hookrelay-web` for the dashboard (find the image with `fly releases --image`).
