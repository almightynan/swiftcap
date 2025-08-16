#!/bin/bash
set -e
CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o swiftcap ./cmd/swiftcap
