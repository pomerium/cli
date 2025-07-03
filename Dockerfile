FROM golang:1.24.4-bookworm@sha256:2b85dcbf57258bceaa4599bc29efd92824b19b5f9a93b373b9df0856b8127cba as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base-debian12:debug@sha256:7d1d72086ccf7b5c7e0f612dd59ae064765a529daafaecac97ea4a8b48b69e93
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
