FROM golang:latest@sha256:54184d6d892f9d79dd332a6794bb11085c3f8b31f8be8e0911bed4df80044c93 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:9283685c6be8f12cec61d9b6812ed71a6ca9c8cebe211c8df7dbc4d1194591bb
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
