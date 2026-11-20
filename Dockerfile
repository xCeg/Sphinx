FROM golang:1.24-alpine AS build
WORKDIR /src
COPY go.mod ./
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /sphinx ./cmd/sphinx

FROM scratch
COPY --from=build /sphinx /sphinx
WORKDIR /work
ENTRYPOINT ["/sphinx"]
