package service

import (
	"bytes"
	"context"
	"errors"
	"io"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/txix-open/isp-kit/http/httpcli"
	"github.com/txix-open/isp-kit/log"
)

const (
	pythonMetricsEndpoint      = "/internal/metrics"
	pythonMetricsScrapeTimeout = 3 * time.Second

	pythonMetricsUpName = "py_metrics_scrape_up"
	pythonMetricsUpHelp = "Whether the python process /internal/metrics was scraped successfully (1) or not (0)."
)

type PythonMetricsCollector struct {
	cli      *httpcli.Client
	logger   log.Logger
	scrapeUp prometheus.Gauge
}

func NewPythonMetricsCollector(cli *httpcli.Client, logger log.Logger) *PythonMetricsCollector {
	return &PythonMetricsCollector{
		cli:    cli,
		logger: logger,
		scrapeUp: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: pythonMetricsUpName,
			Help: pythonMetricsUpHelp,
		}),
	}
}

func (c *PythonMetricsCollector) Describe(_ chan<- *prometheus.Desc) {
}

func (c *PythonMetricsCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), pythonMetricsScrapeTimeout)
	defer cancel()

	body, status, err := c.cli.Get(pythonMetricsEndpoint).DoAndReadBody(ctx)
	if err != nil {
		c.scrapeUp.Set(0)
		c.logger.Warn(ctx, "scrape python metrics failed", log.Any("error", err))
		c.scrapeUp.Collect(ch)
		return
	}
	if status < 200 || status > 299 {
		c.scrapeUp.Set(0)
		c.logger.Warn(ctx, "scrape python metrics failed", log.Int("status", status))
		c.scrapeUp.Collect(ch)
		return
	}

	c.scrapeUp.Set(1)
	c.scrapeUp.Collect(ch)

	decoder := expfmt.NewDecoder(bytes.NewReader(body), expfmt.NewFormat(expfmt.TypeTextPlain))
	for {
		var mf dto.MetricFamily
		err := decoder.Decode(&mf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			c.logger.Warn(ctx, "parse python metrics failed", log.Any("error", err))
			break
		}
		c.emitFamily(ch, &mf)
	}
}

func (c *PythonMetricsCollector) emitFamily(ch chan<- prometheus.Metric, mf *dto.MetricFamily) {
	labelNames := make([]string, 0, len(mf.GetMetric()[0].GetLabel()))
	seen := make(map[string]struct{}, len(mf.GetMetric()[0].GetLabel()))
	for _, lp := range mf.GetMetric()[0].GetLabel() {
		if _, ok := seen[lp.GetName()]; ok {
			continue
		}
		seen[lp.GetName()] = struct{}{}
		labelNames = append(labelNames, lp.GetName())
	}

	desc := prometheus.NewDesc(mf.GetName(), mf.GetHelp(), labelNames, nil)

	for _, m := range mf.GetMetric() {
		labelValues := make([]string, 0, len(m.GetLabel()))
		for _, lp := range m.GetLabel() {
			labelValues = append(labelValues, lp.GetValue())
		}

		var (
			metric prometheus.Metric
			err    error
		)
		switch mf.GetType() {
		case dto.MetricType_COUNTER:
			metric, err = prometheus.NewConstMetric(desc, prometheus.CounterValue,
				m.GetCounter().GetValue(), labelValues...)
		case dto.MetricType_GAUGE:
			metric, err = prometheus.NewConstMetric(desc, prometheus.GaugeValue,
				m.GetGauge().GetValue(), labelValues...)
		case dto.MetricType_UNTYPED:
			metric, err = prometheus.NewConstMetric(desc, prometheus.UntypedValue,
				m.GetUntyped().GetValue(), labelValues...)
		case dto.MetricType_HISTOGRAM:
			histogram := m.GetHistogram()
			buckets := make(map[float64]uint64, len(histogram.GetBucket()))
			for _, bucket := range histogram.GetBucket() {
				buckets[bucket.GetUpperBound()] = bucket.GetCumulativeCount()
			}
			metric, err = prometheus.NewConstHistogram(desc, histogram.GetSampleCount(),
				histogram.GetSampleSum(), buckets, labelValues...)
		case dto.MetricType_SUMMARY:
			summary := m.GetSummary()
			quantiles := make(map[float64]float64, len(summary.GetQuantile()))
			for _, quantile := range summary.GetQuantile() {
				quantiles[quantile.GetQuantile()] = quantile.GetValue()
			}
			metric, err = prometheus.NewConstSummary(desc, summary.GetSampleCount(),
				summary.GetSampleSum(), quantiles, labelValues...)
		default:
			continue
		}

		if err != nil {
			continue
		}
		ch <- metric
	}
}
