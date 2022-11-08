FROM golang:latest@sha256:7ffa70183b7596e6bc1b78c132dbba9a6e05a26cd30eaa9832fecad64b83f029 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:cd1bf874ac029cfca6d6a8221f79bb435c5223a3d3eb717e045ca5b0163f2a6c
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
