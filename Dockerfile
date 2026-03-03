FROM golang:1.25-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 go build -o /vnode ./cmd/vnode

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /vnode /vnode

ENTRYPOINT ["/vnode"]
