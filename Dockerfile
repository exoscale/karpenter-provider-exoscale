FROM golang:1.25.0 AS builder

ARG VERSION
ARG VCS_REF
ARG BUILD_DATE

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/
COPY apis/ apis/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-s -w -X main.commit="${VCS_REF}" -X main.branch="${VCS_BRANCH}" -X main.buildDate="${BUILD_DATE}" -X main.version="${VERSION} \
    -o manager \
    cmd/karpenter-exoscale/main.go

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

ARG VERSION
ARG VCS_REF
ARG BUILD_DATE

LABEL org.label-schema.build-date=${BUILD_DATE} \
      org.label-schema.vcs-ref=${VCS_REF} \
      org.label-schema.vcs-url="https://github.com/exoscale/karpenter-exoscale" \
      org.label-schema.version=${VERSION} \
      org.label-schema.name="karpenter-exoscale" \
      org.label-schema.vendor="Exoscale" \
      org.label-schema.description="Exoscale Karpenter" \
      org.label-schema.schema-version="1.0"

WORKDIR /
COPY --from=builder /workspace/manager /karpenter-exoscale
USER nonroot:nonroot

ENTRYPOINT ["/karpenter-exoscale"]
