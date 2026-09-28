FROM golang:1.26-alpine AS build

WORKDIR /src
COPY go.mod go.sum ./
COPY cmd ./cmd
COPY internal ./internal
COPY migrations ./migrations

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/trip-service ./cmd/trip-service

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/trip-service /trip-service

USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/trip-service"]
