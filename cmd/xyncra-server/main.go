// Command xyncra-server is the entry point for the Xyncra WebSocket server.
//
// It initializes the database, message broker, and connection store, then
// starts the WebSocket server. Configuration is provided via command-line
// flags (which take precedence) or environment variables.
//
// Usage:
//
//	xyncra-server [flags]
//
// Flags:
//
//	-addr          WebSocket server listen address (default ":8080")
//	-redis-addr    Redis server address (default "localhost:6379")
//	-redis-password  Redis AUTH password (default "")
//	-redis-db      Redis database index (default 0)
//	-db-driver     Database driver: sqlite, postgres, mysql (default "sqlite")
//	-db-dsn        Database DSN / connection string (default "xyncra.db")
//	-max-conns     Max connections per user, 0 = unlimited (default 0)
//
// Environment variables (used as fallback when flags are not set):
//
//	XYNCRA_ADDR, XYNCRA_REDIS_ADDR, XYNCRA_REDIS_PASSWORD, XYNCRA_REDIS_DB,
//	XYNCRA_DB_DRIVER, XYNCRA_DB_DSN, XYNCRA_MAX_CONNS_PER_USER
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/PineappleBond/xyncra-server/internal/cleanup"
	"github.com/PineappleBond/xyncra-server/internal/handler"
	"github.com/PineappleBond/xyncra-server/internal/mq"
	"github.com/PineappleBond/xyncra-server/internal/server"
	"github.com/PineappleBond/xyncra-server/internal/store"
	"github.com/redis/go-redis/v9"
)

// Version information, injected at build time via -ldflags.
var (
	version   = "dev"
	commit    = "unknown"
	buildTime = "unknown"
)

