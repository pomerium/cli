FROM golang:latest@sha256:63c5d6404238855365d5bac79ed0564a1e1d3bcda916dfc87cfb32d027e28e1a as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:4f9fe9414e01e5398881db4f30d810e832bcaece5f11f3d09a1e541311b2ce53
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
