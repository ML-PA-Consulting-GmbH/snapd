#!/bin/bash

if [ "$#" -lt 2 ] || [ "$#" -gt 3 ]; then
    echo "Usage: $0 <SSH_TARGET> <BINARY_PATH> [TARGET]"
    exit 1
fi

eval "$(ssh-agent -s)" > /dev/null

IP="$1"
BINARY_PATH="$2"
TARGET="$3"
BINARY_NAME=$(basename "$BINARY_PATH")
LOCK_FILE="$BINARY_NAME.lock"

# Require SSH target in the form user@host
if [[ "$IP" != *@* ]]; then
    echo "Error: please pass SSH target as user@host (e.g. root@192.168.1.10)"
    exit 1
fi

SSH_USER="${IP%%@*}"
echo "SSH user: $SSH_USER"
if [[ "$SSH_USER" == "root" ]]; then
    HOME_DIR="/root"
else
    HOME_DIR="/home/$SSH_USER"
fi
export HOME_DIR
echo "HOME_DIR: $HOME_DIR"

CACHE_DIR="${XDG_CACHE_HOME:-$HOME/.cache}/copy-file-to-remote"
mkdir -p "$CACHE_DIR"
HOST_KEY="$IP"
CACHE_FILE="$CACHE_DIR/${HOST_KEY//[^A-Za-z0-9._@-]/_}.last"
FILE_HASH=$(sha256sum "$BINARY_PATH" | awk '{print $1}')
if [ -f "$CACHE_FILE" ]; then
    NOW=$(date +%s)
    MTIME=$(stat -c %Y "$CACHE_FILE")
    AGE=$((NOW - MTIME))
    if [ "$AGE" -le 3600 ]; then
        PREV_HASH=$(cat "$CACHE_FILE")
        if [ "$PREV_HASH" = "$FILE_HASH" ]; then
            echo "Same file hash seen within the last hour for $IP; skipping copy."
            exit 0
        fi
    fi
fi

ssh "$IP" "rm -f $HOME_DIR/$LOCK_FILE"
ssh "$IP" "touch $HOME_DIR/$LOCK_FILE"

DEST="$HOME_DIR"
if [ -n "$TARGET" ]; then
    echo "TARGET: $TARGET"
    DEST="$HOME_DIR/$TARGET"
    PARENT_DIR=$(dirname "$DEST")
    ssh "$IP" "mkdir -p '$PARENT_DIR'"
fi
echo "DEST: $DEST"
echo "Copying $BINARY_PATH to $DEST on $IP..."
scp "$BINARY_PATH" "$IP:$DEST"

if [ $? -eq 0 ]; then
    echo "File copied successfully."
    printf '%s\n' "$FILE_HASH" > "$CACHE_FILE"
else
    echo "Failed to copy file."
    exit 1
fi

echo "Wait until lock file is removed..."
while true; do
    if ! ssh "$IP" "[ -f $HOME_DIR/$LOCK_FILE ]"; then
        break
    fi
    sleep 1
done