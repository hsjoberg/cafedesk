# Builds the Cafedesk server with the admin inside it.
# The build context is the repository's root:
#   docker build -f apps/server/Dockerfile .

FROM oven/bun:1 AS admin
WORKDIR /src/apps/admin
COPY apps/admin/package.json apps/admin/bun.lock ./
RUN bun install --frozen-lockfile
COPY api /src/api
COPY apps/admin ./
# Writes the build into ../server/web/dist.
RUN bun run build

FROM golang:1.27-alpine AS server
# Fetch the Go version go.mod asks for, if the image's is older.
ENV GOTOOLCHAIN=auto
WORKDIR /src/apps/server
COPY apps/server/go.mod apps/server/go.sum ./
RUN go mod download
COPY apps/server ./
COPY --from=admin /src/apps/server/web/dist ./web/dist
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/server .

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=server /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
