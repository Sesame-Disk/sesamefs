#!/bin/sh
set -e

# Inject runtime configuration into index.html
# This allows SESAMEFS_API_URL to be set at container runtime

if [ -n "$SESAMEFS_API_URL" ]; then
    echo "Configuring SESAMEFS_API_URL: $SESAMEFS_API_URL"

    # Inject the API URL into all HTML entry points before the closing </head> tag
    for f in /usr/share/nginx/html/index.html /usr/share/nginx/html/sysadmin.html; do
        if [ -f "$f" ]; then
            sed -i "s|</head>|<script>window.SESAMEFS_API_URL='$SESAMEFS_API_URL';</script></head>|" "$f"
        fi
    done
fi

# Execute the main command
exec "$@"
