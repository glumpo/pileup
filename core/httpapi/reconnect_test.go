package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
)

func TestReconnectAsksNodeToRetryNow(t *testing.T) {
	c := start(t, "android")
	c.call("POST", "/reconnect", "wrong", nil, http.StatusUnauthorized, nil)
	c.do("POST", "/reconnect", nil, http.StatusNoContent, nil)

	var events []struct{ Kind, Message string }
	c.do("GET", "/diag", nil, http.StatusOK, &events)
	for _, e := range events {
		if e.Kind == "connection" && strings.HasPrefix(e.Message, "reconnect, requested by the app") {
			return
		}
	}
	t.Fatalf("events %+v, want the reconnect requested by the app", events)
}
