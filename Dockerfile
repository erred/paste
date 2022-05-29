FROM ghcr.io/seankhliao/gotip AS build
WORKDIR /workspace
ENV CGO_ENABLED=0 \
    GOFLAGS=-trimpath
COPY go.* ./
RUN go mod download
COPY . .
RUN go build -ldflags='-s -w' .

FROM gcr.io/distroless/static
COPY --from=build /workspace/paste /bin/paste
ENTRYPOINT [ "/bin/paste" ]
