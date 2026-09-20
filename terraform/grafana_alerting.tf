resource "grafana_folder" "alerting_folder" {
  provider = grafana.stack
  title    = "OIBP Alerts"
}


resource "grafana_rule_group" "application_critical" {
  provider = grafana.stack

  name             = "OIBP Application Critical"
  folder_uid       = grafana_folder.alerting_folder.uid
  interval_seconds = 60

  rule {
    name = "Batch HTTP failures | HTTP 5xx ratio | >5% for 10m with traffic"
    uid  = "oibp-http-5xx-5percent"

    condition      = "B"
    for            = "10m"
    no_data_state  = "OK"
    exec_err_state = "Alerting"

    labels = {
      severity = "critical"
      service  = "batch-api"
      signal   = "http-5xx"
    }

    annotations = {
      summary     = "Batch HTTP requests are returning server errors"
      description = "More than 5% of batch API HTTP requests returned a 5xx status for 10 minutes while request traffic exceeded 0.05 requests per second."
    }

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.from_name.uid
      query_type     = ""

      relative_time_range {
        from = 300
        to   = 0
      }

      model = jsonencode({
        expr           = <<-EOT
          (
            sum(rate(http_server_request_duration_seconds_count{job="openitinerary-batch-api",http_response_status_code=~"5.."}[5m]))
            /
            clamp_min(sum(rate(http_server_request_duration_seconds_count{job="openitinerary-batch-api"}[5m])), 1e-9)
          )
          and
          sum(rate(http_server_request_duration_seconds_count{job="openitinerary-batch-api"}[5m])) > 0.05
        EOT
        format         = "time_series"
        intervalFactor = 1
        interval       = ""
        maxDataPoints  = 43200
        legendFormat   = "batch-api"
        refId          = "A"
      })
    }

    data {
      ref_id         = "B"
      datasource_uid = "-100"
      query_type     = ""

      relative_time_range {
        from = 0
        to   = 0
      }

      model = jsonencode({
        conditions = [{
          evaluator = {
            params = [0.05]
            type   = "gt"
          }
          operator = {
            type = "and"
          }
          query = {
            params = ["A"]
          }
          reducer = {
            params = []
            type   = "last"
          }
          type = "query"
        }]
        datasource = {
          type = "__expr__"
          uid  = "-100"
        }
        hide          = false
        intervalMs    = 1000
        maxDataPoints = 43200
        refId         = "B"
        type          = "classic_conditions"
      })
    }
  }

  rule {
    name = "Batch gRPC failures | non-OK ratio | >5% for 10m with traffic"
    uid  = "oibp-grpc-errors-5percent"

    condition      = "B"
    for            = "10m"
    no_data_state  = "OK"
    exec_err_state = "Alerting"

    labels = {
      severity = "critical"
      service  = "batch-grpc"
      signal   = "grpc-errors"
    }

    annotations = {
      summary     = "Batch gRPC requests are returning errors"
      description = "More than 5% of batch gRPC calls returned a non-OK status for 10 minutes while RPC traffic exceeded 0.05 calls per second."
    }

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.from_name.uid
      query_type     = ""

      relative_time_range {
        from = 300
        to   = 0
      }

      model = jsonencode({
        expr           = <<-EOT
          (
            sum(rate(rpc_server_call_duration_seconds_count{job="openitinerary-batch-grpc",rpc_response_status_code!="OK"}[5m]))
            /
            clamp_min(sum(rate(rpc_server_call_duration_seconds_count{job="openitinerary-batch-grpc"}[5m])), 1e-9)
          )
          and
          sum(rate(rpc_server_call_duration_seconds_count{job="openitinerary-batch-grpc"}[5m])) > 0.05
        EOT
        format         = "time_series"
        intervalFactor = 1
        interval       = ""
        maxDataPoints  = 43200
        legendFormat   = "batch-grpc"
        refId          = "A"
      })
    }

    data {
      ref_id         = "B"
      datasource_uid = "-100"
      query_type     = ""

      relative_time_range {
        from = 0
        to   = 0
      }

      model = jsonencode({
        conditions = [{
          evaluator = {
            params = [0.05]
            type   = "gt"
          }
          operator = {
            type = "and"
          }
          query = {
            params = ["A"]
          }
          reducer = {
            params = []
            type   = "last"
          }
          type = "query"
        }]
        datasource = {
          type = "__expr__"
          uid  = "-100"
        }
        hide          = false
        intervalMs    = 1000
        maxDataPoints = 43200
        refId         = "B"
        type          = "classic_conditions"
      })
    }
  }

  rule {
    name = "Adapter HTTP failures | HTTP 5xx ratio | >5% for 10m with traffic"
    uid  = "oi-adapter-http-5xx-5percent"

    condition      = "B"
    for            = "10m"
    no_data_state  = "OK"
    exec_err_state = "Alerting"

    labels = {
      severity = "critical"
      service  = "adapter-api"
      signal   = "http-5xx"
    }

    annotations = {
      summary     = "Adapter API requests are returning server errors"
      description = "More than 5% of Adapter API HTTP requests returned a 5xx status for 10 minutes while request traffic exceeded 0.05 requests per second."
    }

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.from_name.uid
      query_type     = ""

      relative_time_range {
        from = 300
        to   = 0
      }

      model = jsonencode({
        expr           = <<-EOT
          (
            (
              sum(rate(http_server_request_duration_seconds_count{job="oi-adapter",http_response_status_code=~"5.."}[5m]))
              /
              clamp_min(sum(rate(http_server_request_duration_seconds_count{job="oi-adapter"}[5m])), 1e-9)
            )
            and
            sum(rate(http_server_request_duration_seconds_count{job="oi-adapter"}[5m])) > 0.05
          )
          or
          (
            (
              sum(rate(http_server_duration_milliseconds_count{job="oi-adapter",http_status_code=~"5.."}[5m]))
              /
              clamp_min(sum(rate(http_server_duration_milliseconds_count{job="oi-adapter"}[5m])), 1e-9)
            )
            and
            sum(rate(http_server_duration_milliseconds_count{job="oi-adapter"}[5m])) > 0.05
          )
        EOT
        format         = "time_series"
        intervalFactor = 1
        interval       = ""
        maxDataPoints  = 43200
        legendFormat   = "adapter-api"
        refId          = "A"
      })
    }

    data {
      ref_id         = "B"
      datasource_uid = "-100"
      query_type     = ""

      relative_time_range {
        from = 0
        to   = 0
      }

      model = jsonencode({
        conditions = [{
          evaluator = {
            params = [0.05]
            type   = "gt"
          }
          operator = {
            type = "and"
          }
          query = {
            params = ["A"]
          }
          reducer = {
            params = []
            type   = "last"
          }
          type = "query"
        }]
        datasource = {
          type = "__expr__"
          uid  = "-100"
        }
        hide          = false
        intervalMs    = 1000
        maxDataPoints = 43200
        refId         = "B"
        type          = "classic_conditions"
      })
    }
  }

  rule {
    name = "Terminal job failures | failed ratio | >5% for 10m with completions"
    uid  = "oibp-terminal-failures-5percent"

    condition      = "B"
    for            = "10m"
    no_data_state  = "OK"
    exec_err_state = "Alerting"

    labels = {
      severity = "critical"
      service  = "batch-processing"
      signal   = "terminal-job-failures"
    }

    annotations = {
      summary     = "Jobs are reaching terminal failure"
      description = "More than 5% of observed terminal job events were failures for 10 minutes while terminal job activity exceeded 0.01 events per second."
    }

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.from_name.uid
      query_type     = ""

      relative_time_range {
        from = 300
        to   = 0
      }

      model = jsonencode({
        expr           = <<-EOT
          (
            sum(rate(oibp_jobs_terminal_total{outcome="failed"}[5m]))
            /
            clamp_min(sum(rate(oibp_jobs_terminal_total[5m])), 1e-9)
          )
          and
          sum(rate(oibp_jobs_terminal_total[5m])) > 0.01
        EOT
        format         = "time_series"
        intervalFactor = 1
        interval       = ""
        maxDataPoints  = 43200
        legendFormat   = "terminal-job-failures"
        refId          = "A"
      })
    }

    data {
      ref_id         = "B"
      datasource_uid = "-100"
      query_type     = ""

      relative_time_range {
        from = 0
        to   = 0
      }

      model = jsonencode({
        conditions = [{
          evaluator = {
            params = [0.05]
            type   = "gt"
          }
          operator = {
            type = "and"
          }
          query = {
            params = ["A"]
          }
          reducer = {
            params = []
            type   = "last"
          }
          type = "query"
        }]
        datasource = {
          type = "__expr__"
          uid  = "-100"
        }
        hide          = false
        intervalMs    = 1000
        maxDataPoints = 43200
        refId         = "B"
        type          = "classic_conditions"
      })
    }
  }

  rule {
    name = "Worker failures | failed run ratio | >20% for 10m with activity"
    uid  = "oibp-worker-failures-20percent"

    condition      = "B"
    for            = "10m"
    no_data_state  = "OK"
    exec_err_state = "Alerting"

    labels = {
      severity = "critical"
      service  = "batch-worker"
      signal   = "worker-failures"
    }

    annotations = {
      summary     = "Worker runs are failing"
      description = "More than 20% of worker runs failed for 10 minutes while worker activity exceeded 0.01 runs per second."
    }

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.from_name.uid
      query_type     = ""

      relative_time_range {
        from = 300
        to   = 0
      }

      model = jsonencode({
        expr           = <<-EOT
          (
            sum(rate(oibp_worker_runs_total{job="openitinerary-batch-worker",outcome="failed"}[5m]))
            /
            clamp_min(sum(rate(oibp_worker_runs_total{job="openitinerary-batch-worker"}[5m])), 1e-9)
          )
          and
          sum(rate(oibp_worker_runs_total{job="openitinerary-batch-worker"}[5m])) > 0.01
        EOT
        format         = "time_series"
        intervalFactor = 1
        interval       = ""
        maxDataPoints  = 43200
        legendFormat   = "batch-worker"
        refId          = "A"
      })
    }

    data {
      ref_id         = "B"
      datasource_uid = "-100"
      query_type     = ""

      relative_time_range {
        from = 0
        to   = 0
      }

      model = jsonencode({
        conditions = [{
          evaluator = {
            params = [0.2]
            type   = "gt"
          }
          operator = {
            type = "and"
          }
          query = {
            params = ["A"]
          }
          reducer = {
            params = []
            type   = "last"
          }
          type = "query"
        }]
        datasource = {
          type = "__expr__"
          uid  = "-100"
        }
        hide          = false
        intervalMs    = 1000
        maxDataPoints = 43200
        refId         = "B"
        type          = "classic_conditions"
      })
    }
  }

}

