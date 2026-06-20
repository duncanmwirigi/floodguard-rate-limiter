// Command example demonstrates floodguard protecting a fake POST /withdraw
// endpoint. See README.md for test scenarios and testdata/.
package main

import (
	"context"
	"log"
	"net/http"

	"github.com/redis/go-redis/v9"
	"github.com/ultimateprogrammer/floodguard/config"
	"github.com/ultimateprogrammer/floodguard/example/app"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	client := redis.NewClient(&redis.Options{Addr: cfg.Redis.Addr})
	ctx, cancel := context.WithTimeout(context.Background(), cfg.Redis.PingTimeout)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		log.Fatalf("cannot reach Redis at %s: %v\nStart redis-server or set REDIS_ADDR", cfg.Redis.Addr, err)
	}
	log.Printf("connected to Redis at %s (prefix=%s)", cfg.Redis.Addr, cfg.Redis.KeyPrefix)

	srv, err := app.New(cfg, client)
	if err != nil {
		log.Fatal(err)
	}

	log.Printf("floodguard example listening on %s", cfg.Server.ListenAddr)
	log.Printf("demo routes: POST /withdraw  POST /demo/trust-device  GET /demo/balance  GET /health")
	if err := http.ListenAndServe(cfg.Server.ListenAddr, srv.Mux); err != nil {
		log.Fatal(err)
	}
}
