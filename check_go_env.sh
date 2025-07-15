#!/bin/zsh
echo "Checking Go environment:"
echo "--------------------"
echo "which go: $(which go)"
echo "go version: $(go version)"
echo "go env GOPATH: $(go env GOPATH)"
echo "go env GOROOT: $(go env GOROOT)"
echo "--------------------"
echo "Verifying VS Code Go settings:"
echo "Checking gopls installation:"
ls -la $(go env GOPATH)/bin/gopls || echo "gopls not found in GOPATH/bin"
echo "Checking if VS Code can find Go:"
/opt/homebrew/bin/go version
