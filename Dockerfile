# Build the manager binary
FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.25.1 AS builder

ARG TARGETOS
ARG TARGETARCH
ENV CGO_ENABLED=0

WORKDIR /workspace
COPY . .



# Build shoot-grafter operator and tooling.
RUN --mount=type=cache,target=/go/pkg/mod \
	--mount=type=cache,target=/root/.cache/go-build \
	CGO_ENABLED=${CGO_ENABLED} GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -ldflags="-s -w" -a -o shoot-grafter main.go

# Use distroless as minimal base image to package the shoot-grafter binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM --platform=${BUILDPLATFORM:-linux/amd64} gcr.io/distroless/static:nonroot
LABEL source_repository="https://github.com/cloudoperators/shoot-grafter"
WORKDIR /
COPY --from=builder /workspace/shoot-grafter .
USER 65532:65532

ENTRYPOINT ["/shoot-grafter"]