package assembly //nolint:testpackage // unit-tests the unexported registerPythonMetrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/txix-open/isp-kit/http/httpcli"
	"github.com/txix-open/isp-kit/log"
	"github.com/txix-open/isp-kit/metrics"
)

const asmSampleMetrics = `# HELP py_thing A metric.
# TYPE py_thing gauge
py_thing 7
`

func newTestLogger(t *testing.T) *log.Adapter {
	t.Helper()
	logger, err := log.New(log.WithDisableDefaultOutput())
	if err != nil {
		t.Fatalf("new logger: %v", err)
	}
	return logger
}

func TestRegisterPythonMetrics_RegistersCollector(t *testing.T) {
	t.Parallel()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(asmSampleMetrics))
	}))
	t.Cleanup(ts.Close)

	cli := httpcli.New()
	cli.GlobalRequestConfig().BaseUrl = ts.URL

	reg := metrics.NewRegistry()
	registerPythonMetrics(reg, cli, newTestLogger(t))

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}
	found := map[string]bool{}
	for _, mf := range mfs {
		found[mf.GetName()] = true
	}
	if !found["py_metrics_scrape_up"] || !found["py_thing"] {
		t.Errorf("expected families, got %v", found)
	}
}
