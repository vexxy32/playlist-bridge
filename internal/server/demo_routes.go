//go:build playlist_bridge_demo

package server

import "net/http"

func registerDemoRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/offline-tests", listOfflineTests)
	mux.HandleFunc("POST /api/offline-tests/{id}", runOfflineTest)
}
