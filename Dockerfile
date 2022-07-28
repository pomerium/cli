FROM golang:latest@sha256:6e10f44d212b24a3611280c8035edaf9d519f20e3f4d8f6f5e26796b738433da as build
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
