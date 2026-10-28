# The per-machine Docker daemon: rootless dockerd from the official image,
# pinned by digest, with Kuayle's start script and health check. It runs as
# UID 1000 without privileged mode; see TECHNICAL.md "Machine Docker daemon"
# for the confinement it needs. The Docker CLI pins in
# common/install-docker-cli.sh follow this image's Docker version.
FROM docker:29.9.1-dind-rootless@sha256:825e451c9bd898245ca8fb07bd9a48999ccf33c0192fd68594c73aed7b67d11f

USER 0:0
COPY docker/kuayle-docker-start docker/kuayle-docker-healthcheck /usr/local/bin/
RUN chmod 0755 /usr/local/bin/kuayle-docker-start /usr/local/bin/kuayle-docker-healthcheck \
    && dockerd --version \
    && rootlesskit --version
USER 1000:1000
ENV DOCKER_TLS_CERTDIR=/certs
ENTRYPOINT ["/usr/local/bin/kuayle-docker-start"]
