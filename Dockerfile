FROM golang:latest@sha256:25de7b6b28219279a409961158c547aadd0960cf2dcbc533780224afa1157fd4 as build
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
