FROM golang:latest@sha256:1860373709cc04bacfcf3bb1aaa8c14bb1243e971e9c4e70607ffa05f72466d6 as build
WORKDIR /go/src/github.com/pomerium/cli

# cache depedency downloads
COPY go.mod go.sum ./
RUN go mod download
COPY . .

# build
RUN make build

FROM gcr.io/distroless/base:debug@sha256:65afaf88f6af3d29db56d79d38e03725d72fd193c4311733e44cf18eb0aa594f
WORKDIR /pomerium
COPY --from=build /go/src/github.com/pomerium/cli/bin/* /bin/
ENTRYPOINT [ "/bin/pomerium-cli" ]
