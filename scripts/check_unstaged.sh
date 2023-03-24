#!/usr/bin/env bash

set -euo pipefail

# cdroot changes directory to the root of the repository.
PROJECT_ROOT="$(git rev-parse --show-toplevel)"

cdroot() {
	cd "$PROJECT_ROOT" || error "Could not change directory to '$PROJECT_ROOT'"
}

# error prints an error message and returns an error exit code.
error() {
  log "ERROR: $*"
  exit 1
}

# log prints a message to stderr.
log() {
  echo "$*" 1>&2
}

cdroot

FILES="$(git ls-files --other --modified --exclude-standard)"
if [[ "$FILES" != "" ]]; then
	mapfile -t files <<<"$FILES"

	log
	log "The following files contain unstaged changes:"
	log
	for file in "${files[@]}"; do
		log "  - $file"
	done

	log
	log "These are the changes:"
	log
	for file in "${files[@]}"; do
		git --no-pager diff "$file" 1>&2
	done

	log
	error "Unstaged changes, see above for details."
fi
