package main

import (
	"github.com/alecthomas/kong"
	"go.uber.org/zap"
)

var cli struct {
	Serve ServeCmd `cmd:"" default:"1" help:"Run the server part of grokki."`
}

func main() {
	initLogger()

	ctx := kong.Parse(&cli)
	if err := ctx.Run(); err != nil {
		zap.L().Fatal("Cannot run command.", zap.Error(err))
	}
}

func initLogger() {
	logger, err := zap.NewDevelopment()
	if err != nil {
		panic(err)
	}
	zap.ReplaceGlobals(logger)
}
