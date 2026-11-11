package youtube

import (
	"net/http"
	"time"
)

var apiClient = &http.Client{Timeout: 15 * time.Second}
