FROM node:24-alpine AS frontend
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci
COPY frontend/ ./
RUN npm run build

FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/support ./cmd/support
RUN mkdir -p /out/private

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /home/nonroot
COPY --from=build /out/support /support
COPY --from=frontend /src/frontend/dist ./frontend/dist
COPY --chown=65532:65532 --from=build /out/private ./data/private
VOLUME ["/home/nonroot/data/private"]
EXPOSE 8080
HEALTHCHECK --interval=30s --timeout=5s --start-period=30s --retries=3 CMD ["/support", "healthcheck"]
USER nonroot:nonroot
ENTRYPOINT ["/support"]
