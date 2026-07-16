# --- Meridian from OUR fork (FiiLabs/meridian).
#
# Why a fork (not the npm release): the multi-turn "flatten" leak. On any fresh
# (non-resume) request with multi-turn history, stock Meridian flattens the
# conversation into a single `Human: …\n\nAssistant: …` transcript and hands it
# to the single-turn Claude Code SDK, which then CONTINUES the transcript on its
# own — fabricating fake turns, replying on the user's behalf, and leaking the
# raw `[Assistant: …]` prompt labels into the reply. The fork rebuilds the fresh
# (non-resume) history as a STREAM of discrete structured user messages, and
# collapses prior turns into ONE inert, read-only "record" block followed by the
# genuine latest user turn — so the model reads history as reference, not as a
# transcript to continue. A response-side scrub also strips any leaked leading
# scaffolding (<system-reminder>/[Assistant:]/role labels) as a safety net.
# Based on upstream meridian-v1.49.1 (gains upstream resume-robustness so the
# fresh path is rarely taken). See FiiLabs/meridian branch fix-on-1491
# (8a21b8b: reference-block fresh-path + output scrub, rebased onto 1.49.1).

# Build Meridian itself from our fork. dist/ is NOT committed to the fork, so we
# build it here with the repo's own toolchain (bun build + tsc via `bun run
# build`; the postbuild step uses node, which node:22-slim provides), then
# `npm pack` a tarball identical in layout to the npm release for the final stage
# to install globally. Pinning MERIDIAN_COMMIT + the digest-pinned image below
# binds the exact bytes; the commit is the source provenance.
FROM node:22-slim AS meridianbuild
ARG MERIDIAN_REPO=https://github.com/FiiLabs/meridian
ARG MERIDIAN_COMMIT=8a21b8bb49b1bd2b1957db5b547254c6c17e1427
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*
RUN npm install -g bun@1.3.14
RUN git clone "$MERIDIAN_REPO" /src \
 && cd /src \
 && git checkout "$MERIDIAN_COMMIT" \
 && bun install --frozen-lockfile \
 && bun run build \
 && mkdir /out \
 && npm pack --pack-destination /out
# /out/rynfar-meridian-*.tgz now holds the built package (dist/ + plugin/ + assets/).

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
# our fork tarball built above (same global layout as the npm release; the
# digest-pinned image below binds the exact bytes, and the fork commit is the
# source provenance).
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
