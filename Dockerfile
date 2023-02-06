FROM golang:latest@sha256:bb9811fad43a7d6fd2173248d8331b2dcf5ac9af20976b1937ecd214c5b8c383 as build
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
