# Seafile CLI container for testing sync protocol
# Uses Debian bullseye for official seafile-cli package support

FROM debian:bullseye-slim

# Install dependencies
RUN apt-get update && apt-get install -y --no-install-recommends \
    wget \
    gnupg \
    curl \
    jq \
    sqlite3 \
    ca-certificates \
    procps \
    lsb-release \
    && rm -rf /var/lib/apt/lists/*

# Add Seafile repository and install client
RUN wget -q https://linux-clients.seafile.com/seafile.asc -O /usr/share/keyrings/seafile-keyring.asc \
    && echo "deb [arch=amd64 signed-by=/usr/share/keyrings/seafile-keyring.asc] https://linux-clients.seafile.com/seafile-deb/bullseye/ stable main" > /etc/apt/sources.list.d/seafile.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends seafile-cli \
    && rm -rf /var/lib/apt/lists/*

# Create seafile user and directories
RUN useradd -m -u 1000 seafuser \
    && mkdir -p /seafile-data /seafile-client /home/seafuser/.ccnet \
    && chown -R seafuser:seafuser /seafile-data /seafile-client /home/seafuser

# Copy test scripts
COPY --chown=seafuser:seafuser scripts/ /usr/local/bin/

# Make scripts executable
RUN chmod +x /usr/local/bin/*.sh 2>/dev/null || true

WORKDIR /seafile-client
USER seafuser

# Initialize seafile config on first run
ENV CCNET_CONF_DIR=/home/seafuser/.ccnet
ENV SEAFILE_DATA_DIR=/seafile-data

# Keep container running for interactive testing
CMD ["tail", "-f", "/dev/null"]
