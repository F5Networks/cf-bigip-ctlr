#!/bin/bash


set -e

CURDIR="$(dirname $BASH_SOURCE)"
BUILD_VARIANT=debug

# Turn on race detection
BUILD_VARIANT_FLAGS="-race"

. $CURDIR/_build-lib.sh

go_install $(all_pkgs)

for pkg in $(all_pkgs); do
  test_pkg_profile "$pkg"
done
