# The image an agent runs in when agents have containers of their own
# (WEIR_AGENT_RUNNER=docker, and the cloud launchers).
#
# It holds the weir program, the Claude command line and git. It is given
# Weir's address, the one-time token for one step and the model's credential,
# and nothing else. Add the toolchains your code needs on top of it:
#
#   FROM ghcr.io/you/weir-agent
#   USER root
#   RUN apt-get update && apt-get install -y golang && rm -rf /var/lib/apt/lists/*
#   USER agent
#
# Build it from the repository's root:
#
#   docker build -f deploy/agent.Dockerfile -t weir-agent .
# The program is built on the machine doing the build and for the machine it
# will run on, so that an image for another kind of processor is not built
# under emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# The agent's program needs no bucket libraries: it talks only to Weir's API.
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -tags nobuckets -trimpath -ldflags "-s -w" -o /out/weir ./cmd/weir

FROM node:22-bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git curl ripgrep \
    && rm -rf /var/lib/apt/lists/*
ARG CLAUDE_CODE_VERSION=latest
RUN npm install -g "@anthropic-ai/claude-code@${CLAUDE_CODE_VERSION}" && npm cache clean --force
COPY --from=build /out/weir /usr/local/bin/weir
# The agent is not root: it can do what it likes to its own directory and
# home, and nothing to the image.
# The user is given by number, which is what a cluster needs to see to know
# the container does not run as root.
RUN useradd --uid 10001 --create-home --shell /bin/bash agent
USER 10001
WORKDIR /home/agent
ENV HOME=/home/agent
CMD ["weir", "agent"]
