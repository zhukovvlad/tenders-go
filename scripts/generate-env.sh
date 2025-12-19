#!/bin/bash
# Генерация безопасного ключа для GO_SERVER_API_KEY

set -e

if ! command -v openssl &> /dev/null; then
    echo "Error: openssl is not installed" >&2
    exit 1
fi

KEY=$(openssl rand -base64 32)
echo "GO_SERVER_API_KEY=${KEY}"
