package db

import (
  "context"
  "os"
  "time"

  "github.com/jackc/pgx/v5/pgxpool"
)

func MustConnect() *pgxpool.Pool {
  url := os.Getenv("DATABASE_URL")
  cfg, err := pgxpool.ParseConfig(url)
  if err != nil { panic(err) }
  ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
  defer cancel()
  pool, err := pgxpool.NewWithConfig(ctx, cfg)
  if err != nil { panic(err) }
  if err := pool.Ping(ctx); err != nil { panic(err) }
  return pool
}
