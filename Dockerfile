#syntax=docker/dockerfile:1

FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/hub ./cmd/hub

FROM gcr.io/distroless/static-debian12:nonroot AS runtime
COPY --from=build /out/hub /usr/local/bin/hub
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/hub"]
CMD ["serve"]
