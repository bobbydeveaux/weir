# The Weir server: the API, a worker and the console in one image.
#
# Build it from the repository's root:
#
#   docker build -f deploy/Dockerfile -t weir .
#
# It needs a Postgres database (WEIR_DATABASE_URL, or WEIR_DATABASE_HOST and
# its parts) and an admin key (WEIR_ADMIN_KEY). It refuses to start without
# the key, so that a server on a network is never open by accident.
#
# It holds git, which checking out and committing to a repository need. It
# does not hold a model's command line: agents run in containers of their own
# (deploy/agent.Dockerfile), not in this one.
# The program is built on the machine doing the build and for the machine it
# will run on, so that an image for another kind of processor is not built
# under emulation.
FROM --platform=$BUILDPLATFORM golang:1.26-bookworm AS build
ARG TARGETOS TARGETARCH
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags "-s -w" -o /out/weir ./cmd/weir

# The console is plain static files, the same for every kind of processor.
FROM --platform=$BUILDPLATFORM node:24-bookworm-slim AS console
WORKDIR /console
COPY console/package.json console/package-lock.json console/.npmrc ./
RUN npm ci
COPY console/ ./
RUN npm run build

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/weir /usr/local/bin/weir
COPY --from=console /console/build /usr/share/weir/console
RUN useradd --uid 10001 --create-home --shell /usr/sbin/nologin weir
USER 10001
WORKDIR /home/weir
ENV WEIR_REQUIRE_ADMIN_KEY=1
# The server serves the console at every address that is not the API's.
ENV WEIR_CONSOLE_DIR=/usr/share/weir/console
EXPOSE 8080
ENTRYPOINT ["weir"]
CMD ["serve"]
