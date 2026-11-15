# Backups

Instance sysadmins schedule backups from **Settings → Backups**. Each backup is
one archive with a PostgreSQL dump and the uploaded files, written to a local
backup directory. A copy can also go to an S3-compatible bucket. The
[release updater](../selfhosting/RELEASE_UPDATER.md)'s pre-upgrade dump appears
in the same history.

## Who can use it

A backup contains every workspace, so the page and its API
(`/api/system/backups/*`) are limited to instance **sysadmins**: the user IDs
listed in `SYSADMINS` (see [Updating](self-hosting.md#updating) for how to find
your ID). Workspace owners and admins who are not sysadmins see neither the page
nor the data. Personal access tokens cannot reach these routes, and neither can
a session signed in through a workspace's SSO.

The issue asked for workspace owner/admin; backups contain every workspace, so
access is limited to instance sysadmins.

## Configure

| Setting | Meaning |
| --- | --- |
| Enabled | Runs scheduled backups. **Back up now** works either way. |
| Schedule | Daily or weekly at `HH:MM` **UTC**, or a 5-field cron expression in UTC (`minute hour day month weekday`; `*`, numbers, `a-b` ranges, lists and `*/n` or `a-b/n` steps; weekday 0–6 with Sunday as 0, 7 also meaning Sunday). |
| Retention | Keep the last *N* successful backups, or the ones from the last *N* days (1–1000). |
| S3 destination | Endpoint (empty for AWS), bucket, region, prefix, access key and secret key. **Test connection** writes and deletes a small object under the prefix. Blank keys reuse the saved ones only for the saved endpoint; to test a different endpoint, enter both keys. |
| Encryption | Optional client-side encryption with a passphrase of at least 12 characters. **Without the passphrase a backup cannot be restored.** Keep it outside the instance. |

Secrets (S3 keys and the passphrase) are encrypted at rest with a key derived
from `JWT_SECRET` and are write-only: the API and the page only say whether one
is stored. Leave a secret field blank to keep it, or use **Clear** to remove it.
Errors, logs and history never contain secrets or the database password.

Operator settings (environment of the `backend` service):

| Variable | Default | Meaning |
| --- | --- | --- |
| `BACKUP_DIR` | `./backups` (`/app/backups`, the `backups` volume, in Compose) | Where archives are written. |
| `BACKUP_TIMEOUT` | `2h` | Maximum duration of one run, upload included. |
| `BACKUP_S3_ALLOW_PRIVATE_NETWORK` | `false` | Allow the S3 endpoint to be a private or loopback address, such as a MinIO on the same host. Off by default: the endpoint is user-configured and dials only public addresses, without following redirects. |

The backend image includes `pg_dump` (PostgreSQL 17 client, matching the Compose
`postgres:17-alpine` image). The database password reaches `pg_dump` in
`PGPASSWORD`, never on its command line.

## How a run works

- **One at a time.** A run takes a lease in the database (`backup_settings`
  lease columns, 10-minute TTL renewed every 2 minutes). With several backend
  replicas only one runs a backup; **Back up now** returns 409 while another
  runs, and a scheduled slot that finds the lease taken is skipped. A run left
  `running` by a stopped replica is marked failed ("interrupted") once its lease
  expires.
- **Scheduling.** Every replica checks once a minute; the due slot is claimed
  with a compare-and-set on `next_run_at`, so it runs once.
- **Archive.** The archive is written to `<file>.partial`, synced and renamed.
  The SHA-256 of the final file is stored in the history and in the
  `<file>.sha256` sidecar (`sha256sum -c` format). Files and the directory are
  owner-only.
- **Upload.** When S3 is enabled the archive and its `.sha256` are uploaded
  under `<prefix>/`, with the checksum in the `x-amz-meta-sha256` metadata.
  Failed uploads are retried 4 times with exponential backoff (1 s, 2 s, 4 s,
  8 s, plus jitter). A failed upload keeps the local backup; the run shows
  `upload failed`. Archives up to 64 MiB are sent with one `PutObject`; larger
  ones use a multipart upload (parts of at least 64 MiB, at most 9,000 parts),
  which is aborted on failure so no parts are left behind. Each run records the
  endpoint and bucket its copy went to.
- **Retention.** After a successful run, backups outside the policy are deleted
  locally and from the bucket. Retention never deletes the newest successful
  backup, a backup that is still uploading or running, or the updater's
  pre-upgrade dumps (they live on the host). A backup is marked deleted only
  once all its copies are gone. A backup whose copy went to a different endpoint
  or bucket than the current settings is kept whole (local file, copy and
  history row) and logged as a warning after every run, because deleting it
  through the new settings would report success while the old object stays.
  Remove those copies from the previous bucket yourself, or point the settings
  back at it until retention has cleaned them up.
- **Failure notifications.** When a scheduled run fails, or its upload fails,
  every sysadmin gets a `backup_failed` inbox notification (in the first
  workspace and team they belong to). Manual runs report in the history only.

## Archive format

`<UTC timestamp>-<run id>.tar.gz` (or `.tar.gz.enc` when encrypted) is a gzip
tar holding:

| Entry | Content |
| --- | --- |
| `database.dump` | `pg_dump --format=custom --no-owner` of the whole database. |
| `uploads/…` | The uploaded files when `STORAGE_TYPE=local` (symbolic links are skipped, and so are files deleted while the backup reads the folder; the manifest counts only what was archived). |
| `uploads-manifest.json` | When `STORAGE_TYPE=s3`: the list of upload objects (`key`, `size`, `etag`). The objects themselves stay in the upload bucket; back that bucket up with your provider's tools. |
| `manifest.json` | `format_version`, `created_at`, `kind`, `app_version`, `database` (`file`, `size`, `sha256`) and `uploads` (`mode`: `files` or `s3-manifest`, `count`, `bytes`). |

### Encrypted archives

An encrypted archive is the archive above, encrypted as a stream:

- Header: the 9 bytes `KUAYLEBK1`, a 16-byte random salt and the chunk size as
  a big-endian `uint32` (65536).
- Key: `scrypt(passphrase, salt, N=32768, r=8, p=1)`, 32 bytes, for AES-256-GCM.
- Body: chunks of 65536 plaintext bytes (the last one shorter, possibly empty),
  each sealed with AES-256-GCM (16-byte tag). The 12-byte nonce is three zero
  bytes, the chunk index as a big-endian `uint64` and a final-chunk flag byte
  (1 on the last chunk). The header is the additional authenticated data, so a
  reordered, truncated or extended file fails to decrypt.

Decrypt with `server backup decrypt <in> <out>` from the backend image (step 3
of the [restore](#restore)). The passphrase is read from
`KUAYLE_BACKUP_PASSPHRASE` or the first line of standard input, never from the
command line. The command needs no database or configuration.

## Verify

**Verify** in the history re-reads a backup (the local file, or the bucket copy
when the local file is gone and the copy is in the endpoint and bucket
configured now) and checks that its SHA-256 matches the recorded
one, that it decrypts and decompresses completely, that `manifest.json` is
present, that `database.dump` starts with the pg_dump custom-format signature
and matches the size and SHA-256 in the manifest, and that the number of
uploaded files matches. It does not run `pg_restore`; the restore procedure
below lists the dump's contents as a final check. An encrypted backup is
verified with the passphrase stored now, so changing the passphrase makes
older backups fail to verify (they still decrypt with their own passphrase).

The updater's pre-upgrade dumps are stored on the host and cannot be read by
the backend; check them with the commands in step 2 below.

## Restore

There is no one-click restore. Restore on the same Kuayle version that made the
backup (see `app_version` in `manifest.json`), with the same `JWT_SECRET` so
stored credentials stay readable. Run the commands from `selfhosting/`; replace
`FILE` with the backup name.

1. **Stop the writers** so nothing changes the database during the restore:

   ```sh
   docker compose stop backend frontend
   # and, when Dev Machines are enabled:
   docker compose stop machine-manager machine-gateway
   ```

2. **Copy the backup out of the volume and check its checksum.** For a backup
   copied from the bucket, download both the archive and its `.sha256`.

   ```sh
   mkdir -p restore && chmod 700 restore && cd restore
   docker compose cp backend:/app/backups/FILE.tar.gz .            # or FILE.tar.gz.enc
   docker compose cp backend:/app/backups/FILE.tar.gz.sha256 .
   sha256sum -c FILE.tar.gz.sha256
   ```

   For an updater pre-upgrade dump, compare the checksum shown in the history
   with `sha256sum selfhosting/backups/release-*/database.dump`, then continue
   at step 5 with that file.

3. **Decrypt** an encrypted backup (see [Encrypted archives](#encrypted-archives)),
   for example with the backend image:

   ```sh
   docker compose run --rm --no-deps -T --user "$(id -u):$(id -g)" -v "$PWD":/work \
     --entrypoint /app/server backend \
     backup decrypt /work/FILE.tar.gz.enc /work/FILE.tar.gz < passphrase.txt
   ```

4. **Extract** the archive and list the dump to check it is readable:

   ```sh
   tar xzf FILE.tar.gz
   cat manifest.json
   docker compose exec -T postgres pg_restore --list < database.dump | head
   ```

5. **Restore the database.** This replaces the objects in the dump:

   ```sh
   docker compose exec -T postgres sh -c \
     'pg_restore --clean --if-exists --no-owner -U "$POSTGRES_USER" -d "$POSTGRES_DB"' \
     < database.dump
   ```

6. **Restore uploads.** With `uploads/` in the archive, copy it back into the
   `uploads` volume:

   ```sh
   docker compose run --rm --no-deps --user root -v "$PWD/uploads":/restore:ro --entrypoint sh backend \
     -c 'cp -a /restore/. /app/uploads/ && chown -R appuser:appgroup /app/uploads'
   ```

   With `uploads-manifest.json`, restore the upload bucket from your provider's
   backup and compare it with the listed keys and sizes.

7. **Restart the services** and check the instance:

   ```sh
   docker compose up -d
   ```

Delete the `restore/` directory afterwards: it contains the whole database.
