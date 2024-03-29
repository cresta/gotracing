package gotracing

import (
	"context"
	"fmt"
	"net/http"

	"github.com/cresta/zapctx"
	"github.com/gorilla/mux"
	"google.golang.org/grpc"
)

type SpanConfig struct {
	OperationName string
}

type Tracing interface {
	WrapRoundTrip(rt http.RoundTripper) http.RoundTripper
	AttachTag(ctx context.Context, key string, value interface{})
	DynamicFields() []zapctx.DynamicFields
	CreateRootMux() (*mux.Router, http.Handler)
	GrpcServerInterceptors(serviceName string) ([]grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor)
	GrpcClientInterceptors(serviceName string) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor)
	StartSpanFromContext(ctx context.Context, cfg SpanConfig, callback func(ctx context.Context) error) error
}

type Constructor func(config Config) (Tracing, error)

type Registry struct {
	Constructors map[string]Constructor
}

func (r *Registry) New(name string, config Config) (Tracing, error) {
	if name == "" || r == nil {
		config.Log.Info(context.Background(), "returning no-op tracer")
		return Noop{}, nil
	}
	cons, exists := r.Constructors[name]
	if !exists {
		return nil, fmt.Errorf("unable to find tracer named: %s", name)
	}
	ret, err := cons(config)
	if err != nil {
		return nil, fmt.Errorf("unable to create registry %s: %w", name, err)
	}
	return ret, nil
}

type Config struct {
	ServiceName string
	Log         *zapctx.Logger
	Env         []string
}

var _ Tracing = Noop{}

type Noop struct{}

func (n Noop) GrpcServerInterceptors(_ string) ([]grpc.UnaryServerInterceptor, []grpc.StreamServerInterceptor) {
	return nil, nil
}

func (n Noop) GrpcClientInterceptors(_ string) ([]grpc.UnaryClientInterceptor, []grpc.StreamClientInterceptor) {
	return nil, nil
}

func (n Noop) StartSpanFromContext(ctx context.Context, _ SpanConfig, callback func(ctx context.Context) error) error {
	return callback(ctx)
}

func (n Noop) WrapRoundTrip(rt http.RoundTripper) http.RoundTripper {
	return rt
}

func (n Noop) AttachTag(_ context.Context, _ string, _ interface{}) {
}

func (n Noop) DynamicFields() []zapctx.DynamicFields {
	return nil
}

func (n Noop) CreateRootMux() (*mux.Router, http.Handler) {
	ret := mux.NewRouter()
	return ret, ret
}

// MuxTagging adds key/value tags to the context for a handler based upon the mux Vars
func MuxTagging(t Tracing) func(handler http.Handler) http.Handler {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			for k, v := range mux.Vars(request) {
				t.AttachTag(request.Context(), fmt.Sprintf("mux.vars.%s", k), v)
			}
			if r := mux.CurrentRoute(request); r != nil {
				if r.GetName() != "" {
					t.AttachTag(request.Context(), "mux.name", r.GetName())
				}
			}
			handler.ServeHTTP(writer, request)
		})
	}
}
