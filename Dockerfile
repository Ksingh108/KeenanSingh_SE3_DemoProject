# Build stage
FROM node:18-alpine AS css-builder

WORKDIR /app

# Copy package files and build CSS
COPY package.json tailwind.config.js ./
COPY web/static/input.css ./web/static/
COPY web/static/*.html ./web/static/
RUN npm install && npm run build-css-prod

# Go build stage
FROM golang:1.25.2-alpine AS builder

WORKDIR /app

# Copy go mod files. go.sum is copied too so the build uses exactly the
# dependency versions and checksums that are committed; `go mod verify` fails
# the build if a downloaded module does not match them. (`go mod tidy` is
# deliberately not run here: it could silently rewrite go.mod/go.sum.)
COPY go.mod go.sum ./
RUN go mod download && go mod verify

# Copy source code
COPY . .

# Build the application from cmd/server
RUN CGO_ENABLED=0 GOOS=linux go build -o main ./cmd/server

# Runtime stage. Pinned to the Alpine release the golang builder image is based
# on, so rebuilds are repeatable and base-image upgrades are deliberate.
FROM alpine:3.22

RUN apk --no-cache add ca-certificates \
    && addgroup -S app \
    && adduser -S -G app -H -s /sbin/nologin app

WORKDIR /app

# Copy the binary and static files. They stay owned by root and are read-only
# to the app user, so a compromised process cannot modify its own code.
COPY --from=builder /app/main .
COPY --from=css-builder /app/web/static ./web/static

# Run as an unprivileged user; the server listens on 8080, which needs no privileges.
USER app

EXPOSE 8080

CMD ["./main"]