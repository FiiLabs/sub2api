# Rust-capable git-launcher for the private-ai-gateway in-TEE build.
#
# The upstream launcher (dstacktee/git-launcher) is Ubuntu 24.04 with apt but
# WITHOUT a C toolchain. The gateway's entrypoint.sh bootstraps rustc/cargo via
# rustup but never installs a C compiler, so `cargo build` fails with
# `error: linker cc not found`. We add build-essential (gcc/ld/make) plus the
# common Rust FFI deps here so the launcher can actually link the gateway.
#
# The gateway SOURCE stays pinned+attested via the compose `gateway-pin`
# COMMIT_SHA; only the builder image changes, and it is pinned by digest so it
# is measured into compose_hash. See deploy/README.md "Toolchain Posture" —
# this is the "Rust-capable aggregator image" that posture asks for.
FROM docker.io/dstacktee/git-launcher@sha256:4437dce18ec713b0991d34bd926d324966b1a0b90fad485b8ddb3f4ed2af138b

RUN apt-get update \
 && apt-get install -y --no-install-recommends \
      build-essential \
      pkg-config \
      libssl-dev \
      ca-certificates \
 && rm -rf /var/lib/apt/lists/*
