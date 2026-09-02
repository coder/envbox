FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY device-plugins/go.mod go.mod
COPY device-plugins/main.go main.go
RUN go mod tidy
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags='-s -w' -o /out/kvm-device-plugin ./main.go

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/kvm-device-plugin /kvm-device-plugin
ENTRYPOINT ["/kvm-device-plugin"]
