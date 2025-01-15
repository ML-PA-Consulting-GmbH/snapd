#!/bin/bash

# This script needs to be run on the target device where the binary is present

if [ -z "$1" ] || [ -z "$2" ]; then
    echo "Usage: $0 <BINARY_PATH> <PORT>"
    exit 1
fi

BINARY_PATH="$1"
PORT="$2"
BINARY_NAME=$(basename "$BINARY_PATH")
RUNNING_BINARY_PATH="${BINARY_PATH}_running"
RUNNING_BINARY_NAME=$(basename "$RUNNING_BINARY_PATH")
DELVE_PATH="/root/delve-arm64"
RED='\033[0;31m'
NC='\033[0m' # No Color

start_delve() {
    echo "Starting Delve..."
    PARAMS_FILE="${BINARY_NAME}_params"
    PARAMS=""
    if [ -f "$PARAMS_FILE" ]; then
        PARAMS=$(cat "$PARAMS_FILE")
    fi
    $DELVE_PATH --headless=true --listen=:"$PORT" --api-version=2 exec "$RUNNING_BINARY_PATH" "$PARAMS" &
    DELVE_PID=$!
    echo "Delve started with PID $DELVE_PID"
}

wait_until_port_in_use() {
    echo "Wait until port is in use..."
    while true; do
        if ss -tln | grep -q ":$PORT"; then
            break
        fi
        sleep 1
    done
}

if [ ! -f $DELVE_PATH ]; then
    echo "Delve not found at $DELVE_PATH. Please ensure it is present."
    exit 1
fi

kill_delve_instances() {
    echo "Killing all Delve instances..."
    pkill -f "delve-arm64.*$RUNNING_BINARY_NAME"
    echo "Wait until port is not in use anymore..."
    while true; do
        if ! ss -tln | grep -q ":$PORT"; then
            break
        fi
        sleep 1
    done
}

kill_delve_instances
start_delve
trap kill_delve_instances EXIT

# Monitor if new binary appears or Delve terminated
while true; do
    # Check if new binary is present, but we need to wait until it is fully copied. Compare via size change, as
    # tools for it are not necessarily available on the target device.
    if [ -f "$BINARY_PATH" ]; then
        NEW_BINARY_SIZE=$(stat -c%s "$BINARY_PATH")
        sleep 1
        CURRENT_BINARY_SIZE=$(stat -c%s "$BINARY_PATH")
        if [ "$NEW_BINARY_SIZE" -eq "$CURRENT_BINARY_SIZE" ]; then
            echo -e "${RED}New binary detected, replacing and restarting Delve...${NC}"
            kill_delve_instances
            rm -f "$RUNNING_BINARY_PATH"
            mv "$BINARY_PATH" "$RUNNING_BINARY_PATH"
            start_delve
            wait_until_port_in_use
            # Remove lock file, if present
            rm -f "$BINARY_PATH.lock"
        fi
    fi
    if ! ps -p $DELVE_PID > /dev/null; then
        echo -e "${RED}Delve terminated, restarting...${NC}"
        start_delve
    fi

    sleep 1
done