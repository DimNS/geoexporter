# geoexporter

## Install

1. For `Prometheus` to be able to see the geoexporter, you need to enable `extra_hosts`.

```yaml
services:
  prometheus:
    ...
    extra_hosts:
      - "host.docker.internal:host-gateway"
```

2. Add the following lines to `prometheus.yaml`

```yaml
scrape_configs:
  - job_name: "geoexporter"
    static_configs:
      - targets: ["host.docker.internal:9123"]
```

## Metrics

| Metric                              | Type  | Labels    | Description                   |
| ----------------------------------- | ----- | --------- | ----------------------------- |
| `geo_active_connections_by_country` | gauge | `country` | Active connections by country |
