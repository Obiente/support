# Initial threat model

This document covers private intake and the initial single-maintainer moderation console. It must be extended when public promotion, follow-up messaging, or GitHub synchronization is introduced.

## Protected data

- Unreviewed titles and descriptions.
- Optional contact details.
- Diagnostic archives and their contents.
- Private status and deletion capabilities.
- Idempotency keys while held by a submitting client.
- The encryption key and database credentials.
- The admin password hash, server-side sessions, CSRF tokens, and audit records.

Support codes, product IDs, request types, timestamps, and moderation states are not secrets by themselves, but they are not public tracker entries until moderation explicitly promotes them.

## Trust boundaries

1. The user's browser or application constructs a report after explicit review.
2. The HTTPS reverse proxy terminates public transport security.
3. The Go API validates and bounds metadata and archives before persistence.
4. PostgreSQL stores lookup metadata and encrypted private payloads.
5. A private filesystem volume stores encrypted diagnostic objects.
6. An authenticated maintainer browser receives decrypted report details and explicitly requested diagnostic downloads.
7. Future public tracker and GitHub integrations are outside this boundary.

## Controls

### Guessing and enumeration

Private status capabilities contain 256 bits of randomness. Database lookups use SHA-256 hashes of capabilities. Invalid and deleted capabilities return the same not-found response. Support codes cannot access a report.

### Replay and duplicate submission

Clients provide a 256-bit idempotency key. The service stores only its hash and binds it to a hash of the normalized metadata and archive. A retry returns the original encrypted receipt capability. Reusing the same key for different content is rejected.

### Archive attacks

The HTTP body, metadata part, compressed archive, expanded archive, entry count, content type, and entry names are bounded. Every ZIP entry must match the selected product's registry. Nested paths, traversal, duplicates, special files, and unregistered entries are rejected. Archives are never extracted on the intake host.

### Data disclosure

Private fields, the recoverable receipt capability, and diagnostic objects use AES-256-GCM with random nonces and report-specific associated data. Database lookup columns contain hashes rather than bearer secrets. The service does not log request bodies, remote addresses, capability paths, or attachment names.

Infrastructure still needs encrypted PostgreSQL and volume storage, protected backups, and encrypted backup transport. Application encryption does not replace host and backup encryption.

### Retention and deletion

Each product defines a private-data retention period. Expired and user-deleted records are purged hourly. Diagnostic object deletion must succeed before database hard deletion so an orphaned encrypted object remains discoverable for a later retry. Deleting a live report immediately revokes its capability.

### Browser attacks

The service sends a restrictive Content Security Policy, denies framing, disables MIME sniffing, uses same-origin API requests, and sets a no-referrer policy. Private status responses and SPA routes are not cacheable. The Vue application renders user-safe API fields rather than private report text.

### Maintainer authentication and access

The service accepts one configured username and bcrypt password hash. Successful authentication creates a random 256-bit server-side session with a 12-hour expiry. Its cookie is HTTP-only, SameSite-strict, and secure when the canonical origin uses HTTPS. Mutating admin requests require a separate rotating 256-bit CSRF token. Failed logins are throttled per directly observed network address.

List, detail, diagnostic download, status change, login, and logout actions are recorded in PostgreSQL. The audit trail stores a keyed hash of the observed network address rather than the address itself. Private payloads and diagnostics are decrypted only after session validation. The console does not create public issues or publish private fields.

## Known incomplete controls

- Abuse scoring, accessible challenge escalation, and distributed rate limits.
- Multiple maintainer identities, role-based authorization, second-factor authentication, and immediate session revocation when the configured password changes.
- Exact public-field preview and approval for tracker promotion.
- Malware inspection beyond strict product schemas.
- Key rotation with dual-key reads and re-encryption.
- Backup restoration exercises and deletion propagation into backups.
- Public tracker and GitHub bridge threat boundaries.

These are release gates for the corresponding later capabilities, not claims of current support.
