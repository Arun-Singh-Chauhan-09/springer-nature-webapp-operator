# Base images are pinned by tag AND digest (hack/pin-supply-chain.sh); Dependabot bumps them.

# Build stage: Go is only needed inside Docker, not on your machine.
FROM golang:1.25@sha256:699337d620559a59b4a2bb298ad59611e535d2ee755a34cf2d2a98f37578dc80 AS build
WORKDIR /src
# go.mod/go.sum are committed and checked in CI, so the build never changes dependencies:
# download exactly what go.sum lists and verify the checksums.
COPY go.mod go.sum ./
RUN go mod download && go mod verify
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -mod=readonly -trimpath -ldflags="-s -w" -o /manager ./cmd

# Runtime stage: distroless, non-root, no shell.
FROM gcr.io/distroless/static:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /manager /manager
USER 65532:65532
ENTRYPOINT ["/manager"]
