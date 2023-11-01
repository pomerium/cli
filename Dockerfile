FROM golang:1.21.3-bookworm@sha256:d0214956a9c50c300e430c1f6c0a820007ace238e5242c53762e61b344659e05 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base-debian12:debug@sha256:5e24c7a60ad746d78fd96034b6d043c00ef6ed94ec55ee7882d93162c939f3a1
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
