# The Weir server: the API and a worker in one program.
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
FROM golang:1.26-bookworm AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/weir ./cmd/weir

FROM debian:bookworm-slim
RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates git \
    && rm -rf /var/lib/apt/lists/*
COPY --from=build /out/weir /usr/local/bin/weir
RUN useradd --create-home --shell /usr/sbin/nologin weir
USER weir
WORKDIR /home/weir
ENV WEIR_REQUIRE_ADMIN_KEY=1
EXPOSE 8080
ENTRYPOINT ["weir"]
CMD ["serve"]
