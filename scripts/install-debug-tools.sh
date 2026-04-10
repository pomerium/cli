#!/bin/sh
set -eu

packages_file="${1:?packages file is required}"

apt-get update
xargs -r -a "$packages_file" apt-get install -y --no-install-recommends
apt-get clean
rm -rf /var/lib/apt/lists/* "$packages_file"
