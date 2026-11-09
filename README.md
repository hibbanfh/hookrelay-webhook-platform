# Portfolio Product Team for Claude Code

A ready-made "virtual team" for Claude Code that builds portfolio projects
with a Go backend and a Next.js frontend.

## What's inside
```
CLAUDE.md                      # shared rules, stack, repo layout, workflow (every agent reads it)
.claude/agents/
  product-manager.md           # PRD, user stories, acceptance criteria, scope control
  architect.md                 # system design, OpenAPI contract, data model, ADRs
  backend-engineer.md          # Go implementation with TDD
  frontend-engineer.md         # Next.js + TS + Tailwind UI
  qa-engineer.md               # test plan, E2E/API tests, bug reports, PASS/FAIL
  code-reviewer.md             # read-only senior review
  devops-engineer.md           # Docker, CI/CD, deploy, observability
  tech-writer.md               # portfolio README, changelog, case study
.claude/commands/
  kickoff.md   → /kickoff [idea]      new project: idea → PRD → design → working skeleton
  feature.md   → /feature <US-id>     one story through the full team
  review.md    → /review              review + QA on current changes
  ship.md      → /ship [version]      release, deploy, docs, LinkedIn post draft
```

## Setup
1. Install Claude Code and create an empty project folder (`git init`).
2. Copy `CLAUDE.md` and the `.claude/` folder into the project root.
3. Run `claude` inside the folder. Type `/agents` to check that all 8 roles are listed.

## Usage
```
/kickoff                       # PM suggests 3 product ideas, you pick one
/kickoff a job queue SaaS where devs can schedule webhooks with retries
/feature US-001
/review
/ship v0.1.0
```
You can also call a single role directly:
"Use the architect subagent to design rate limiting for the public API."

## Tips
- Approve the PM's MVP scope before building. Keeping scope small is how you actually finish.
- Work one story at a time with `/feature`, and commit after each one.
- The `model:` lines use opus for thinking-heavy roles (PM, architect, reviewer) and sonnet for
  implementation. Change them to fit your plan, or delete the line to use your default model.
- Edit the stack section of `CLAUDE.md` if you prefer different tools (e.g. Gin instead of chi,
  or React + Vite instead of Next.js).
