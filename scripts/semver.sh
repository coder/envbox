#!/usr/bin/env bash

#!/bin/bash
VERSION="${1}"
rx='^([0-9]+\.){2}(\*|[0-9]+)$'
if [[ $VERSION =~ $rx ]]; then
 exit 0
else
 echo "ERROR: Invalid version $VERSION, should be in the form '0.1.2'"
 exit 1
fi
