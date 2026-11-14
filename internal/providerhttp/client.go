package providerhttp

import (
	"context"
	"net/http"
)

type transportKey struct{}

// WithTransport scopes a replacement transport to one operation.
func WithTransport(ctx context.Context, transport http.RoundTripper) context.Context {
	return context.WithValue(ctx, transportKey{}, transport)
}

// Do uses the configured client, with an optional request-scoped transport.
func Do(client *http.Client, request *http.Request) (*http.Response, error) {
	if transport, ok := request.Context().Value(transportKey{}).(http.RoundTripper); ok {
		offline := *client
		offline.Transport = transport
		return offline.Do(request)
	}
	return client.Do(request)
}
