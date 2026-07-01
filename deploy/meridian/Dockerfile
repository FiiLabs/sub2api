FROM node:22-slim

# gost = optional SOCKS5 -> HTTP shim. ProxyLite is a SOCKS5 proxy, while
# Meridian / the Claude Code SDK honor only HTTP proxy env (HTTPS_PROXY).
# Provide the binary in the build context via ./fetch-gost.sh before building
# (it is gitignored, not committed). Only invoked at runtime when
# PROXYLITE_SOCKS5 is set (see entrypoint.sh).
COPY gost /usr/local/bin/gost
RUN chmod +x /usr/local/bin/gost

# Meridian bridges a Claude Max/Pro subscription into an Anthropic-compatible
# endpoint. It bundles its own @anthropic-ai/claude-code runtime.
RUN npm install -g @rynfar/meridian

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
