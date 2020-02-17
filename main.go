package main

import (
	crand "crypto/rand"
	"encoding/binary"
	"github.com/alecthomas/kong"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"math/rand"
)

var cli struct {
	Debug bool `help:"Enable debug logging." short:"d"`

	Serve ServeCmd `cmd:"" default:"1" help:"Run the server part of grokki."`
}

func main() {
	ctx := kong.Parse(&cli)

	initLogger()
	initRandom()

	if err := ctx.Run(); err != nil {
		zap.L().Fatal("Cannot run command.", zap.Error(err))
	}
}

func initLogger() {
	var lc zap.Config
	if cli.Debug {
		lc = zap.NewDevelopmentConfig()
		lc.EncoderConfig.EncodeLevel = zapcore.CapitalColorLevelEncoder
	} else {
		lc = zap.NewProductionConfig()
		lc.EncoderConfig.EncodeTime = zapcore.RFC3339NanoTimeEncoder
	}

	logger, err := lc.Build()
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
