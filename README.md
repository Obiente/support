# Obiente Support

Obiente Support is the product-neutral support portal and private intake service for Obiente projects. It lets people report bugs, suggest features, ask for help, and send explicitly approved diagnostics without needing a GitHub account.

The public portal is a Vue 3 and TypeScript application. A small Go service owns the versioned intake contract, private storage, idempotency, status capabilities, deletion, and retention.

## What this foundation includes

- Bug, feature, and general-support intake for products in a maintained registry.
- A versioned multipart API shared by the web portal and native applications.
- Explicit 4 MiB diagnostic upload bounds and per-product ZIP entry allowlists.
- Idempotent submission and receipt reconciliation after an uncertain response.
- Human-readable support codes and unguessable private status/deletion links.
- Application-level AES-256-GCM encryption for private report fields, capabilities, and diagnostic objects.
- Automatic private-data expiration and immediate deletion through the private capability.
- A second product registration and synthetic contract fixtures to prevent product-specific service code.
- A responsive, keyboard-operable Vue portal with reduced-motion behavior and upload cancellation.

This PR advances issue #1. Moderation queues, maintainer authentication, public tracker promotion, GitHub synchronization, safe follow-up messages, and audited maintainer access remain required before the complete issue can close.

## Local development

Requirements:

- Go 1.24 or newer
- Node.js 22 or newer
- PostgreSQL 17

Create a data key and local environment file:

```bash
cp .env.example .env
openssl rand -base64 32
```

Place the generated value in `SUPPORT_DATA_KEY`, set a database password, then start PostgreSQL:

```bash
docker compose up database
```

Build the Vue portal and run the same-origin service on port 8080:

```bash
cd frontend
npm ci
npm run build
cd ..
set -a
. ./.env
set +a
go run ./cmd/support
```

For Vue hot reload, keep a built portal available for the Go process, set `SUPPORT_PUBLIC_URL=http://localhost:5173`, run `npm run dev`, and open port 5173. Vite proxies `/api` to the Go service on port 8080.

For the production-shaped single-container build:

```bash
docker compose up --build
```

The support image contains both the built Vue portal and Go service. Mount persistent encrypted report storage at `/data`; the service stores diagnostic objects under `/data/private`. Runtime configuration is supplied through environment variables and is not built into the image.

## Validation

```bash
go test ./...
go vet ./...
cd frontend
npm ci
npm run lint
npm test
npm run build
```

## Privacy boundaries

- New reports are private. Nothing is published automatically.
- Diagnostic attachments are optional and must match the selected product's registered schema.
- The service does not store raw idempotency keys or raw status capabilities in database lookup columns.
- Private report text, contact details, receipt capabilities, and diagnostic objects are encrypted before storage.
- Support codes are identifiers, not authentication secrets.
- Private status URLs are bearer capabilities. Applications must never put them in diagnostics, telemetry, or public issues.
- The application does not submit diagnostics automatically, after a crash, or in the background.
- Server logs contain operational failures but no request body, capability path, contact data, diagnostic content, or remote address.

See [docs/threat-model.md](docs/threat-model.md), [docs/operations.md](docs/operations.md), and [openapi.yaml](openapi.yaml) for the initial security and integration contract.

## License

GNU Affero General Public License v3.0. See [LICENSE](LICENSE).
