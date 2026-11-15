FROM golang:1.27.3 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/respkv . \
    && mkdir /out/data

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/respkv /respkv
COPY --from=build --chown=nonroot:nonroot /out/data /data
VOLUME /data
EXPOSE 6379
ENTRYPOINT ["/respkv"]
CMD ["-addr", ":6379", "-aof", "/data/appendonly.aof"]
