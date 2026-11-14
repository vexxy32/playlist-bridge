//go:build !playlist_bridge_demo

package server

import "net/http"

func registerDemoRoutes(*http.ServeMux) {}
