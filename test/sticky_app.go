package test

import (
	"fmt"
	"io"
	"net/http"

	"github.com/cf-bigip-ctlr/proxy"
	"github.com/cf-bigip-ctlr/route"
	"github.com/cf-bigip-ctlr/test/common"
	"github.com/nats-io/nats"
)

func NewStickyApp(urls []route.Uri, rPort uint16, mbusClient *nats.Conn, tags map[string]string) *common.TestApp {
	app := common.NewTestApp(urls, rPort, mbusClient, tags, "")
	app.AddHandler("/sticky", stickyHandler(app.Port()))

	return app
}

func stickyHandler(port uint16) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie := &http.Cookie{
			Name:  proxy.StickyCookieKey,
			Value: "xxx",
		}
		http.SetCookie(w, cookie)
		io.WriteString(w, fmt.Sprintf("%d", port))
	}
}
