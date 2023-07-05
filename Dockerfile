FROM golang:1.21rc2-alpine3.18 AS builder

WORKDIR /app

COPY go.mod ./
COPY go.sum ./
RUN go mod download

COPY *.go ./
ADD pkg ./pkg
ADD cmd ./cmd

RUN go build -o /kubernetes-database-scaler

FROM alpine:3

WORKDIR /

COPY --from=builder /kubernetes-database-scaler /kubernetes-database-scaler

ENTRYPOINT ["/kubernetes-database-scaler"]
