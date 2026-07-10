# --- Meridian from the UPSTREAM npm release (back on the main line), plus the
# official hermes-scrub plugin baked in. Background: the openai-adapter Hermes
# "Extra Usage" problem the FiiLabs fork patched in src/ was NEVER merged into
# Meridian core — the maintainer keeps content-mutating transforms out of src/
# (rynfar/meridian PR #545, closed not merged) and ships the fix as an external
# opt-in plugin instead. So we install stock Meridian and bake in
# @rynfar/meridian-plugin-hermes-scrub, built from a pinned commit so this
# enclave image's provenance still binds to exact plugin source (the plugin is
# not yet on npm; pin the commit the same way the fork build did).

# Build the hermes-scrub plugin from a pinned commit (immutable source binding).
# dist/ is self-contained: only relative imports, NO @rynfar/meridian runtime dep
# (the plugin re-exports meridian-compatible types locally), so it loads standalone.
FROM node:22-slim AS pluginbuild
ARG PLUGIN_REPO=https://github.com/rynfar/meridian-plugin-hermes-scrub
ARG PLUGIN_COMMIT=0068dafa1ecc4365ab973854210cb3d3753685ba
RUN apt-get update \
 && apt-get install -y --no-install-recommends git ca-certificates \
 && rm -rf /var/lib/apt/lists/*
RUN git clone "$PLUGIN_REPO" /src \
 && cd /src \
 && git checkout "$PLUGIN_COMMIT" \
 && npm ci \
 && npm run build
# /src/dist now holds index.js (+ scrub.js, types.js), built with tsc.

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
# the UPSTREAM npm release, pinned by version (digest-pinned image below binds
# the exact bytes; the version here documents the source line).
RUN npm install -g @rynfar/meridian@1.45.1

# Bake in the hermes-scrub plugin (built above) OUTSIDE meridian's auto-discovery
# dir (~/.config/meridian/plugins), and enable it explicitly via plugins.json
# with an ABSOLUTE path so the loader imports only index.js (its ./scrub.js
# resolves alongside it) — a flat drop-in would also try to load scrub.js/types.js
# as plugins. This strips Hermes Agent's "# Finishing the job" harness fingerprint
# so those requests bill under Max instead of failing 400 "out of extra usage";
# it is a content-scoped no-op for non-Hermes prompts (Claude Code / OpenCode / pi).
COPY --from=pluginbuild /src/dist /opt/meridian-plugins/hermes-scrub
RUN mkdir -p /root/.config/meridian \
 && printf '%s\n' '{"plugins":[{"path":"/opt/meridian-plugins/hermes-scrub/index.js","enabled":true}]}' \
      > /root/.config/meridian/plugins.json

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
