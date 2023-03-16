#!/bin/bash

set -euo pipefail

# We have to define realpath before these otherwise it fails on Mac's bash.
SCRIPT_DIR="$(realpath "$(dirname "${BASH_SOURCE[1]}")")"
# cdroot changes directory to the root of the repository.
PROJECT_ROOT="$(cd "$SCRIPT_DIR" && realpath "$(git rev-parse --show-toplevel)")"

cdroot() {
	cd "$PROJECT_ROOT" || error "Could not change directory to '$PROJECT_ROOT'"
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
