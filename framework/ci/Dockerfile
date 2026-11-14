# The image the harness runs the desktop framework in, in the pipelines:
# harness test, and harness build for a release. It is the runner GitHub
# gives the jobs, Ubuntu 24.04, with what the framework needs: Go, Node.js,
# git, and the system libraries of the browsers the e2e tests and the apps
# drive. The harness tags it with this folder's git tree, so a change here
# is a new image, and anything else reuses the one already published.
#
# The harness runs it as the runner's user, which it does not know: HOME,
# and everything under it, is writable by anyone. The VOLUMEs are the
# caches the pipelines keep between runs.
FROM golang:1.26-bookworm AS go
FROM node:24-bookworm-slim AS node

FROM ubuntu:24.04
RUN apt-get update \
 && apt-get install -y --no-install-recommends ca-certificates curl git procps unzip xz-utils \
 && rm -rf /var/lib/apt/lists/*
COPY --from=go /usr/local/go /usr/local/go
COPY --from=node /usr/local/bin/node /usr/local/bin/node
COPY --from=node /usr/local/lib/node_modules /usr/local/lib/node_modules
RUN ln -s ../lib/node_modules/npm/bin/npm-cli.js /usr/local/bin/npm \
 && ln -s ../lib/node_modules/npm/bin/npx-cli.js /usr/local/bin/npx

# The WebKit the e2e tests open pages in, and the Chromium the apps run
# services' tools in, need these; the browsers themselves are the
# framework's to download, into its cache.
RUN npx -y playwright@1.62 install-deps webkit chromium \
 && rm -rf /var/lib/apt/lists/* /root/.npm

ENV HOME=/home/ci \
    GOPATH=/home/ci/go \
    GOTOOLCHAIN=local \
    PATH=/home/ci/go/bin:/usr/local/go/bin:$PATH
RUN mkdir -p /home/ci/go/bin /home/ci/go/pkg /home/ci/.cache /home/ci/.npm /home/ci/.harness \
 && chmod -R 1777 /home/ci

# Go's modules and builds, npm's packages, and the framework's own cache
# (its tools, browsers, and dependencies, in ~/.harness/desktop).
VOLUME ["/home/ci/go/pkg/mod", "/home/ci/.cache", "/home/ci/.npm", "/home/ci/.harness"]
