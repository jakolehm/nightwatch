FROM scratch

ARG binary

COPY ./output/$binary /nightwatch
