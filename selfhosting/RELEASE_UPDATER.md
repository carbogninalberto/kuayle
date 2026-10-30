# Published-release updater

Use this opt-in flow for a self-hosted instance with an operator-maintained Compose
file and published API/UI images (and, with Dev Machines enabled, published
control-plane and runtime images). The default `update.sh` remains the source-build
workflow and requires a tracking Git branch. The release updater supports detached
checkouts and does not pull into, switch, or reset the operator's checkout.

## Setup

Requirements: Linux, Docker Engine, Compose v2 with `up --wait`, Git, tar and Python 3 with PyYAML. The updater
image includes these tools. The repository must have the Kuayle origin remote and
GitHub/GHCR network access. GHCR must allow anonymous pulls of `kuayle-api`,
`kuayle-web` and, for an active Dev Machines profile, the release's
`kuayle-machine-gateway`, `kuayle-machine-manager` and `kuayle-dev-machine-*`
images (or the Docker daemon must already be logged in to `ghcr.io` with read access). The deployment must use the reference service names,
PostgreSQL's `POSTGRES_USER`/`POSTGRES_DB`, and mapping-style service environments.
The proxy must already serve the `selfhosting/runtime/upgrading` maintenance marker.
Custom deployment secrets and policies remain in the production Compose file and
`.env`; generated configuration and backups have owner-only access.

Configure the updater service with the existing shared token and repository/Docker
socket mounts, plus these values (replace paths and project name):

```yaml
environment:
  UPDATER_TOKEN: ${SYSTEM_UPDATER_TOKEN}
  REPO_ROOT: /opt/kuayle
  UPDATE_SCRIPT: /opt/kuayle/selfhosting/release-update.sh
  RELEASE_COMPOSE_FILE: /opt/kuayle/selfhosting/docker-compose.production.yml
  RELEASE_COMPOSE_PROJECT: kuayle-production
```

Rebuild the updater image to include Python, then recreate only that service using
the deployment's explicit `-p PROJECT -f FILE --profile updater` Compose selection.
Keep the updater private. The existing backend `SYSADMINS`, `SYSTEM_UPDATER_TOKEN`
and `SYSTEM_UPDATER_URL` settings and Settings → Version button apply unchanged.
Both script files may instead be copied into a persistent directory inside the
repository mount, with `UPDATE_SCRIPT` pointing to that copy. This allows an
operator hotfix without modifying a detached release's tracked files. Such a copy
does not change when the release does: refresh both `release-update.sh` and
`release_update.py` in that directory from a release that includes the behavior
you need (for example image pulling for Dev Machines) before running the next update.

To build Dev Machines images on the server instead of pulling them, add
`RELEASE_BUILD_DEV_MACHINE_IMAGES: "true"` to the updater service environment and
recreate the updater service.

## Update behavior

1. Acquire an instance lock and read GitHub's latest published stable release.
2. Pull both release images and record their immutable GHCR digests. Export the
   release tag's source into `selfhosting/runtime/releases/COMMIT` without changing
   the checkout. Release publishing must build its images from the tagged source.
3. Preserve the production Compose configuration, secrets references, networks and
   custom policies. For an active Dev Machines profile, pull the release gateway,
   manager and every runtime image (each `dev-machine-images` service in the
   release Compose file) from `ghcr.io/carbogninalberto/kuayle-SERVICE:TAG`, and
   configure the services and the backend/manager `DEV_MACHINE_*_IMAGE` settings
   with their immutable digests. An image that cannot be pulled (a release
   published before these images, a fork, or no registry access) is built locally
   from the release source under an instance-specific tag, as the output reports;
   `RELEASE_BUILD_DEV_MACHINE_IMAGES=true` builds all of them locally. Newly
   introduced runtime image settings are added to the backend and manager.
   Disabled profiles remain disabled. Compose validation and image preparation
   precede any downtime.
4. Back up configuration and `.env` under `selfhosting/backups/release-TIMESTAMP/`.
   Enable maintenance, stop backend/manager/gateway writers, and dump PostgreSQL
   in custom format. A failed backup restarts the same previous containers.
5. Write the candidate production configuration (JSON, also valid YAML), migrate
   with the new backend, provision gateway grants if active, and start the new
   application and control plane with health waits. The proxy, database, Redis,
   updater, existing machine workloads and unrelated Compose projects are not
   recreated. No Docker pruning or volume deletion is performed.
6. Remove maintenance only after healthy startup. Save version, source, API/UI
   digests, pulled Dev Machines digests (`dev_machine_images`), locally built Dev
   Machines services (`dev_machine_builds`) and backup path in
   `runtime/release-updater-state.json`. Repeating an already
   successful update verifies health and returns without restarting services.

Schema/config architecture changes beyond the reference service contract require
operator review. This flow preserves existing instance settings rather than
replacing the production file with the repository's defaults. It does not refresh
its own running sidecar or remove older runtime images/source exports/backups.

## Failure recovery

Before migrations, the previous containers can be restarted unchanged. Once a
migration has been attempted, failure keeps maintenance enabled and stops writers;
**never restart previous backend/manager images against a potentially newer schema**.
The candidate production configuration remains installed for diagnosis and retry.
Inspect updater logs, preserved service logs and the backup directory. Correct the
cause and retry the update to run idempotent migrations and health checks again.
Do not delete the upgrade marker to bypass a failed migration. A database restore
is an operator action requiring matching pre-update configuration and images.
Keep private backups off-host according to the deployment's backup policy.

## Published images

Release publishing (`.github/workflows/build.yml`) builds each Dev Machines image
natively on amd64 and arm64, runs its smoke tests and HIGH/CRITICAL scan, then
pushes that scanned image as `kuayle-SERVICE:TAG-ARCH` and combines both
architectures into the `kuayle-SERVICE:TAG` and `:latest` manifests the updater
pulls. Pull requests build and scan the same images without pushing.

## Validation

```sh
python3 -m unittest discover -s selfhosting/tests -v
bash selfhosting/update_test.sh
node AGENTS/TOOLS/check-harness.mjs
```

Tests mock command execution and exercise candidate construction, preservation of
instance policy, published Dev Machines image pulls and digest pinning, local-build
fallback for missing images and the forced-build setting, disabled profiles, backup ordering, pre-migration recovery,
migration failure and failed health waits. They do not contact production or prove
registry availability. On deployment, verify authenticated updater status, actual
service health, public readiness, migration version and a repeat no-op update.
