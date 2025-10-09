#!/bin/bash

if [ "$#" -ne 1 ]; then
    echo "Usage: $0 <NEW_IP>"
    exit 1
fi

NEW_IP="$1"
RUN_DIR=".run"
TARGET_FILE="$RUN_DIR/Copy+Debug snapd on arm64 target.run.xml"

CURRENT_IP=$(grep -oP '(?<=host=")[^"]*' "$TARGET_FILE")

if [ -z "$CURRENT_IP" ]; then
    echo "No IP found in $TARGET_FILE"
    exit 1
fi

echo "Current IP: $CURRENT_IP"

for FILE in "$RUN_DIR"/*.xml; do
    if [ -f "$FILE" ]; then
        sed -i "s/$CURRENT_IP/$NEW_IP/g" "$FILE"
        echo "Replaced IP in $FILE"
    fi
done

echo "IP replacement complete."
echo -e "\e[1;31mPlease restart GoLand to apply the changes.\e[0m"
