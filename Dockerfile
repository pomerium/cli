FROM golang:latest@sha256:2d17ffd12a2cdb25d4a633ad25f8dc29608ed84f31b3b983427d825280427095 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:4689543bf8eda8e89710637198c28a5e9c87df53efe8b8f85e2bb8272238da69
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
