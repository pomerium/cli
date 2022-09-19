FROM golang:latest@sha256:4c8f4b8402a868dc6fb3902c97032b971d0179fbe007be408b455697e98d194a as build
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
