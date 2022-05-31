FROM us-central1-docker.pkg.dev/com-seankhliao/build/gotip:latest AS build
WORKDIR /workspace
ENV CGO_ENABLED=0 \
    GOFLAGS=-trimpath
COPY go.* ./
RUN go mod download
COPY . .
RUN go test -vet=all ./... && \
    go build -ldflags='-s -w' .

FROM gcr.io/distroless/static
COPY --from=build /workspace/paste /bin/paste
ENTRYPOINT [ "/bin/paste" ]
