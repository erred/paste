package main

import (
	"net/http"

	"go.seankhliao.com/paste/server"
	"go.seankhliao.com/svcrunner"
)

func main() {
	hs := &http.Server{}
	svr := server.New(hs)
	svcrunner.Options{}.Run(
		svcrunner.NewHTTP(hs, svr.Register, svr.Init),
	)
}
