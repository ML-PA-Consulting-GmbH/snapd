#!/bin/bash

if [ "$#" -ne 3 ]; then
    echo "Usage: $0 <IP> <BINARY_PATH> <TARGET>"
    exit 1
fi

eval "$(ssh-agent -s)" > /dev/null

IP="$1"
BINARY_PATH="$2"
TARGET="$3"

echo "Copying $BINARY_PATH to $TARGET on $IP..."
scp "$BINARY_PATH" "$IP:$TARGET"

if [ $? -eq 0 ]; then
    echo "File copied successfully."
else
    echo "Failed to copy file."
    exit 1
fi