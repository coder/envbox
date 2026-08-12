#!/usr/bin/env bash
set -euo pipefail

case "${1:-linux/$(go env GOARCH)}" in
	linux/amd64)
		printf '%s\n' '9d6d5484f980d0a17f86c492c1262015c2afb66280bdb97215b79fde6a0261c5'
		;;
	linux/arm64)
		printf '%s\n' '04ca894ae0b53f0fa54eaacc173ce40363c9a95ea5450f773716a84ef650a69b'
		;;
	*)
		echo "unsupported architecture: ${1:-}" >&2
		exit 1
		;;
esac
