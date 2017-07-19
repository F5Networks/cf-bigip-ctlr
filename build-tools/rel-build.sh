#!/bin/bash


set -e

CURDIR="$(dirname $BASH_SOURCE)"

. $CURDIR/_build-lib.sh

go_install $(all_pkgs)

ginkgo_test_with_coverage
