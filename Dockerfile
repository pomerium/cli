FROM golang:1.20.5-buster@sha256:b0f97bfb6270353ab877014a44966138b3b31cdc0cea972697008e9f448f7176 as build
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
