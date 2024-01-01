FROM golang:1.21.4-bookworm@sha256:52362e252f452df17c24131b021bf2ebf1c9869f65c28f88ddb326191defea9c as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base-debian12:debug@sha256:996c583af12770668a65722aeab748b4e058feac61f728c01e4763c7f31c7246
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
