#!/bin/sh
# Cross-compiles every platform's binary into npm/bin for publishing.
set -eu
rm -rf npm/bin
mkdir -p npm/bin
for target in darwin/arm64/darwin/arm64 darwin/amd64/darwin/x64 linux/arm64/linux/arm64 linux/amd64/linux/x64 windows/amd64/win32/x64; do
  IFS=/ read -r goos goarch os cpu <<TARGET
$target
TARGET
  exe=""
  [ "$goos" = windows ] && exe=.exe
  CGO_ENABLED=0 GOOS=$goos GOARCH=$goarch go build -trimpath -ldflags='-s -w -buildid=' -o "npm/bin/slowplan-$os-$cpu$exe" .
done
chmod +x npm/bin/*
