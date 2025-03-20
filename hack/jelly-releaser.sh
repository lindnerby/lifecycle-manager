#!/bin/bash

if ! command -v gh &> /dev/null; then
    echo "GitHub CLI (gh) is NOT installed."
    exit 1
else
    echo "GitHub CLI (gh) is installed."
    gh --version
fi
