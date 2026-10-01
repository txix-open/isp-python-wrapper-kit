package service_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/txix-open/isp-kit/http/httpcli"
	"github.com/txix-open/isp-kit/log"
	"github.com/txix-open/isp-python-wrapper-kit/service"
)

const sampleMetrics = `# HELP py_handler_total Total requests handled.
# TYPE py_handler_total counter
py_handler_total{path="/",method="GET"} 42
# HELP py_temperature Current temperature.
# TYPE py_temperature gauge
py_temperature{unit="c"} 3.14
`

func newTestLogger(t *testing.T) *log.Adapter {
	t.Helper()
	logger, err := log.New(log.WithDisableDefaultOutput())
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	return logger
}

func newTestRegistry(t *testing.T, body string) *prometheus.Registry {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)

	cli := httpcli.New()
	cli.GlobalRequestConfig().BaseUrl = ts.URL

	reg := prometheus.NewRegistry()
	err := reg.Register(service.NewPythonMetricsCollector(cli, newTestLogger(t)))
	if err != nil {
		t.Fatalf("register collector: %v", err)
	}
	return reg
}

func findFamily(t *testing.T, mfs []*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()
	for _, mf := range mfs {
		if mf.GetName() == name {
			return mf
		}
	}
	return nil
}

func TestPythonMetricsCollector_PassesThroughCounterAndGauge(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t, sampleMetrics)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	counter := findFamily(t, mfs, "py_handler_total")
	if counter == nil {
		t.Fatal("py_handler_total family missing")
	}
	if got := counter.GetMetric()[0].GetCounter().GetValue(); got != 42 {
		t.Errorf("counter value = %v, want 42", got)
	}

	gauge := findFamily(t, mfs, "py_temperature")
	if gauge == nil {
		t.Fatal("py_temperature family missing")
	}
	if got := gauge.GetMetric()[0].GetGauge().GetValue(); got != 3.14 {
		t.Errorf("gauge value = %v, want 3.14", got)
	}

	up := findFamily(t, mfs, "py_metrics_scrape_up")
	if up == nil || up.GetMetric()[0].GetGauge().GetValue() != 1 {
		t.Errorf("py_metrics_scrape_up not present or not 1: %+v", up)
	}
}

const sampleHistogram = `# HELP py_request_duration_seconds Request duration.
# TYPE py_request_duration_seconds histogram
py_request_duration_seconds_bucket{le="0.5"} 1
py_request_duration_seconds_bucket{le="1"} 2
py_request_duration_seconds_bucket{le="+Inf"} 2
py_request_duration_seconds_sum 0.9
py_request_duration_seconds_count 2
# HELP py_latency Summary of latency.
# TYPE py_latency summary
py_latency{route="/",quantile="0.5"} 1.5
py_latency_sum{route="/"} 4.5
py_latency_count{route="/"} 3
`

func TestPythonMetricsCollector_PassesThroughHistogramAndSummary(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t, sampleHistogram)
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	hist := findFamily(t, mfs, "py_request_duration_seconds")
	if hist == nil {
		t.Fatal("histogram family missing")
	}
	h := hist.GetMetric()[0].GetHistogram()
	if h.GetSampleCount() != 2 || h.GetSampleSum() != 0.9 {
		t.Errorf("histogram count/sum = %d/%v, want 2/0.9", h.GetSampleCount(), h.GetSampleSum())
	}

	sum := findFamily(t, mfs, "py_latency")
	if sum == nil {
		t.Fatal("summary family missing")
	}
	s := sum.GetMetric()[0].GetSummary()
	if s.GetSampleCount() != 3 || s.GetSampleSum() != 4.5 || len(s.GetQuantile()) != 1 || s.GetQuantile()[0].GetValue() != 1.5 {
		t.Errorf("summary = %+v, want count=3 sum=4.5 q[0]=1.5", s)
	}
}

func TestPythonMetricsCollector_ChildReturns500(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(ts.Close)
	cli := httpcli.New()
	cli.GlobalRequestConfig().BaseUrl = ts.URL

	reg := prometheus.NewRegistry()
	err := reg.Register(service.NewPythonMetricsCollector(cli, newTestLogger(t)))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	if up := findFamily(t, mfs, "py_metrics_scrape_up"); up == nil || up.GetMetric()[0].GetGauge().GetValue() != 0 {
		t.Errorf("py_metrics_scrape_up = %+v, want 0", up)
	}
	if f := findFamily(t, mfs, "py_handler_total"); f != nil {
		t.Errorf("child family leaked on failure: %s", f.GetName())
	}
}

func TestPythonMetricsCollector_ChildUnreachable(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := ts.URL
	ts.Close() // force connection refused

	cli := httpcli.New()
	cli.GlobalRequestConfig().BaseUrl = url
	reg := prometheus.NewRegistry()
	err := reg.Register(service.NewPythonMetricsCollector(cli, newTestLogger(t)))
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather must not error when child is unreachable: %v", err)
	}
	if up := findFamily(t, mfs, "py_metrics_scrape_up"); up == nil || up.GetMetric()[0].GetGauge().GetValue() != 0 {
		t.Errorf("py_metrics_scrape_up = %+v, want 0", up)
	}
}

func TestPythonMetricsCollector_PreservesLabels(t *testing.T) {
	t.Parallel()
	reg := newTestRegistry(t, sampleMetrics)
	mfs, _ := reg.Gather()
	counter := findFamily(t, mfs, "py_handler_total")
	if counter == nil {
		t.Fatal("py_handler_total missing")
	}
	labels := map[string]string{}
	for _, lp := range counter.GetMetric()[0].GetLabel() {
		labels[lp.GetName()] = lp.GetValue()
	}
	if labels["path"] != "/" || labels["method"] != "GET" {
		t.Errorf("labels = %v, want path=/ method=GET", labels)
	}
}
