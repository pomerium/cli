FROM golang:latest@sha256:63c5d6404238855365d5bac79ed0564a1e1d3bcda916dfc87cfb32d027e28e1a as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:2956285aeb60733478311477527e80cac877c90b073f8e00319aa6f90d6bfee4
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
