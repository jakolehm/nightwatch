# Nightwatch

Nightwatch is a command line tool to easily handle events on file system modifications.

<img src="./nightwatch.jpg" width="300">

## Download & Docker

Download `nightwatch` from [releases](https://github.com/jakolehm/nightwatch/releases) page. Linux (amd64, arm64, armhf), MacOS and Windows are supported.


COPY for Dockerfile:
```
COPY --from=jakolehm/nightwatch-amd64:0.4 /nightwatch /usr/bin
```
or
```
COPY --from=jakolehm/nightwatch-arm64:0.4 /nightwatch /usr/bin
```

## Example Usage

```
$ find *.js | nightwatch node app.js
```

## Building From Source

```
$ make build
```
