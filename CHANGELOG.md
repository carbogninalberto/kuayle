# Changelog

## Unreleased

- Custom workspace roles support per-team overrides. Self-hosted installations with multiple owner memberships in one workspace must reconcile them before migration 202. The [upgrade guide](docs/self-hosting.md#before-migration-202-workspace-roles) includes a preflight query and recovery instructions.
- Settings → Instance configuration (sysadmins only) lists every variable the API reads, with its value and source; secrets show only whether they are set. `WORKFLOW_PIPELINES_ENABLED`, `DISABLE_REGISTRATION` and `BACKUP_TIMEOUT` can be changed there without a restart (migration 211). Team workflows (pipelines) are now on by default; set `WORKFLOW_PIPELINES_ENABLED=false` to keep them off. See [instance configuration](docs/self-hosting.md#instance-configuration).