func main() {
	// ---------------------------------------------------------------
	// Configuration
	// ---------------------------------------------------------------

	addr := flag.String("addr", envOrDefault("XYNCRA_ADDR", ":8080"),
		"WebSocket server listen address")
	redisAddr := flag.String("redis-addr", envOrDefault("XYNCRA_REDIS_ADDR", "localhost:6379"),
		"Redis server address")
	redisPassword := flag.String("redis-password", os.Getenv("XYNCRA_REDIS_PASSWORD"),
		"Redis AUTH password")
	redisDB := flag.Int("redis-db", envOrDefaultInt("XYNCRA_REDIS_DB", 0),
		"Redis database index")
	dbDriver := flag.String("db-driver", envOrDefault("XYNCRA_DB_DRIVER", "sqlite"),
		"Database driver (sqlite, postgres, mysql)")
	dbDSN := flag.String("db-dsn", envOrDefault("XYNCRA_DB_DSN", "xyncra.db"),
		"Database DSN / connection string")
	maxConns := flag.Int("max-conns", envOrDefaultInt("XYNCRA_MAX_CONNS_PER_USER", 0),
		"Max connections per user (0 = unlimited)")
	flag.Parse()

	log.Printf("starting xyncra-server %s (%s) built %s on %s", version, commit, buildTime, *addr)

	// ---------------------------------------------------------------
	// Database / Store
	// ---------------------------------------------------------------

	db, err := store.NewDatabase(store.DatabaseConfig{
		Driver: *dbDriver,
		DSN:    *dbDSN,
	})
	if err != nil {
		log.Fatalf("failed to open database: %v", err)
	}
	defer db.Close()

	dataStore := store.NewFromDatabase(db)

	// Run auto-migration to ensure all tables exist.
	migrateCtx, migrateCancel := context.WithTimeout(context.Background(), 30*time.Second)
	if err := dataStore.AutoMigrate(migrateCtx); err != nil {
		migrateCancel()
		log.Fatalf("failed to auto-migrate database: %v", err)
	}
	migrateCancel()
	log.Println("database migrated successfully")

	// ---------------------------------------------------------------
	// Redis ConnectionStore
	// ---------------------------------------------------------------

	connStore, err := server.NewRedisConnectionStore(server.RedisConnectionStoreConfig{
		Addr:                  *redisAddr,
		Password:              *redisPassword,
		DB:                    *redisDB,
		MaxConnectionsPerUser: *maxConns,
	})
	if err != nil {
		log.Fatalf("failed to create connection store: %v", err)
	}
	defer connStore.Close()

	// ---------------------------------------------------------------
	// Message Broker (Asynq over Redis)
	// ---------------------------------------------------------------

	broker, err := mq.NewAsynqBroker(mq.AsynqConfig{
		RedisAddr:     *redisAddr,
		RedisPassword: *redisPassword,
		RedisDB:       *redisDB,
	})
	if err != nil {
		log.Fatalf("failed to create broker: %v", err)
	}
	defer broker.Close()

	// ---------------------------------------------------------------
	// Node Broadcaster (cross-node message routing via Redis Pub/Sub)
	// ---------------------------------------------------------------

	// Uses a dedicated redis.Client for Pub/Sub since Pub/Sub requires
	// an exclusive connection that cannot be shared with regular commands.
	nodeBroadcasterClient := redis.NewClient(&redis.Options{
		Addr:     *redisAddr,
		Password: *redisPassword,
		DB:       *redisDB,
	})
	nodeBroadcaster := server.NewRedisNodeBroadcaster(nodeBroadcasterClient, "xyncra")
	defer nodeBroadcasterClient.Close()

	// ---------------------------------------------------------------
	// Message Handlers
	// ---------------------------------------------------------------

	msgHandler := server.NewDefaultMessageHandler()

	// ---------------------------------------------------------------
	// WebSocket Server
	// ---------------------------------------------------------------

	srv, err := server.NewWebSocketServer(
		server.WSWithAddr(*addr),
		server.WSWithConnectionStore(connStore),
		server.WSWithStore(dataStore),
		server.WSWithBroker(broker),
		server.WSWithMessageHandler(msgHandler),
		server.WSWithNodeBroadcaster(nodeBroadcaster),
	)
	if err != nil {
		log.Fatalf("failed to create websocket server: %v", err)
	}

	// Register method handlers after srv is created so that BroadcastFn
	// can reference srv.BroadcastUpdates.
	handler.RegisterAll(msgHandler, handler.Dependencies{
		ConnStore:   connStore,
		Store:       dataStore,
		Broker:      broker,
		BroadcastFn: srv.BroadcastUpdates,
	})

	// ---------------------------------------------------------------
	// Context & signal handling
	// ---------------------------------------------------------------

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		log.Printf("received signal %v, shutting down...", sig)
		cancel()
	}()

	// Start the broker worker pool in a background goroutine.
	// Tasks that are not yet handled are logged and acknowledged.
	taskHandler := mq.NewTaskHandler()
	taskHandler.Register(mq.TypeSendMessage,
		handler.NewSendMessageTaskHandler(srv.BroadcastUpdates, srv.Logger()))
	go func() {
		if err := broker.Start(ctx, taskHandler); err != nil {
			log.Printf("broker error: %v", err)
		}
	}()

	// Start the UserUpdate cleanup goroutine.
	// Periodically removes expired UserUpdate records (older than 30 days).
	// Uses default 1-hour interval per D-001 zero-config philosophy.
	cleaner := cleanup.NewUserUpdateCleaner(cleanup.Config{
		Store: dataStore.UserUpdateStore(),
	})
	go cleaner.Run(ctx)

	// ---------------------------------------------------------------
	// Run
	// ---------------------------------------------------------------

	if err := srv.Start(ctx); err != nil {
		log.Fatalf("server error: %v", err)
	}

	// ---------------------------------------------------------------
	// Graceful shutdown
	// ---------------------------------------------------------------

	stopCtx, stopCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer stopCancel()
	if err := srv.GracefulStop(stopCtx); err != nil {
		log.Printf("graceful stop error: %v", err)
	}

	log.Println("server stopped")
}

// envOrDefault returns the value of the environment variable identified by
// key, or fallback if the variable is empty or unset.
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envOrDefaultInt returns the integer value of the environment variable
// identified by key, or fallback if the variable is empty, unset, or cannot
// be parsed as an integer.
func envOrDefaultInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: invalid integer %q for env %s, using default %d\n", v, key, fallback)
		return fallback
	}
	return n
}
