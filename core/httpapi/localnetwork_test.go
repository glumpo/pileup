package httpapi_test

import (
	"net/http"
	"testing"

	"github.com/glumpo/pileup/core/invite"
)

func TestLocalNetworkDenial(t *testing.T) {
	a := start(t, "android")
	p := a.create("Грузия", "Аня")
	i, err := invite.Parse(a.invite(p))
	if err != nil {
		t.Fatal(err)
	}
	i.Secret = make([]byte, 32)
	b, r := startIn(t, "ios")

	var out struct{ Denied bool }
	b.do("GET", "/local-network", nil, http.StatusOK, &out)
	if out.Denied {
		t.Fatal("denied before the platform said so")
	}
	b.joinFails(i.String(), http.StatusBadGateway, "inviter_unreachable")

	r.handler(t, "_pileup._tcp").Denied(true)
	b.do("GET", "/local-network", nil, http.StatusOK, &out)
	if !out.Denied {
		t.Fatal("not denied after the platform reported a denial")
	}
	b.joinFails(i.String(), http.StatusBadGateway, "local_network_denied")

	r.handler(t, "_airplay._tcp").Denied(false)
	b.do("GET", "/local-network", nil, http.StatusOK, &out)
	if out.Denied {
		t.Fatal("still denied after the platform reported access")
	}
}
