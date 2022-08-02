FROM golang:latest@sha256:fb249eca1b9172732de4950b0fb0fb5c231b83c2c90952c56d822d8a9de4d64b as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:3a6219499a89088ff5d37ce8fd3e3a61fccb75ef05a4e0ba2092ea92d380f48f
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
