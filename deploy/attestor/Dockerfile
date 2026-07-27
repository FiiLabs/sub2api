# sub2api attestation sidecar — a ~90-line Node HTTP server that serves a
# nonce-bound Intel TDX DCAP quote + dstack event log over CORS. It is a
# SEPARATE image from sub2api for isolation and a clean measurement: it holds
# NO secrets and only talks to the dstack guest-agent socket.
#
# Built + cosign-signed by .github/workflows/publish-tee-images.yml; pin the
# resulting linux/amd64 digest into deploy/phala/docker-compose.yml.
FROM node:22-slim
WORKDIR /app

# Deps are pinned (exact versions in package.json, locked in package-lock.json);
# `npm ci` installs exactly the lockfile so the image contents are reproducible
# and measured into the CVM's compose_hash via this image's digest.
COPY package.json package-lock.json ./
RUN npm ci --omit=dev --no-audit --no-fund

COPY server.mjs ./

ENV ATTESTOR_HOST=0.0.0.0 \
    ATTESTOR_PORT=8091 \
    DSTACK_SOCKET=/var/run/dstack.sock

EXPOSE 8091
ENTRYPOINT ["node", "server.mjs"]
