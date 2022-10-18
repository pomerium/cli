FROM golang:latest@sha256:25de7b6b28219279a409961158c547aadd0960cf2dcbc533780224afa1157fd4 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:9681f07e699fa65958ef9b902399d618cb6a4638868d468cf730a2bafd8f3dcc
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
