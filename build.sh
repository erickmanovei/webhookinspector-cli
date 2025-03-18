#!/bin/bash
platforms=("windows/amd64" "windows/386" "linux/amd64" "linux/386" "linux/arm" "linux/arm64" "darwin/amd64" "darwin/arm64")

for platform in "${platforms[@]}"; do
    OS=$(echo $platform | cut -d'/' -f1)
    ARCH=$(echo $platform | cut -d'/' -f2)
    output_name="webhookinspector-${OS}-${ARCH}"
    
    if [ "$OS" == "windows" ]; then
        output_name+=".exe"
    fi

    echo "Building for $OS $ARCH..."
    env GOOS=$OS GOARCH=$ARCH go build -o ./releases/$output_name main.go
done

echo "Finished building!"
