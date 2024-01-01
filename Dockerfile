FROM golang:1.21.5-bookworm@sha256:1415bb0b25d3bffc0a44dcf9851c20a9f8bbe558095221d931f2e4a4cc3596eb as build
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
