FROM golang:1.20.3-buster@sha256:413cd9e04db86fee3f5c667de293f37d9199b74880771c37dcfeb165cefaf424 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:357bc96a42d8db2e4710d8ae6257da3a66b1243affc03932438710a53a8d1ac6
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
