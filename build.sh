if [ "$1" = "-p" ] || [ "$1" = "--production" ]; then
  go build -ldflags "-s -w" -o "../clustta/src-tauri/clustta_cli-x86_64-apple-darwin" ./cmd/cli
else
  go build -o "../clustta/src-tauri/clustta_cli-x86_64-apple-darwin" ./cmd/cli
fi