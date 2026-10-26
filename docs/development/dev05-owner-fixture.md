# DEV-05 CMS owner fixture

The separate development-bootstrap image exposes `--cms-capability-phase` and
`--cms-capability-source` for the disposable Nabhold CMS profile. A phase cannot
be combined with external-provider or Subscriptions fixture phases. Ordinary
development standing authority applies; this creates no protected permission.

The CMS and external Pulse profiles reuse one orchestration implementation, with
separate profile keys, locks, products, subscriptions, capabilities and instances.
Tenant and instance identities come only from the current leased CP bootstrap.
The native fixture table is keyed by run and profile, and retries must preserve
the immutable source/declaration/OCI proof. No canonical IDs or endpoints can be
selected in the proof file. The file remains private and mounted read-only; its
one-shot command must use the operator UID/GID to read its 0600 inode.

`register` syncs the canonical catalogue and registers a DRAFT CMS provider via
the normal registry. Capability fields come from the embedded Shared content/v1
definition, with only catalogue-only metadata omitted from the registration
envelope. Registration is not activation and creates no grant or binding.

`activate` fetches and hashes the actual published run-owned OCI manifest and
observes CMS's real Payload/PostgreSQL readiness over verified TLS. It uses normal
release approval and provider activation with distinct synthetic maker/checker
Principals. An expiring, nonmonetary TRIAL subscription expands through the normal
composition service into a content grant, followed by normal provider binding
admission. It does not assert incorporation, INTERNAL eligibility, certification,
finance authority, staging or production approval.

`read`, `unentitled`, `regrant`, `absent`, `restore` and `retire` retain the existing
normal lifecycle controls. The response independently reads current source,
grant, projection, binding and provider facts; a requested transition is never
its own evidence. The Infrastructure qualifier must additionally exercise the
actual CMS/BFF path and independently join native CMS audit to CP's resolution.

Owner tests cover closed canonical registration, profile/tenant isolation,
registration replay without activation, changed-proof refusal, out-of-order
withdrawal, and protected/expired refusal. Actual joined NAB-03 qualification
requires all fourteen original cases and issuer/host/private-state teardown in
Infrastructure; these source/owner tests cannot supply it. DEV-06's unchanged
eighteen-case profile remains a required regression after this shared refactor.
