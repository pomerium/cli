FROM golang:latest@sha256:bf4b15c14a715b9c2d278809254268d57dd74bbee69489e781e9303c814160d5 as build
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
