#!/bin/sh
# Inject SESAMEFS_API_URL into index.html at runtime
if [ -n "$SESAMEFS_API_URL" ]; then
  for f in /usr/share/nginx/html/index.html; do
    if [ -f "$f" ]; then
      sed -i "s|</head>|<script>window.SESAMEFS_API_URL='$SESAMEFS_API_URL';</script></head>|" "$f"
    fi
  done
fi
# Inject bypass login flag for testing
if [ "$BYPASS_LOGIN" = "true" ]; then
  for f in /usr/share/nginx/html/index.html; do
    if [ -f "$f" ]; then
      sed -i "s|</head>|<script>window.BYPASS_LOGIN=true;</script></head>|" "$f"
    fi
  done
fi
