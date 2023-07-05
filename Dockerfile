FROM golang:1.20-rc-bullseye AS build

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

COPY --from=build /kubernetes-database-scaler /kubernetes-database-scaler

USER nonroot:nonroot

ENTRYPOINT ["/kubernetes-database-scaler"]
