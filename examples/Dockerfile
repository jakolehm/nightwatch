FROM golang:1.13.10-alpine3.11 as builder

WORKDIR /build
COPY . .

RUN go build

FROM scratch

COPY --from=builder /build/nightwatch /nightwatch
