#!/bin/bash

if [ "$#" -ne 3 ]; then
    echo "Usage: $0 <IP> <BINARY_PATH> <TARGET>"
    exit 1
fi

eval "$(ssh-agent -s)" > /dev/null

IP="$1"
BINARY_PATH="$2"
TARGET="$3"
BINARY_NAME=$(basename "$BINARY_PATH")
LOCK_FILE="$BINARY_NAME.lock"

ssh "$IP" "rm -f $LOCK_FILE"
ssh "$IP" "touch $LOCK_FILE"

echo "Copying $BINARY_PATH to $TARGET on $IP..."
scp "$BINARY_PATH" "$IP:$TARGET"

if [ $? -eq 0 ]; then
    echo "File copied successfully."
else
    echo "Failed to copy file."
    exit 1
fi

echo "Wait until lock file is removed..."
while true; do
    if ! ssh "$IP" "[ -f $LOCK_FILE ]"; then
        break
    fi
    sleep 1
done