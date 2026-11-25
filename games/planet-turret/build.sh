#!/usr/bin/env bash
# 교사 배포용 Windows exe 빌드(순수 Go, CGO 불필요 — 리눅스·윈도우 어디서든).
#
#   ./build.sh                          # dist/planet-turret.exe (콘솔 창 = 실행 중 표시, 닫으면 종료)
#   ./build.sh dist/planet-turret.exe gui   # 콘솔 창 없이
set -euo pipefail
cd "$(dirname "$0")"
OUT="${1:-dist/planet-turret.exe}"
MODE="${2:-console}"
mkdir -p "$(dirname "$OUT")"
LDFLAGS="-s -w"
if [ "$MODE" = "gui" ]; then LDFLAGS="-H windowsgui -s -w"; fi
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags "$LDFLAGS" -o "$OUT" .
echo "built: $OUT ($(du -h "$OUT" | cut -f1))  mode=$MODE"
