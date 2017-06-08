package test

import (
	"fmt"
	"io"
	"net/http"

	"github.com/cf-bigip-ctlr/route"
	"github.com/cf-bigip-ctlr/test/common"

	"github.com/nats-io/nats"
)

func NewGreetApp(urls []route.Uri, mbusClient *nats.Conn, tags map[string]string) *common.TestApp {
	app := common.NewTestApp(urls, mbusClient, tags, "")
	app.AddHandler("/", greetHandler)
	app.AddHandler("/forwardedprotoheader", headerHandler)

	return app
}

func headerHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, fmt.Sprintf("%+v", r.Header.Get("X-Forwarded-Proto")))
}
func greetHandler(w http.ResponseWriter, r *http.Request) {
	io.WriteString(w, "Hello, world")
}
