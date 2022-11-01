FROM golang:latest@sha256:992d5fea982526ce265a0631a391e3c94694f4d15190fd170f35d91b2e6cb0ba as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:856944e81ffb36babf549e52933e1c09379372135a9b73a97f275fa2973de13a
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
