#!/bin/bash

BINARY_PATH="bin/m2cp-tpm/linux-arm64/m2cp-tpm"
env GOOS=linux GOARCH=arm64 CGO_ENABLED=1 CC="aarch64-linux-gnu-gcc" m2cp-go build -o "$BINARY_PATH" cmd/m2cp-tpm/**

echo compiled to $BINARY_PATH
