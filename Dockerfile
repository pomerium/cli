FROM golang:1.20.4-buster@sha256:4cf6dc46fc03a7aecde79ccc135d875129145b065cb1d29747ac7c8d86979266 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:ba4cde5fe426c9ac93a3ba103d15a917c8b9a05837fca7bdcc313c3110dd9766
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
