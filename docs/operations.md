# Operations

## Required production configuration

- `SUPPORT_PUBLIC_URL`: canonical HTTPS origin, for example `https://support.obiente.org`.
- `SUPPORT_ENVIRONMENT=production`: rejects a non-HTTPS public URL.
- `SUPPORT_DATA_KEY`: base64-encoded 32-byte key from a cryptographically secure source.
- `DATABASE_URL`: PostgreSQL connection with a dedicated least-privilege database user.
- `SUPPORT_OBJECT_ROOT`: optional object-root override. The production image uses `/data/private`; mount the persistent private-data volume at `/data`.
- `SUPPORT_WEB_ROOT`: built Vue distribution included in the production image.

Terminate HTTPS at a trusted reverse proxy. Do not expose PostgreSQL or the private object volume. Do not enable request-body logging at the proxy.

The production image builds both the Vue portal and Go service and declares a Docker health check against `GET /healthz`. The probe uses `SUPPORT_ADDRESS` when set and defaults to port 8080. It does not require a shell or additional utility in the final image.

## Backups

Back up PostgreSQL and the private object volume as one retention unit. Encrypt backups independently from `SUPPORT_DATA_KEY`, restrict operator access, and apply the same deletion schedule to expired backups. A restore test must verify that database object keys and encrypted files remain consistent.

## Key rotation

The initial foundation supports one active data key. Do not replace it while private reports encrypted under the previous key remain. Dual-key reads and an audited re-encryption job are required before production key rotation. If the active key is exposed, stop intake, preserve audit evidence, notify the incident owner, and treat all retained private payloads as affected.

## Recovery

1. Restore PostgreSQL and the private object volume from the same backup generation.
2. Restore the exact `SUPPORT_DATA_KEY` version used for that generation.
3. Start the service without public traffic and verify `/healthz`.
4. Use synthetic fixtures to test create, reconcile, status, and deletion.
5. Confirm the retention purge completes before reopening intake.

Never inspect a real user's plaintext report as a recovery test.

## Data deletion

Capability deletion soft-revokes the report immediately and removes its diagnostic object. The hourly retention task hard-deletes revoked and expired rows. If object deletion fails, the row remains so cleanup can retry without losing the object reference.

## Incident response

Do not paste private reports, capabilities, contact details, database rows, or decrypted diagnostics into GitHub issues, chat, or CI logs. Use synthetic identifiers in public incident tracking and a separately authorized private evidence path for affected data.
