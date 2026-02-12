# SesameFS Frontend - Standalone React build
# Matches backend's Debian Trixie base

# Build stage
FROM node:22-bookworm AS builder

WORKDIR /app

# Copy package files
COPY package*.json ./

# Install dependencies
RUN npm ci --legacy-peer-deps

# Copy source code
COPY . .

# Copy .env file for build configuration
# If .env doesn't exist, create a default one
COPY .env* ./
RUN if [ ! -f .env ]; then \
    echo "NODE_MAX_MEMORY=4096" > .env && \
    echo "GENERATE_SOURCEMAP=false" >> .env && \
    echo "WEBPACK_PARALLEL_BUILD=true" >> .env && \
    echo "SESAMEFS_API_URL=" >> .env; \
    fi

# Build argument for API URL (can be empty for relative URLs)
ARG SESAMEFS_API_URL=
ENV SESAMEFS_API_URL=${SESAMEFS_API_URL}

# Load build configuration from .env file
# These settings control memory usage and build optimization
# Strip Windows CRLF from .env (file may come from Windows host)
RUN sed -i 's/\r$//' .env
RUN export $(cat .env | grep -v '^#' | xargs) && \
    # Set defaults if not specified in .env \
    NODE_MAX_MEMORY=${NODE_MAX_MEMORY:-4096} && \
    GENERATE_SOURCEMAP=${GENERATE_SOURCEMAP:-false} && \
    WEBPACK_PARALLEL_BUILD=${WEBPACK_PARALLEL_BUILD:-false} && \
    # Export for npm build \
    export GENERATE_SOURCEMAP && \
    export WEBPACK_PARALLEL_BUILD && \
    # Build with configured memory limit \
    NODE_OPTIONS=--max_old_space_size=$NODE_MAX_MEMORY npm run build

# Production stage
FROM nginx:bookworm

# Copy built assets from builder
COPY --from=builder /app/build /usr/share/nginx/html

# Copy nginx configuration
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Copy runtime config script (sed fixes Windows CRLF line endings)
COPY docker-entrypoint.sh /docker-entrypoint.sh
RUN sed -i 's/\r$//' /docker-entrypoint.sh && chmod +x /docker-entrypoint.sh

EXPOSE 80

ENTRYPOINT ["/docker-entrypoint.sh"]
CMD ["nginx", "-g", "daemon off;"]
