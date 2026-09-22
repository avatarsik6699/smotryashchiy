# syntax=docker/dockerfile:1
# smotryashchiy server image: node (UI) -> go (static binary) -> distroless nonroot (docs/SPEC.md §4f).
# Base images are pinned by digest; bump them deliberately (and re-run scripts/image-smoke.sh).

FROM node:24-alpine@sha256:ebfe2f90462722a7a4de65e91990e97fe0d401c70e0e762c5b53302f905ec1c1 AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

FROM golang:1.26.6-alpine@sha256:3889b425f035be855a72fb4755265311293b6d414521f0a519d819df32222d83 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./web/dist
# RELEASE is the 40-character git SHA reported by /health/ready and `version`.
ARG RELEASE=development
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags "-s -w -X main.release=${RELEASE}" -o /out/smotryashchiy ./cmd/smotryashchiy \
 && mkdir -p /out/data

FROM gcr.io/distroless/static-debian12@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab
ARG RELEASE=development
LABEL org.opencontainers.image.title="smotryashchiy" \
      org.opencontainers.image.description="Self-contained self-hosted monitoring for solo developers" \
      org.opencontainers.image.source="https://github.com/avatarsik6699/smotryashchiy" \
      org.opencontainers.image.revision="${RELEASE}" \
      org.opencontainers.image.licenses="see LICENSE"
COPY --from=build /out/smotryashchiy /smotryashchiy
# The only writable place: the SQLite database and its WAL. Owned by the nonroot user (65532).
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
ENV SMOTRYASHCHIY_ADDR=:8080 \
    SMOTRYASHCHIY_DB_PATH=/data/smotryashchiy.db
VOLUME ["/data"]
EXPOSE 8080/tcp 51820/udp
# No shell or curl in this image: the binary probes its own readiness endpoint.
HEALTHCHECK --interval=30s --timeout=5s --start-period=15s --retries=3 CMD ["/smotryashchiy", "healthcheck"]
ENTRYPOINT ["/smotryashchiy"]
CMD ["server"]
