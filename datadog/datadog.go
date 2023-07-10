package datadog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cresta/gotracing"
	"github.com/gorilla/mux"
	"google.golang.org/grpc"

	"github.com/cresta/zapctx"

	"go.uber.org/zap"
	grpctrace "gopkg.in/DataDog/dd-trace-go.v1/contrib/google.golang.org/grpc"
	ddtrace2 "gopkg.in/DataDog/dd-trace-go.v1/contrib/gorilla/mux"
	ddhttp "gopkg.in/DataDog/dd-trace-go.v1/contrib/net/http"
	"gopkg.in/DataDog/dd-trace-go.v1/ddtrace/tracer"
)

// Abstract these constants out
const ddApmFile = "/var/run/datadog/apm.socket"
const ddStatsFile = "/var/run/datadog/dsd.socket"

var (
	_ gotracing.Constructor = NewTracer
	// Check every 5 seconds
	fileExistsCheckInterval = time.Second * 5
	fileExistsMaxAttempt    = 5
)

type config struct {
	ApmFile   string `json:"DD_APM_RECEIVER_SOCKET"`
	StatsFile string `json:"DD_DOGSTATSD_SOCKET"`
}

func (c *config) apmFile() string {
	if c.ApmFile == "" {
		return ddApmFile
	}
	return c.ApmFile
}

func (c *config) statsFile() string {
	if c.StatsFile == "" {
		return ddStatsFile
	}
	return c.StatsFile
}

func envToStruct(env []string, into interface{}) error {
	m := make(map[string]string)
	for _, e := range env {
		p := strings.SplitN(e, "=", 1)
		if len(p) != 2 {
			continue
		}
		m[p[0]] = p[1]
	}
	b, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("unable to convert environment into map: %w", err)
	}
	return json.Unmarshal(b, into)
}

func NewTracer(originalConfig gotracing.Config) (gotracing.Tracing, error) {
	var cfg config
	if err := envToStruct(originalConfig.Env, &cfg); err != nil {
		return nil, fmt.Errorf("unable to convert env to config: %w", err)
	}
	if !fileExists(cfg.apmFile()) {
		return nil, fmt.Errorf("unable to find datadog APM file %s", cfg.apmFile())
	}

	startOptions := []tracer.StartOption{
		tracer.WithRuntimeMetrics(), tracer.WithLogger(ddZappedLogger{originalConfig.Log}), tracer.WithUDS(cfg.apmFile()),
	}
	if fileExists(cfg.statsFile()) {
		startOptions = append(startOptions, tracer.WithDogstatsdAddress("unix://"+cfg.statsFile()))
	}
	tracer.Start(startOptions...)
	originalConfig.Log.Info(context.Background(), "DataDog tracing enabled")
	return &Tracing{
		serviceName: originalConfig.ServiceName,
	}, nil
}

var _ gotracing.Tracing = &Tracing{}

type Tracing struct {
	serviceName string
}

func (t *Tracing) GrpcClientInterceptors(serviceName string) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	si := grpctrace.StreamClientInterceptor(grpctrace.WithServiceName(serviceName), grpctrace.WithUntracedMethods("/grpc.health.v1.Health/Check"))
	ui := grpctrace.UnaryClientInterceptor(grpctrace.WithServiceName(serviceName), grpctrace.WithUntracedMethods("/grpc.health.v1.Health/Check"))
	return []grpc.UnaryClientInterceptor{ui}, []grpc.StreamClientInterceptor{si}
}

func (t *Tracing) GrpcServerInterceptors(serviceName string) ([]grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor) {
	si := grpctrace.StreamServerInterceptor(grpctrace.WithServiceName(serviceName), grpctrace.WithUntracedMethods("/grpc.health.v1.Health/Check"))
	ui := grpctrace.UnaryServerInterceptor(grpctrace.WithServiceName(serviceName), grpctrace.WithUntracedMethods("/grpc.health.v1.Health/Check"))
	return []grpc.UnaryServerInterceptor{ui}, []grpc.StreamServerInterceptor{si}
}

func (t *Tracing) StartSpanFromContext(ctx context.Context, cfg gotracing.SpanConfig, callback func(ctx context.Context) error) (retErr error) {
	span, ctx := tracer.StartSpanFromContext(ctx, cfg.OperationName)
	defer func() {
		var opts []tracer.FinishOption
		if retErr != nil {
			opts = append(opts, tracer.WithError(retErr))
		}
		span.Finish(opts...)
	}()
	return callback(ctx)
}

func (t *Tracing) AttachTag(ctx context.Context, key string, value interface{}) {
	sp, ok := tracer.SpanFromContext(ctx)
	if !ok {
		return
	}
	sp.SetTag(key, value)
}

func (t *Tracing) DynamicFields() []zapctx.DynamicFields {
	return []zapctx.DynamicFields{
		func(ctx context.Context) []zap.Field {
			sp, ok := tracer.SpanFromContext(ctx)
			if !ok || sp.Context().TraceID() == 0 {
				return nil
			}
			return []zap.Field{
				zap.Uint64("dd.trace_id", sp.Context().TraceID()),
				zap.Uint64("dd.span_id", sp.Context().SpanID()),
			}
		},
	}
}

func (t *Tracing) CreateRootMux() (*mux.Router, http.Handler) {
	var opts []ddtrace2.RouterOption
	if t.serviceName != "" {
		opts = append(opts, ddtrace2.WithServiceName(t.serviceName))
	}
	ret := ddtrace2.NewRouter(opts...)
	return ret.Router, ret
}

func (t *Tracing) WrapRoundTrip(rt http.RoundTripper) http.RoundTripper {
	if t == nil {
		return rt
	}
	return ddhttp.WrapRoundTripper(rt)
}

func fileExists(filename string) bool {
	for attempt := 0; attempt < fileExistsMaxAttempt; attempt++ {
		info, err := os.Stat(filename)
		if !os.IsNotExist(err) {
			return !info.IsDir()
		}
		time.Sleep(fileExistsCheckInterval)
	}
	return false
}

type ddZappedLogger struct {
	*zapctx.Logger
}

func (d ddZappedLogger) Log(msg string) {
	d.Logger.Info(context.Background(), msg)
}
