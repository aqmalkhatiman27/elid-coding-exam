package main

import (
  "log"
  "net/http"
  dbpkg "elid-coding-exam/backend/internal/db"
  httpx "elid-coding-exam/backend/internal/http"
)

func main() {
  pool := dbpkg.MustConnect()
  srv := httpx.NewServer(pool)
  log.Println("listening on :8080")
  log.Fatal(http.ListenAndServe(":8080", srv.R))
}
