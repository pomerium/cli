FROM golang:latest@sha256:1860373709cc04bacfcf3bb1aaa8c14bb1243e971e9c4e70607ffa05f72466d6 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:ec73486ea23cdff10230ef3eab795b5bf8c72afd88e0e74ee49ccd447bc7b07b
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
