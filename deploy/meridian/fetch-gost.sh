#!/bin/sh
# Fetch the gost binary into this build context (gitignored, not committed).
# Run once before `docker build` / `docker compose build`.
#   ./fetch-gost.sh
set -e
VER="${GOST_VERSION:-3.0.0}"
DIR="$(cd "$(dirname "$0")" && pwd)"
url="https://github.com/go-gost/gost/releases/download/v${VER}/gost_${VER}_linux_amd64.tar.gz"
echo "Fetching gost ${VER} ..."
curl -fsSL "$url" -o /tmp/gost.tgz
tar -xzf /tmp/gost.tgz -C "$DIR" gost
chmod +x "$DIR/gost"
rm -f /tmp/gost.tgz
echo "gost ${VER} ready at $DIR/gost"
