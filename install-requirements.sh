#!/bin/bash

# List of packages to check
packages=("libc6-dev" "libc6-dev-i386" "gcc-multilib")

# Keep track of missing packages
missing_packages=()

# Check each package
for package in "${packages[@]}"; do
    dpkg -l $package &> /dev/null
    if [ $? -eq 0 ]; then
        echo "$package is already installed."
    else
        echo "$package is not installed."
        missing_packages+=($package)
    fi
done

# If there are missing packages, update and install
if [ ${#missing_packages[@]} -ne 0 ]; then
    echo "Updating package list and installing missing packages..."
    sudo apt-get update
    sudo apt-get install -y "${missing_packages[@]}"
fi
