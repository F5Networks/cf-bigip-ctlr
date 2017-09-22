#!/bin/bash

# run python style and unit tests
# simplified to run in build-devel-image

set -ex

(cd python && flake8 . --exclude src,lib,go,bin,docs,cmd)
(cd python && pytest . -slvv --ignore=src/ -p no:cacheprovider)
