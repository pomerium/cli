FROM golang:latest@sha256:2edf6aab2d57644f3fe7407132a0d1770846867465a39c2083770cf62734b05d as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:8e770ae2610ef67ac2104617e871c3212522a625359be484c951ec119212a896
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
