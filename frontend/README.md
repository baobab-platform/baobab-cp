# CP Console

The Baobab Control Plane Console is the administrative frontend defined by ADR-BCP-019. It is a Next.js App Router
application that runs as its own container and reaches the Go Control Plane API only from the server. The browser
never calls the Control Plane directly, and never holds a token.

Gates FE-01 (foundation: tooling, configuration, build, image and CI) and FE-02 (design system) are in place.
Authentication, the shell and every workspace arrive in later gates (`docs/frontend/fe-00-architecture-lock.md` §9).
No page shows Control Plane data yet.

## Stack

| Concern | Choice |
|---|---|
| Runtime | Node.js 24 LTS (`.node-version`) |
| Framework | Next.js 16 (App Router, `output: "standalone"`), React 19 |
| Language | TypeScript 5.9, strict, with `noUncheckedIndexedAccess` and `exactOptionalPropertyTypes` |
| Package manager | pnpm, pinned by the root `package.json` `packageManager` and enabled through corepack |
| Lint | ESLint 9 with `eslint-config-next` (TypeScript 7 and ESLint 10 wait on `typescript-eslint` and `eslint-plugin-react` support) |
| Unit tests | Vitest, Testing Library, jsdom and axe-core |

The repository root is the pnpm workspace (`package.json`, `pnpm-workspace.yaml`, `pnpm-lock.yaml`), with this
directory as its only package. The organisation's foundation gates install and audit Node dependencies at the root.
pnpm holds back newly published versions by its default minimum release age, and the workspace makes no exceptions.

## Design system

`src/components` is the Console's own design system (FE-02, prompt §§52-54). Features import from `@/components`.

| Area | Components |
|---|---|
| Tokens | `src/styles/tokens.css`: colour, the nine semantic status tones, typography, spacing, radius, elevation, focus, motion |
| Actions | `Button` (primary, secondary, danger, ghost; `busy`), `ConfirmDialog` (typed confirmation and audited reason) |
| Forms | `Field` (label, hint and error wired to the control), `TextInput`, `Select`, `Textarea` |
| Feedback | `Alert`, `StatusBadge`, `LoadingState`, `EmptyState`, `ErrorState`, `Dialog` |
| Structure | `PageHeader`, `Table` (captioned, server-sorted), `SideNav`, `Tabs`, `SkipLink` |

Rules the components enforce:

- Status is never colour alone. Every tone has a glyph and text, and features keep their precise domain wording.
- Every token pair meets WCAG 2.2 AA contrast. `tests/unit/tokens.test.ts` computes the ratios.
- Dialogs are native `<dialog>` modals, so the rest of the page is inert, and focus returns to the opener.
- Loading states name what is loading, empty states explain what would fill them, and error states give a next step and
  the Control Plane reference.
- Tables sort, filter and page on the server. A sortable header is a link, and its state is exposed with `aria-sort`.
- Reduced motion is honoured, and layouts reflow at 320 CSS pixels.
- Strings live in `src/lib/i18n/messages.ts`, and nothing concatenates sentences.

Every component test runs axe in jsdom. `/design-system` shows every component and state with illustrative data, and
returns 404 when `CONSOLE_ENVIRONMENT` is `production`.

## Configuration

Configuration is read only on the server, by `src/server/env.ts`. It is validated when the server starts
(`src/instrumentation.ts`), and invalid configuration stops the process. Lint forbids `process.env` anywhere else.

| Variable | Required | Meaning |
|---|---|---|
| `CONSOLE_ENVIRONMENT` | yes | `development`, `integration`, `staging` or `production` |
| `CP_API_BASE_URL` | yes | The Control Plane API the server calls. It must be https, except `localhost` in development. |

Nothing uses the `NEXT_PUBLIC_` prefix. Next.js inlines such variables into browser bundles, so startup refuses any
`NEXT_PUBLIC_` variable whose name suggests a secret.

## Develop

```sh
make frontend-install   # corepack enable, then pnpm install --frozen-lockfile at the root
CONSOLE_ENVIRONMENT=development CP_API_BASE_URL=http://localhost:8080 make frontend-dev   # http://localhost:3000
make frontend-typecheck frontend-lint frontend-test frontend-build
make frontend-image     # docker build --file frontend/Dockerfile .
```

The Dev Container installs Node 24 and forwards ports 8080 (Control Plane API) and 3000 (Console).

## Image

`frontend/Dockerfile` builds from the repository root (its build context), and `Dockerfile.dockerignore` limits the
context to the workspace manifests and this directory. It builds with the Node 24 image and runs on distroless
`nodejs24-debian13` as a non-root user.
Both base images are pinned by digest. `GET /healthz` reports liveness only, and the image's `HEALTHCHECK` calls it
through `healthcheck.mjs`, because the runtime image has no shell.

Every response carries baseline security headers (`next.config.ts`), including `frame-ancestors 'none'`. A
nonce-based Content-Security-Policy comes with authentication (FE-03), and FE-17 hardens it.

## CI

`.github/workflows/frontend.yml` runs only when `frontend/` or the workspace files change. It installs from the
lockfile, then typechecks, lints, tests, builds, audits dependencies and builds the image.
