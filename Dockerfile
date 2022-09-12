FROM golang:latest@sha256:d3f734e1f46ec36da8c1bce67cd48536138085289e24cfc8765f483c401b7d96 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:8a7afd5bc2228ca2e6d1186bf964295d99650a9c664ad22e75b02bf4a027aca7
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
