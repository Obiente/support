# Operations

## Required production configuration

- `SUPPORT_PUBLIC_URL`: canonical HTTPS origin. The Obiente deployment uses `https://support.obiente.org`.
- `SUPPORT_ENVIRONMENT=production`: requires `SUPPORT_PUBLIC_URL` and rejects HTTP, user information, non-root paths, queries, and fragments.
- `SUPPORT_DATA_KEY`: base64-encoded 32-byte key from a cryptographically secure source.
- `DATABASE_URL`: PostgreSQL connection with a dedicated least-privilege database user.
- `SUPPORT_OBJECT_ROOT`: optional object-root override. The production image uses `/data/private`; mount the persistent private-data volume at `/data`.
- `SUPPORT_WEB_ROOT`: built Vue distribution included in the production image.
- `SUPPORT_ADMIN_USERNAME`: the initial maintainer username. Use a distinct non-email identifier.
- `SUPPORT_ADMIN_PASSWORD_HASH`: a bcrypt hash, never a plaintext password. Generate one with `htpasswd -bnBC 12 admin 'a-long-password' | cut -d: -f2`.

Terminate HTTPS at a trusted reverse proxy. Do not expose PostgreSQL or the private object volume. Do not enable request-body logging at the proxy.

The production image and Compose service default to `SUPPORT_PUBLIC_URL=https://support.obiente.org` and `SUPPORT_ENVIRONMENT=production`. Override the public URL only when deploying a separate support origin. The production image builds both the Vue portal and Go service and declares a Docker health check against `GET /healthz`. The probe uses `SUPPORT_ADDRESS` when set and defaults to port 8080. It does not require a shell or additional utility in the final image.

## Maintainer access

The maintainer console is available at `/admin/login`. Successful logins create a random server-side session lasting 12 hours. The browser receives an HTTP-only, SameSite-strict cookie; production cookies are also marked secure. Admin state changes require a separate rotating CSRF token.

Five failed login attempts from one observed network address block further attempts for 15 minutes in that service process. Put an additional distributed rate limit at the trusted reverse proxy when running more than one replica. Do not expose the admin API through a separate origin.

Changing `SUPPORT_ADMIN_PASSWORD_HASH` affects new logins but does not revoke existing sessions. To force sign-out after a credential incident, delete rows from `support_admin_sessions` as a separately authorized operational action, then rotate the password hash. Audit rows in `support_admin_audit` must follow the same access controls and backup policy as support reports.

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
