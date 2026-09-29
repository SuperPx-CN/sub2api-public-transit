FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /transit ./cmd/transit

FROM alpine:3.23
RUN apk add --no-cache ca-certificates && addgroup -g 10001 transit && adduser -D -H -u 10001 -G transit transit
COPY --from=build /transit /usr/local/bin/transit
USER 10001:10001
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/transit"]
