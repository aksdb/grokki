package main

import (
	crand "crypto/rand"
	"encoding/binary"
	"github.com/alecthomas/kong"
	"go.uber.org/zap"
	"math/rand"
)

var cli struct {
	Serve ServeCmd `cmd:"" default:"1" help:"Run the server part of grokki."`
}

func main() {
	initLogger()
	initRandom()

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

func initRandom() {
	var seed int64
	if err := binary.Read(crand.Reader, binary.BigEndian, &seed); err != nil {
		zap.L().Fatal("Cannot read from random.", zap.Error(err))
	}
	rand.Seed(seed)
}
