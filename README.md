# Nightwatch

Nightwatch is a command line tool to easily handle events on file system modifications.

<img src="./nightwatch.jpg" width="300">

## Download & Docker

Download `nightwatch` from [releases](https://github.com/jakolehm/nightwatch/releases) page. Linux (amd64, arm64, armhf), MacOS and Windows are supported.


COPY for Dockerfile:
```
COPY --from=jakolehm/nightwatch-amd64:1.3 /nightwatch /usr/bin
```
or
```
COPY --from=jakolehm/nightwatch-arm64:1.3 /nightwatch /usr/bin
```

## Example Usage


#### Using `--find-cmd`:

```
$ nightwatch --find-cmd "find *.js" node app.js
```

#### Using `--files`:

```
$ nightwatch --files "package.json,src/" node app.js
```

#### Via `STDIN`:

```
$ find *.js | nightwatch node app.js
```

## Building From Source

```
$ make build
```


## Testing

```
cd examples/bash
nightwatch --debug simple.sh
```

## Testing with docker-compose

```
cd examples
docker-compose build
docker-compose run example nightwatch --debug bash/simple.sh
```
