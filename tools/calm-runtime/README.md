# tools/calm-runtime

This `package-lock.json` is the ADR-0005 integrity source for managed provisioning
of the FINOS `calm` CLI (`@finos/calm-cli`). It pins the full dependency tree so
`npm ci` fails closed on any tampered or unavailable package rather than silently
resolving a different tree, and the managed-provisioning doctor check verifies
the installed tree matches via `npm ls --json`.

When bumping the CALM CLI version, update this lockfile (`npm install
--package-lock-only`), the Dockerfile's `npm install -g @finos/calm-cli@...`
pin, and any installer manifests together so all three stay in agreement.
