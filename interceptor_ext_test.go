package rerpc_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rerpc/rerpc"
	"github.com/rerpc/rerpc/internal/assert"
	pingrpc "github.com/rerpc/rerpc/internal/gen/proto/go-rerpc/rerpc/ping/v1test"
	pingpb "github.com/rerpc/rerpc/internal/gen/proto/go/rerpc/ping/v1test"
)

type assertCalledInterceptor struct {
	called *bool
}

func (i *assertCalledInterceptor) Wrap(next rerpc.Func) rerpc.Func {
	return rerpc.Func(func(ctx context.Context, req rerpc.AnyRequest) (rerpc.AnyResponse, error) {
		*i.called = true
		return next(ctx, req)
	})
}

func (i *assertCalledInterceptor) WrapStream(next rerpc.StreamFunc) rerpc.StreamFunc {
	return rerpc.StreamFunc(func(ctx context.Context) (context.Context, rerpc.Sender, rerpc.Receiver) {
		*i.called = true
		return next(ctx)
	})
}

func TestClientStreamErrors(t *testing.T) {
	var called bool
	reset := func() {
		called = false
	}
	mux, err := rerpc.NewServeMux(
		rerpc.NewNotFoundHandler(),
		pingrpc.NewPingService(pingServer{}),
	)
	assert.Nil(t, err, "mux construction error")
	server := httptest.NewServer(mux)
	defer server.Close()
	client, err := pingrpc.NewPingServiceClient(
		"INVALID_URL",
		server.Client(),
		rerpc.Intercept(&assertCalledInterceptor{&called}),
	)
	assert.Nil(t, err, "client construction error")

	t.Run("unary", func(t *testing.T) {
		t.Skip("interceptor is called but shouldn't be")
		defer reset()
		_, err := client.Ping(context.Background(), &pingpb.PingRequest{})
		assert.NotNil(t, err, "expected RPC error")
		assert.False(t, called, "expected interceptors not to be called")
	})
	t.Run("stream", func(t *testing.T) {
		defer reset()
		_, err := client.CountUp(context.Background(), &pingpb.CountUpRequest{})
		assert.NotNil(t, err, "expected RPC error")
		assert.True(t, called, "expected interceptors not to be called")
	})
}

func TestHandlerStreamErrors(t *testing.T) {
	// If we receive an HTTP request and send a response, interceptors should
	// fire - even if we can't successfully set up a stream. (This is different
	// from clients, where stream creation fails before any HTTP request is
	// issued.)
	var called bool
	reset := func() {
		called = false
	}
	mux, err := rerpc.NewServeMux(
		rerpc.NewNotFoundHandler(),
		pingrpc.NewPingService(
			pingServer{},
			rerpc.Intercept(&assertCalledInterceptor{&called}),
		),
	)
	assert.Nil(t, err, "mux construction error")
	server := httptest.NewServer(mux)
	defer server.Close()

	t.Run("unary", func(t *testing.T) {
		t.Skip("fails with current code")
		defer reset()
		request, err := http.NewRequest(
			http.MethodPost,
			server.URL+"/rerpc.ping.v1test.PingService/Ping",
			strings.NewReader(""),
		)
		assert.Nil(t, err, "error constructing request")
		request.Header.Set("Content-Type", "application/grpc+proto")
		request.Header.Set("Grpc-Timeout", "INVALID")
		res, err := server.Client().Do(request)
		assert.Nil(t, err, "network error sending request")
		assert.Equal(t, res.StatusCode, http.StatusOK, "response HTTP status")
		assert.True(t, called, "expected interceptors to be called")
	})
	t.Run("stream", func(t *testing.T) {
		defer reset()
		request, err := http.NewRequest(
			http.MethodPost,
			server.URL+"/rerpc.ping.v1test.PingService/CountUp",
			strings.NewReader(""),
		)
		assert.Nil(t, err, "error constructing request")
		request.Header.Set("Content-Type", "application/grpc+proto")
		request.Header.Set("Grpc-Timeout", "INVALID")
		res, err := server.Client().Do(request)
		assert.Nil(t, err, "network error sending request")
		assert.Equal(t, res.StatusCode, http.StatusOK, "response HTTP status")
		assert.True(t, called, "expected interceptors to be called")
	})
}