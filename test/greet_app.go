/*
 * Portions Copyright (c) 2017, F5 Networks, Inc.
 */

package test

import (
	"fmt"
	"io"
	"net/http"

	"github.com/F5Networks/cf-bigip-ctlr/route"
	"github.com/F5Networks/cf-bigip-ctlr/test/common"

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
