# syntax=docker/dockerfile:1

FROM golang:1.26-alpine AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath \
	-ldflags="-s -w -X main.version=${VERSION}" \
	-o /twitter-rss .

FROM gcr.io/distroless/static-debian13:nonroot
COPY --from=build /twitter-rss /twitter-rss
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/twitter-rss"]
