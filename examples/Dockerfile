FROM jakolehm/nightwatch-amd64:1.1 as nightwatch

FROM alpine:3.11

RUN apk add --no-cache \
  bash

COPY --from=nightwatch /nightwatch /usr/bin
