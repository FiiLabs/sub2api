# --- Build the patched Meridian from the FiiLabs fork instead of the upstream
# npm package. The fork carries the openai-adapter Hermes system-prompt fix
# (rynfar/meridian PR #545). Pinned to an immutable commit so this enclave
# image's SLSA provenance binds to exact Meridian source.
# Upstream install was: RUN npm install -g @rynfar/meridian
FROM node:22-slim AS meridianbuild
ARG MERIDIAN_REPO=https://github.com/FiiLabs/meridian
ARG MERIDIAN_COMMIT=96771c939abac13b5d03bba6949776b9af630ea6
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates unzip \
 && rm -rf /var/lib/apt/lists/* \
 && npm install -g bun
RUN git clone "$MERIDIAN_REPO" /src \
 && cd /src \
 && git checkout "$MERIDIAN_COMMIT" \
 && bun install \
 && bun run build \
 && mkdir -p /out \
 && npm pack --pack-destination /out
# /out now holds rynfar-meridian-<version>.tgz (dist/ built with the patch).

FROM node:22-slim

# gost = optional SOCKS5 -> HTTP shim. ProxyLite is a SOCKS5 proxy, while
# Meridian / the Claude Code SDK honor only HTTP proxy env (HTTPS_PROXY).
# Provide the binary in the build context via ./fetch-gost.sh before building
# (it is gitignored, not committed). Only invoked at runtime when
# PROXYLITE_SOCKS5 is set (see entrypoint.sh).
COPY gost /usr/local/bin/gost
RUN chmod +x /usr/local/bin/gost

# Meridian bridges a Claude Max/Pro subscription into an Anthropic-compatible
# endpoint. It bundles its own @anthropic-ai/claude-code runtime. Installed from
# the patched fork build above (packaged tarball) rather than the upstream npm
# release, so the openai-adapter Hermes fix is included.
COPY --from=meridianbuild /out/*.tgz /tmp/meridian.tgz
RUN npm install -g /tmp/meridian.tgz && rm -f /tmp/meridian.tgz

ENV MERIDIAN_HOST=0.0.0.0 \
    MERIDIAN_PORT=3456 \
    MERIDIAN_IDLE_TIMEOUT_SECONDS=300

# Claude subscription OAuth login state (~/.claude/.credentials.json and
# ~/.claude.json) MUST be provisioned at runtime via volume / secrets — see
# compose.yaml and README.md. It is intentionally NOT baked into the image.
COPY entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh

EXPOSE 3456
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
