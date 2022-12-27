FROM golang:latest@sha256:660f138b4477001d65324a51fa158c1b868651b44e43f0953bf062e9f38b72f3 as build
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
