FROM golang:1.24-trixie AS build

WORKDIR /build

COPY . .

RUN make

FROM debian:trixie

RUN apt-get update && \
    apt-get install -y --no-install-recommends ca-certificates && \
    rm -rf /var/lib/apt/lists/*

EXPOSE 1234

COPY --from=build /build/server /server

CMD ["/server"]
