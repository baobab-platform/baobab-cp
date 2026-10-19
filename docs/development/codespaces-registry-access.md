# Baobab Control Plane: secure Codespaces environment
Use the v1.4.4 [baobab-dev published image](https://github.com/baobab-platform/baobab-dev/releases/tag/v1.4.4),
pinned to the full multi-language image digest `sha256:9603f0e7402aa85771e904db1c20f2ecc02f705131b266cb4ec3d1c24ea58ae4`. Before creating
the Codespace, an organisation package owner must add `baobab-platform/baobab-cp` to the
**Manage Codespaces access** permission on the GHCR **baobab-dev** package
(or verify inherited access). **Manage Actions access** is separate and
required for this repository's workflow to authenticate GHCR.
CP also requires access to `baobab-platform/shared` through a new Codespace's
approved read-only repository request. The console consumes pinned
`experience-tokens@0.3.1` and `experience-ui@0.3.1`, and the
Shared package owner must approve CP Codespaces access to these packages.
Go 1.27 and the outside-of-Docker integration workflow are kept distinct
from the Node/pnpm console. No direct SQL or CP authority bypass is provisioned.
The prebuild performs only credential-free offline tests. After the editor
opens, post-create sets only a *literal* user npmrc token reference, verifies
external exact private npm releases when present, then installs the frozen
pnpm lockfile. Credentials: use repository-approved GITHUB_TOKEN or a
restricted Codespaces secret named BAOBAB_PACKAGES_READ_TOKEN with
read:packages and required organisation SSO authorisation.
Never write the raw token to the repository npmrc, .env or image.

After creating a **new** Codespace (approval of requested read-only
cross-repository permissions is required):
```bash
node scripts/codespaces/bootstrap.mjs
baobab-verify --contract
go version
pnpm --dir frontend dev
```
A successful Actions GHCR digest inspection does not establish Codespaces
image/package access. Never make packages public, enable TLS bypass, or
weaken pinned lockfile/version checks to make the image load.
