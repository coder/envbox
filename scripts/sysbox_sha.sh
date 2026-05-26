#!/usr/bin/env bash
set -euo pipefail

case "${1:-linux/$(go env GOARCH)}" in
	linux/amd64)
		printf '%s\n' 'eeff273671467b8fa351ab3d40709759462dc03d9f7b50a1b207b37982ce40a9'
		;;
	linux/arm64)
		printf '%s\n' 'eae9c0e91ddd39bd1826d6a7a313a73d42a8449ef5113e9d6d118b559cb809ba'
		;;
	*)
		echo "unsupported architecture: ${1:-}" >&2
		exit 1
		;;
esac
