ARG CADDY_VERSION=v2.11.2
ARG GO_VERSION=1.25.12

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8 AS builder

ARG CADDY_VERSION
ARG GO_VERSION

RUN microdnf install -y wget tar gzip git && microdnf clean all

RUN wget -q https://go.dev/dl/go${GO_VERSION}.linux-ppc64le.tar.gz -O go.tar.gz && \
    tar -C /usr/local -xzf go.tar.gz && \
    rm go.tar.gz

ENV PATH="/usr/local/go/bin:${PATH}"

RUN set -eux; \
    git clone https://github.com/caddyserver/caddy.git /tmp/caddy; \
    cd /tmp/caddy; \
    git checkout ${CADDY_VERSION}; \
    go build -o /usr/bin/caddy ./cmd/caddy

FROM registry.access.redhat.com/ubi9/ubi-minimal:9.8

ARG CADDY_VERSION

RUN microdnf install -y ca-certificates libcap wget shadow-utils && microdnf clean all

RUN set -eux; \
    groupadd --system --gid 1196 caddy; \
    useradd --system --uid 1196 --gid caddy --shell /sbin/nologin --no-create-home caddy; \
    mkdir -p /config/caddy /data/caddy /etc/caddy /usr/share/caddy; \
    wget -O /etc/caddy/Caddyfile "https://github.com/caddyserver/dist/raw/${CADDY_VERSION}/config/Caddyfile"; \
    wget -O /usr/share/caddy/index.html "https://github.com/caddyserver/dist/raw/${CADDY_VERSION}/welcome/index.html"; \
    chown -R caddy:caddy /config /data /etc/caddy /usr/share/caddy

COPY --from=builder /usr/bin/caddy /usr/bin/caddy

RUN set -eux; \
    setcap cap_net_bind_service=+ep /usr/bin/caddy; \
    chmod +x /usr/bin/caddy; \
    caddy version

ENV XDG_CONFIG_HOME=/config
ENV XDG_DATA_HOME=/data

LABEL org.opencontainers.image.version=${CADDY_VERSION}
LABEL org.opencontainers.image.title=Caddy
LABEL org.opencontainers.image.description="a powerful, enterprise-ready, open source web server with automatic HTTPS written in Go"
LABEL org.opencontainers.image.licenses=Apache-2.0

EXPOSE 80/tcp
EXPOSE 443/tcp
EXPOSE 443/udp
EXPOSE 2019/tcp

WORKDIR /srv

USER caddy

CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
