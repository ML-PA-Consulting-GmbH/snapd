#!/bin/bash

if [ "$#" -ne 2 ]; then
    echo "Usage: $0 <SSH_TARGET> <BINARY_PATH>"
    exit 1
fi

eval "$(ssh-agent -s)" > /dev/null

IP="$1"
BINARY_PATH="$2"

# Require SSH target in the form user@host
if [[ "$IP" != *@* ]]; then
    echo "Error: please pass SSH target as user@host (e.g. root@192.168.1.10)"
    exit 1
fi

SSH_USER="${IP%%@*}"
# Best-effort HOME_DIR for local env; not used for scp path
HOME_DIR=$(ssh "$IP" 'eval echo ~')
export HOME_DIR

echo "Copying $BINARY_PATH to ~/ on $IP..."
scp "$BINARY_PATH" "$IP:~/"

if [ $? -eq 0 ]; then
    echo "File copied successfully."
else
    echo "Failed to copy file."
    exit 1
fi