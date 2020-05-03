FROM golang:1.13.10-alpine3.11

COPY --from=nightwatch /nightwatch /usr/bin

WORKDIR /app
COPY . .

ENTRYPOINT [ "/usr/bin/nightwatch", "go", "run", "main.go" ]
