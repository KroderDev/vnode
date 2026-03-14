FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags "-X github.com/kroderdev/vnode/internal/version.Version=${VERSION}" -o /vnode ./cmd/vnode

FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=builder /vnode /vnode

ENTRYPOINT ["/vnode"]
