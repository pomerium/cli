FROM golang:latest@sha256:54184d6d892f9d79dd332a6794bb11085c3f8b31f8be8e0911bed4df80044c93 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:8848703b0a0d203b9807bda4344716cc24ad760e0686d550c52d6224680f2aac
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
