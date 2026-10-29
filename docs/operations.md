# Operations

## Required production configuration

- `SUPPORT_PUBLIC_URL`: canonical HTTPS origin. The Obiente deployment uses `https://support.obiente.org`.
- `SUPPORT_ENVIRONMENT=production`: requires `SUPPORT_PUBLIC_URL` and rejects HTTP, user information, non-root paths, queries, and fragments.
- `SUPPORT_DATA_KEY`: base64-encoded 32-byte key from a cryptographically secure source.
- `DATABASE_URL`: PostgreSQL connection with a dedicated least-privilege database user.
- `SUPPORT_OBJECT_ROOT`: optional object-root override. The production image uses `/data/private`; mount the persistent private-data volume at `/data`. See [Persistent storage](#persistent-storage).
- `SUPPORT_WEB_ROOT`: built Vue distribution included in the production image.
- `SUPPORT_ADMIN_USERNAME`: the initial maintainer username. Use a distinct non-email identifier.
- `SUPPORT_ADMIN_PASSWORD_HASH`: a bcrypt hash, never a plaintext password. Generate one with `htpasswd -bnBC 12 admin 'a-long-password' | cut -d: -f2`.
- `SUPPORT_STORAGE_ADOPT`: recovery-only override. Leave it unset in normal operation. See [Storage identity refusal](#storage-identity-refusal).

Terminate HTTPS at a trusted reverse proxy. Do not expose PostgreSQL or the private object volume. Do not enable request-body logging at the proxy.

The production image and Compose service default to `SUPPORT_PUBLIC_URL=https://support.obiente.org` and `SUPPORT_ENVIRONMENT=production`. Override the public URL only when deploying a separate support origin. The production image builds both the Vue portal and Go service and declares a Docker health check against `GET /healthz`. The probe uses `SUPPORT_ADDRESS` when set and defaults to port 8080. It does not require a shell or additional utility in the final image.

## Maintainer access

The maintainer console is available at `/admin/login`. Successful logins create a random server-side session lasting 12 hours. The browser receives an HTTP-only, SameSite-strict cookie; production cookies are also marked secure. Admin state changes require a separate rotating CSRF token.

Five failed login attempts from one observed network address block further attempts for 15 minutes in that service process. Put an additional distributed rate limit at the trusted reverse proxy when running more than one replica. Do not expose the admin API through a separate origin.

Maintainers can delete a report from the console once it is no longer needed. See [Retention and deletion](#retention-and-deletion).

Changing `SUPPORT_ADMIN_PASSWORD_HASH` affects new logins but does not revoke existing sessions. To force sign-out after a credential incident, delete rows from `support_admin_sessions` as a separately authorized operational action, then rotate the password hash. Audit rows in `support_admin_audit` must follow the same access controls and backup policy as support reports.

## Persistent storage

Reports are kept until they are deleted, so the private object volume is durable product data, not a cache. The production `/data` mount must be a persistent volume that the deployment platform reattaches on every redeploy, restart, and image update, together with the same PostgreSQL database and `SUPPORT_DATA_KEY`.

The image does not declare a Docker `VOLUME`. A declared volume made Docker create a new anonymous volume for any container started without an explicit mount, so a redeploy could silently switch to an empty `/data` while the database kept referencing objects on the old one. Without a mount, the service now fails at the writable probe on a read-only root filesystem (as in `compose.yaml`) or at the storage identity check. The Compose file mounts the named `private_reports` volume at `/data`; Docker fills an empty named volume from the image's `/data/private` directory, including its non-root ownership.

At startup the service:

1. Creates the object root if needed, then creates, syncs, and removes a probe file in it. If any step fails, the service refuses to start and logs `refusing to start without writable persistent private storage` with the cause.
2. After database migrations, compares `<object root>/.storage-id` with the storage ID recorded in the `support_storage_identity` table:
   - When neither exists, it writes a new random ID to the marker (synced temporary file, mode `0600`, linked into place without replacing an existing marker) and then records it in the database.
   - When only the marker exists, the database adopts its ID.
   - When the database has an ID and the marker is missing or different, the service refuses to start. See [Storage identity refusal](#storage-identity-refusal). It never rewrites the marker or the database on its own.
3. Checks every live report that has a diagnostic attachment, and counts stored `*.enc` objects that no report row references. It logs `private diagnostic objects verified` with `object_root`, `attached`, `missing`, and `orphaned` when every referenced object is present and nothing is orphaned. Otherwise it logs a warning with the same fields and `unknown`.

These checks only read object metadata. They never delete, resolve, or otherwise change a report or an object, and they never log object keys. A warning with `missing` above zero usually means the original volume was not reattached; follow [Restoring missing diagnostic objects](#restoring-missing-diagnostic-objects). A count of `orphaned` objects usually means deletions ran while a different volume was mounted; see [Orphaned diagnostic objects](#orphaned-diagnostic-objects).

The marker starts with a dot and never matches the object key format, so it is never listed, counted, or served as a diagnostic object. Include it in volume backups.

## Backups

Back up PostgreSQL and the private object volume together. Encrypt backups independently from `SUPPORT_DATA_KEY` and restrict operator access. Reports do not expire, so backups do not age out on an expiry schedule; keep each backup generation only as long as it is needed for recovery. A report deleted after a backup was taken remains in that backup until the backup itself is removed. A restore test must verify that database object keys and encrypted files remain consistent.

## Key rotation

The initial foundation supports one active data key. Do not replace it while private reports encrypted under the previous key remain. Dual-key reads and an audited re-encryption job are required before production key rotation. If the active key is exposed, stop intake, preserve audit evidence, notify the incident owner, and treat all retained private payloads as affected.

## Recovery

1. Restore PostgreSQL and the private object volume from the same backup generation.
2. Restore the exact `SUPPORT_DATA_KEY` version used for that generation.
3. Start the service without public traffic and verify `/healthz`.
4. Check the startup log for `private diagnostic objects verified` with `missing` 0.
5. Use synthetic fixtures to test create, reconcile, status, and deletion.
6. Restoring an older generation brings back reports deleted after that backup was taken. Repeat those deletions, using the admin audit trail (`report_deletion_requested`) and reporter requests as the record, before reopening intake.

Never inspect a real user's plaintext report as a recovery test.

## Storage identity refusal

The error `refusing to start: private object root is not the volume this database was written against` names the object root, the storage ID it found (or that the marker is missing), and the first characters of the storage ID the database expects. It means the container mounted a different `/data` than the one the database was written against. Starting anyway would let uploads land on the wrong volume and let deletions report success for objects that are not there.

1. Find the volume the container actually mounts:

   ```bash
   docker inspect <container> --format '{{json .Mounts}}'
   ```

   A mount of type `volume` with a random 64-character name, or no mount at `/data`, means the container is not using the intended persistent volume. Compare the result with the volume the deployment is supposed to use, and list candidate volumes with `docker volume ls`.
2. Find the right volume by its marker. Each candidate volume's `private/.storage-id` holds a 64-character hex ID; the right one starts with the expected ID printed in the error.
3. Mount that volume at `/data` and start the service again. Do not copy objects between volumes while the service runs.
4. If the original volume is gone, restore it from the newest verified backup (the backup includes `.storage-id`), mount it at `/data`, and start the service. Then follow [Restoring missing diagnostic objects](#restoring-missing-diagnostic-objects) for attachments newer than that backup.
5. Only when the original volume cannot be recovered, or a backup taken before storage markers existed is restored, start the service once with `SUPPORT_STORAGE_ADOPT=1`. This rebinds the database to the mounted volume (writing a new marker if the volume has none) and logs an error-level `SUPPORT_STORAGE_ADOPT rebound the database` entry with the previous and new storage IDs. Remove the variable immediately afterwards; while it is set, the service would accept any volume. Reports whose objects are not on the adopted volume remain listed with unavailable diagnostics.

## Restoring missing diagnostic objects

Use this runbook when the startup warning reports `missing` above zero, the console shows "Diagnostic ZIP unavailable", or a download returns `503 diagnostics_unavailable`.

1. Do not delete, resolve, or edit the affected reports. Their metadata and conversations are intact; only the encrypted attachment file is absent.
2. Confirm the deployment mounts the intended persistent volume at `/data` and that `object_root` in the log is the expected path (`/data/private` in the production image). If the platform attached a new empty volume, stop the service, reattach the original volume, start the service, and check the startup log again.
3. If the original volume is lost, list the object keys that live reports still reference:

   ```sql
   SELECT diagnostic_object_key FROM support_reports
   WHERE deleted_at IS NULL AND diagnostic_object_key IS NOT NULL;
   ```

4. From the most recent verified backup of the private object volume, copy only those files into the object root. Do not overwrite files that already exist, and do not restore files that are no longer referenced: they belong to reports that were deleted after the backup was taken.
5. Keep the restored files owned by the service user (UID 65532 in the production image) with mode `0600`. They must have been encrypted under the active `SUPPORT_DATA_KEY`.
6. Restart the service and confirm the warning is gone or `missing` dropped. Each object is bound to its report ID, so a misplaced file fails to decrypt instead of exposing another report.
7. Attachments uploaded after the newest backup cannot be recovered. Leave those reports in place and, if the diagnostics are still needed, ask the reporter for a new diagnostic report through the private conversation.

Do not restore the database from an older backup only to recover objects: that would roll back newer reports, messages, status changes, and deletions.

## Orphaned diagnostic objects

An orphaned object is an `*.enc` file in the object root that no report row references, live or revoked. The usual cause is a report row purged while a different volume was mounted: the object deletion found no file on that volume, treated the missing file as already deleted, and the row was removed, leaving the real file behind on the original volume.

Objects are AES-256-GCM sealed with the associated data `<reportID>:diagnostics`. Once a report row is gone, its report ID cannot be recovered from the object, so the object cannot be opened through the service. Do not write or run a tool that bypasses this binding to read orphaned objects.

The service only counts orphans; it never deletes them. They are private data that a reporter or the old retention policy already deleted. After confirming with the incident owner that no report row will be restored for them (for example from a database backup that still has the rows), remove them as a separately authorized operational action and record the count in the incident notes, never the keys.

## Retention and deletion

Reports, their conversations, and their diagnostic attachments are kept until they are deleted. The service never deletes a report because of its age, and the `retention_until` database column is kept only for compatibility and is always NULL. Contract version 1 responses still contain `retentionUntil`, always `createdAt` plus 30 days, because released clients require it; it is not a deletion date.

A report is deleted in one of three ways:

- The reporter deletes it through the private status link (`DELETE /api/v1/reports/{capability}`).
- A pending submission is cancelled with its idempotency key.
- A maintainer deletes it from the console with **Delete report** once it is no longer needed, for example after the request is resolved (`DELETE /api/v1/admin/reports/{id}`). This requires the admin session and CSRF token, and the request is recorded as `report_deletion_requested` in `support_admin_audit` before it runs.

Each path immediately revokes the report's private link and removes its diagnostic object. An hourly task, which also runs at startup, then hard-deletes the revoked rows and their conversations. If object deletion fails, the row remains revoked so cleanup can retry without losing the object reference; an admin deletion in that state returns an error, and the report no longer appears in the queue.

## Incident response

Do not paste private reports, capabilities, contact details, database rows, or decrypted diagnostics into GitHub issues, chat, or CI logs. Use synthetic identifiers in public incident tracking and a separately authorized private evidence path for affected data.
