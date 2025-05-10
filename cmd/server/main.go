package main

import (
	"github.com/artem-burashnikov/grpc-subpub/internal/config"
	"github.com/artem-burashnikov/grpc-subpub/internal/logger"
	"github.com/artem-burashnikov/grpc-subpub/internal/server"
	"github.com/artem-burashnikov/grpc-subpub/pkg/subpub"
)

func Must[T any](obj T, err error) T {
	if err != nil {
		panic(err)
	}
	return obj
}

func main() {
	const defaultConfigPath = "config.yaml"

	cfg := Must(config.Load(defaultConfigPath))

	log := logger.NewZap(cfg.App.Environment)
	defer log.Sync()

	sp := subpub.New()

	s := server.New(cfg, log, sp)

	if err := s.Run(); err != nil {
		panic(err)
	}
}
