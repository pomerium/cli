FROM golang:latest@sha256:25de7b6b28219279a409961158c547aadd0960cf2dcbc533780224afa1157fd4 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:6ef742b9373ebe1ae90c91116b66276811bcd3ae4c9ec6456e41ed187cd3d6a8
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
