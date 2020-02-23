FROM scratch

ARG binary

ADD ./output/$binary /nightwatch
