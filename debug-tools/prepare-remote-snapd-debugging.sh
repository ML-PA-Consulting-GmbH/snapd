#!/bin/bash

if [ -z "$1" ]; then
    echo "Please provide the IP address of the remote device"
    exit 1
fi

if [ ! -f build-debug/snapd_arm64 ]; then
    echo "snapd_arm64 not found in build-debug. Please build snapd first"
    exit 1
fi

REMOTE_USER="root"
REMOTE_IP="$1"

# Kill all running delve instances on the remote device
ssh $REMOTE_USER@$REMOTE_IP "pkill delve-arm64" || true

# Copy the snapd binary to the remote device
scp build-debug/snapd_arm64 $REMOTE_USER@$REMOTE_IP:/root/snapd_arm64

# Copy delve to the remote device, if its not already present on remote
present=$(ssh $REMOTE_USER@$REMOTE_IP "ls /root/delve-arm64" 2>/dev/null)
if [ -n "$present" ]; then
    echo "delve-arm64 already present on remote device, skipping copy"
else
  scp debug-tools/delve-arm64 $REMOTE_USER@$REMOTE_IP:/root/delve-arm64
fi

# Execute delve on the remote device to allow connecting the debugger. Open it in the background
ssh $REMOTE_USER@$REMOTE_IP "cd /root && ./delve-arm64 --headless=true --listen=:2345 --api-version=2 exec /root/snapd_arm64" &


