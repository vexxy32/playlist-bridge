package providerhttp

import (
	"context"
	"net/http"
)

type transportKey struct{}

// WithTransport задаёт подменный транспорт только для текущего запроса.
func WithTransport(ctx context.Context, transport http.RoundTripper) context.Context {
	return context.WithValue(ctx, transportKey{}, transport)
}

// Do выполняет запрос через обычный или заданный в контексте транспорт.
func Do(client *http.Client, request *http.Request) (*http.Response, error) {
	if transport, ok := request.Context().Value(transportKey{}).(http.RoundTripper); ok {
		offline := *client
		offline.Transport = transport
		return offline.Do(request)
	}
	return client.Do(request)
}