resource "grafana_rule_group" "telemetry_critical" {
  provider = grafana.stack

  name             = "OIBP Telemetry Critical"
  folder_uid       = grafana_folder.alerting_folder.uid
  interval_seconds = 30


  rule {
    name = "OTEL Collector unavailable | scrape target down | >5m"
    uid  = "oibp-otel-collector-down"

    # NOTE: at the time of writing, there collector is not wired up into
    # Grafana cloud yet so this is firing.
    is_paused = true

    condition      = "B"
    for            = "5m"
    no_data_state  = "Alerting"
    exec_err_state = "Alerting"

    labels = {
      severity = "critical"
      service  = "telemetry"
      signal   = "otel-collector-unavailable"
    }

    annotations = {
      summary     = "The OTEL Collector scrape target is unavailable"
      description = "Prometheus has reported the OTEL Collector target as down for 5 minutes. Application telemetry may not be reaching Grafana."
    }

    data {
      ref_id         = "A"
      datasource_uid = data.grafana_data_source.from_name.uid
      query_type     = ""

      relative_time_range {
        from = 60
        to   = 0
      }

      model = jsonencode({
        expr           = "up{job=\"otel-collector\"}"
        format         = "time_series"
        intervalFactor = 1
        interval       = ""
        maxDataPoints  = 43200
        legendFormat   = "otel-collector"
        refId          = "A"
      })
    }

    data {
      ref_id         = "B"
      datasource_uid = "-100"
      query_type     = ""

      relative_time_range {
        from = 0
        to   = 0
      }

      model = jsonencode({
        conditions = [{
          evaluator = {
            params = [1]
            type   = "lt"
          }
          operator = {
            type = "and"
          }
          query = {
            params = ["A"]
          }
          reducer = {
            params = []
            type   = "last"
          }
          type = "query"
        }]
        datasource = {
          type = "__expr__"
          uid  = "-100"
        }
        hide          = false
        intervalMs    = 1000
        maxDataPoints = 43200
        refId         = "B"
        type          = "classic_conditions"
      })
    }
  }
}

variable "oibp_developers_emails" {
  type = list(string)
}

resource "grafana_contact_point" "my_contact_point" {
  provider = grafana.stack
  name     = "oibp_developers"
  email {
    addresses               = var.oibp_developers_emails
    message                 = "{{ len .Alerts.Firing }} firing."
    subject                 = "{{ template \"default.title\" .}}"
    single_email            = true
    disable_resolve_message = false
  }
}

resource "grafana_notification_policy" "oibp_critical" {
  provider = grafana.stack

  contact_point = grafana_contact_point.my_contact_point.name
  group_by      = ["alertname", "service", "signal"]

  group_wait      = "30s"
  group_interval  = "5m"
  repeat_interval = "4h"

  policy {
    matcher {
      label = "severity"
      match = "="
      value = "critical"
    }

    contact_point = grafana_contact_point.my_contact_point.name
  }
}
