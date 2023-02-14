FROM golang:latest@sha256:9be8859445523843084e09747a6f25aee06ce92d23ae320e28d7f101dd6a39e2 as build
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
