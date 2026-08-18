# Reproduces the toolchain netra's CGO+DuckDB binary must be built with:
# Debian Buster (glibc 2.28) so the resulting binary runs on the older
# glibc most production Linux boxes still ship, with DuckDB compiled from
# source using Buster's own GCC 8.3 -- go-duckdb's prebuilt static lib is
# built on Ubuntu 20.04/GCC 9 and links against symbols newer than glibc
# 2.28 provides, which is what makes a plain `go build` on a modern
# distro (including GitHub Actions' own ubuntu-latest runner) produce a
# binary that fails to start on those older targets. See project docs for
# the full incident writeup; this Dockerfile exists so that failure mode
# can't recur silently.
FROM debian:buster

# Buster is EOL: its default sources.list points at deb.debian.org, which
# no longer serves it. Point at the official long-term archive instead.
RUN sed -i \
    -e 's|deb.debian.org/debian|archive.debian.org/debian|g' \
    -e 's|security.debian.org|archive.debian.org/debian-security|g' \
    -e '/buster-updates/d' \
    /etc/apt/sources.list \
    && echo 'Acquire::Check-Valid-Until "false";' > /etc/apt/apt.conf.d/99no-check-valid

RUN apt-get update && apt-get install -y --no-install-recommends \
    build-essential git curl ca-certificates \
    && rm -rf /var/lib/apt/lists/*

# curl's own --retry only fires for a fixed set of error codes it
# considers "transient", which does NOT include every real-world failure
# mode seen from this kind of network path (e.g. curl 35
# SSL_ERROR_SYSCALL) -- Buster's ancient curl (7.64) also predates
# --retry-all-errors, which would otherwise cover this. The `until curl
# ... || retry` loop below retries regardless of *why* curl failed.
# Downloads to a file first and only extracts after a successful
# download, instead of piping curl straight into tar, so a truncated
# transfer fails loudly at the curl step instead of feeding tar a corrupt
# stream ("gzip: unexpected end of file"). Written as a single portable
# `\`-continued RUN (no heredoc) so it behaves the same under either the
# legacy docker builder or BuildKit.

# Buster's own cmake (3.13) is too old for DuckDB's build -- a modern
# prebuilt binary from Kitware's own releases avoids depending on any
# Python/pip toolchain just to get a newer cmake.
ARG CMAKE_VERSION=3.27.9
RUN n=0; until curl -fsSL "https://github.com/Kitware/CMake/releases/download/v${CMAKE_VERSION}/cmake-${CMAKE_VERSION}-linux-x86_64.tar.gz" -o /tmp/cmake.tar.gz; do \
      n=$((n+1)); [ $n -ge 5 ] && echo "giving up after 5 attempts" && exit 1; \
      echo "cmake download attempt $n failed, retrying in 10s..."; sleep 10; \
    done \
    && tar -xzf /tmp/cmake.tar.gz -C /opt && rm /tmp/cmake.tar.gz \
    && ln -s /opt/cmake-${CMAKE_VERSION}-linux-x86_64/bin/cmake /usr/local/bin/cmake \
    && ln -s /opt/cmake-${CMAKE_VERSION}-linux-x86_64/bin/ctest /usr/local/bin/ctest

# Go toolchain, pinned to what go.mod declares.
ARG GO_VERSION=1.25.0
RUN n=0; until curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz; do \
      n=$((n+1)); [ $n -ge 5 ] && echo "giving up after 5 attempts" && exit 1; \
      echo "go download attempt $n failed, retrying in 10s..."; sleep 10; \
    done \
    && tar -xzf /tmp/go.tar.gz -C /usr/local && rm /tmp/go.tar.gz
ENV PATH="/usr/local/go/bin:${PATH}"

# Build DuckDB from source with Buster's own GCC 8.3, so the resulting
# static lib is ABI-matched to Buster's libstdc++ by construction -- no
# mismatch is possible since it's compiled by the very toolchain that
# will later link netra itself. Version must track whatever go-duckdb
# v1.8.5 (see go.mod) expects; bump both together if go-duckdb is upgraded.
ARG DUCKDB_VERSION=v1.1.3
RUN mkdir -p /build && cd /build && n=0 \
    && until curl -fsSL "https://codeload.github.com/duckdb/duckdb/tar.gz/refs/tags/${DUCKDB_VERSION}" -o duckdb.tar.gz; do \
      n=$((n+1)); [ $n -ge 5 ] && echo "giving up after 5 attempts" && exit 1; \
      echo "duckdb download attempt $n failed, retrying in 10s..."; sleep 10; \
    done \
    && tar -xzf duckdb.tar.gz && rm duckdb.tar.gz \
    && mv duckdb-* duckdb

WORKDIR /build/duckdb
RUN BUILD_SHELL=0 BUILD_UNITTESTS=0 DUCKDB_PLATFORM=any \
    ENABLE_EXTENSION_AUTOLOADING=1 ENABLE_EXTENSION_AUTOINSTALL=1 \
    BUILD_EXTENSIONS=json \
    CFLAGS=-O3 CXXFLAGS=-O3 \
    make bundle-library -j4

# Drop the compiled static lib where the "swap into go-duckdb's module
# cache" step (in the workflow, after `go mod download`) can find it --
# building it into the image itself means this expensive step only reruns
# when this Dockerfile's own layers change, not on every workflow run.
RUN mkdir -p /opt/duckdb-lib \
    && cp /build/duckdb/build/release/libduckdb_bundle.a /opt/duckdb-lib/libduckdb.a

WORKDIR /src
