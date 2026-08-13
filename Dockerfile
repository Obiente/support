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
COPY --from=build /out/support /support
COPY --from=frontend /src/frontend/dist /srv/support-web
COPY --chown=65532:65532 --from=build /out/private /var/lib/obiente-support/private
ENV SUPPORT_WEB_ROOT=/srv/support-web \
    SUPPORT_OBJECT_ROOT=/var/lib/obiente-support/private
VOLUME ["/var/lib/obiente-support/private"]
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/support"]
