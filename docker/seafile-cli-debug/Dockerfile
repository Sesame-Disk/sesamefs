# Seafile CLI Debug container for protocol reverse engineering
# Includes mitmproxy for HTTP traffic capture

FROM debian:bullseye-slim

# Install dependencies including mitmproxy and Python for analysis
RUN apt-get update && apt-get install -y --no-install-recommends \
    wget \
    gnupg \
    curl \
    jq \
    sqlite3 \
    ca-certificates \
    procps \
    lsb-release \
    python3 \
    python3-pip \
    xxd \
    bsdmainutils \
    tcpdump \
    net-tools \
    iproute2 \
    iptables \
    && rm -rf /var/lib/apt/lists/*

# Install mitmproxy for HTTP capture
RUN pip3 install mitmproxy

# Add Seafile repository and install client
RUN wget -q https://linux-clients.seafile.com/seafile.asc -O /usr/share/keyrings/seafile-keyring.asc \
    && echo "deb [arch=amd64 signed-by=/usr/share/keyrings/seafile-keyring.asc] https://linux-clients.seafile.com/seafile-deb/bullseye/ stable main" > /etc/apt/sources.list.d/seafile.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends seafile-cli \
    && rm -rf /var/lib/apt/lists/*

# Create seafile user and directories
RUN useradd -m -u 1000 seafuser \
    && mkdir -p /seafile-data /seafile-client /home/seafuser/.ccnet /captures /mitmproxy \
    && chown -R seafuser:seafuser /seafile-data /seafile-client /home/seafuser /captures /mitmproxy

# Copy capture scripts
COPY --chown=seafuser:seafuser scripts/ /usr/local/bin/

# Make scripts executable
RUN chmod +x /usr/local/bin/*.sh 2>/dev/null || true \
    && chmod +x /usr/local/bin/*.py 2>/dev/null || true

WORKDIR /seafile-client
USER seafuser

# Environment
ENV CCNET_CONF_DIR=/home/seafuser/.ccnet
ENV SEAFILE_DATA_DIR=/seafile-data
ENV CAPTURE_DIR=/captures

# mitmproxy will use this CA
ENV SSL_CERT_FILE=/mitmproxy/mitmproxy-ca-cert.pem
ENV REQUESTS_CA_BUNDLE=/mitmproxy/mitmproxy-ca-cert.pem

# Keep container running
CMD ["tail", "-f", "/dev/null"]
