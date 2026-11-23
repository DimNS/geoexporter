# geoexporter

Prometheus exporter for active TCP/UDP connections by peer country.

The socket tables are read from `/proc/<pid>/net/{tcp,tcp6,udp,udp6}` of the host network namespace.

## Install

Run the container with the host `/proc` mounted read-only:

```yaml
services:
  geoexporter:
    image: ghcr.io/dimns/geoexporter:latest
    restart: unless-stopped
    command: ["-proc", "/host/proc"]
    volumes:
      - "/proc:/host/proc:ro"
```

`-proc` must equal the mount destination of the host `/proc`: the image sets no default of its own, so the path lives only in the compose file. Prometheus only has to reach it over the Docker network:

```yaml
scrape_configs:
  - job_name: "geoexporter"
    static_configs:
      - targets: ["geoexporter:9123"]
```

To run the binary directly on a host, point `-proc` at the local `/proc`:

```sh
geoexporter -proc /proc
```

## What it sees

Connections are counted from the host network namespace. Connections that live in a container network namespace are not visible there. Reading `/host/proc/*/net/*` would cover them as well.

## Metrics

| Metric                              | Type  | Labels    | Description                   |
| ----------------------------------- | ----- | --------- | ----------------------------- |
| `geo_active_connections_by_country` | gauge | `country` | Active connections by country |
