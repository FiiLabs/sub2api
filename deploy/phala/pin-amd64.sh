#!/usr/bin/env bash
#
# 从一个 OCI index 里解出**可运行的** linux/amd64 child digest，并可校验
# docker-compose.yml 里钉的就是它。
#
# # 这个脚本为什么存在
#
# 2026-09-15 的 proof-v38 上线把 compose 钉到了 sha256:889892c7——那是这个 index 的
# **attestation-manifest**（platform 为 unknown/unknown 的 SLSA provenance），不是镜像。
# CVM 拉不到可运行的镜像，启动失败，apex1.us 与 api.apex1.us 中断约 15 分钟。
#
# 根因不是手误，是**解析方式**：拿 `docker buildx imagetools inspect` 的人类可读输出
# 去 grep "linux/amd64" 后面第几行的 Name:，窗口错位就取到了下一条记录。那种输出的
# 行距会随 annotations 的多少变化，所以它在下一个 index 上照样会错，而且错得一样安静。
#
# 唯一可靠的做法是 `--raw` 取 JSON、按 platform 字段匹配。这个脚本就是那件事，
# 外加三道拒绝：不是 index、找不到 amd64、或者匹配到的东西带着 attestation 标记，
# 一律非零退出，不打印任何可以被复制粘贴的东西。
#
# 用法：
#   ./pin-amd64.sh <index-ref>            解析并打印 digest 与可直接粘贴的 compose 行
#   ./pin-amd64.sh <index-ref> --verify   额外校验同目录 docker-compose.yml 已经钉对
#
# 例：
#   ./pin-amd64.sh docker.io/markerdao/sub2api:proof-v39 --verify

set -euo pipefail

REF="${1:-}"
MODE="${2:-}"
COMPOSE="$(dirname "$0")/docker-compose.yml"

if [[ -z "$REF" ]]; then
    echo "usage: $0 <index-ref> [--verify]" >&2
    exit 2
fi

RAW="$(docker buildx imagetools inspect --raw "$REF")"

DIGEST="$(printf '%s' "$RAW" | python3 -c '
import json, sys

doc = json.load(sys.stdin)

# 单架构镜像不该出现在这条流水线上：工作流构建的一定是 index。如果这里拿到的是
# 一个 manifest，说明传进来的引用本身就不对（比如已经是 child digest 了），
# 与其"就用它"，不如停下——那正是上一次事故的形态：一个看起来能用的值。
if doc.get("mediaType") not in (
    "application/vnd.oci.image.index.v1+json",
    "application/vnd.docker.distribution.manifest.list.v2+json",
):
    sys.exit("not an image index: mediaType=%s" % doc.get("mediaType"))

hits = []
for m in doc.get("manifests", []):
    plat = m.get("platform") or {}
    if plat.get("os") != "linux" or plat.get("architecture") != "amd64":
        continue
    # attestation-manifest 也可能被打上真实 platform（不同 buildx 版本行为不一），
    # 所以除了看 platform 还要看 annotations，两道都过才算镜像。
    ann = m.get("annotations") or {}
    if ann.get("vnd.docker.reference.type") == "attestation-manifest":
        continue
    if m.get("mediaType") not in (
        "application/vnd.oci.image.manifest.v1+json",
        "application/vnd.docker.distribution.manifest.v2+json",
    ):
        continue
    hits.append(m["digest"])

if not hits:
    sys.exit("no runnable linux/amd64 manifest in this index")
if len(set(hits)) > 1:
    # 不猜。两个都合法说明这个 index 的结构与预期不同，该由人看一眼。
    sys.exit("ambiguous: %d linux/amd64 manifests: %s" % (len(hits), ", ".join(hits)))

print(hits[0])
')"

echo "index : $REF"
echo "amd64 : $DIGEST"
echo
echo "compose 行："
echo "    image: docker.io/markerdao/sub2api@$DIGEST"

if [[ "$MODE" != "--verify" ]]; then
    exit 0
fi

PINNED="$(grep -oE 'image: docker\.io/markerdao/sub2api@sha256:[0-9a-f]{64}' "$COMPOSE" \
    | sed 's/.*@//' || true)"

if [[ -z "$PINNED" ]]; then
    echo "!! 在 $COMPOSE 里找不到 sub2api 的镜像 pin" >&2
    exit 1
fi

echo
if [[ "$PINNED" == "$DIGEST" ]]; then
    echo "✓ compose 已经钉在这个 amd64 child 上"
    exit 0
fi

echo "!! compose 钉的不是这个 index 的 amd64 child" >&2
echo "   compose : $PINNED" >&2
echo "   应该是  : $DIGEST" >&2
exit 1
