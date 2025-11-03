package httpx

import (
  "context"
  "math/rand"
  "net/http"
  "os"
  "strconv"
  "sync"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/golang-jwt/jwt/v5"
  "github.com/jackc/pgx/v5/pgxpool"
)

type Server struct {
  R    *gin.Engine
  DB   *pgxpool.Pool
  Now  func() time.Time

  // activation control
  mu       sync.Mutex
  running  map[int]context.CancelFunc // deviceID -> cancel
}

func NewServer(db *pgxpool.Pool) *Server {
  gin.SetMode(gin.ReleaseMode)
  r := gin.Default()

  // Dev CORS for React on 5173
  r.Use(func(c *gin.Context){
    c.Writer.Header().Set("Access-Control-Allow-Origin","http://localhost:5173")
    c.Writer.Header().Set("Access-Control-Allow-Headers","Authorization, Content-Type")
    c.Writer.Header().Set("Access-Control-Allow-Methods","GET,POST,PUT,DELETE,OPTIONS")
    if c.Request.Method=="OPTIONS" { c.AbortWithStatus(204); return }
    c.Next()
  })

  s := &Server{R:r, DB:db, Now:time.Now, running: make(map[int]context.CancelFunc)}

  r.GET("/health", func(c *gin.Context){
    c.JSON(http.StatusOK, gin.H{"ok": true, "time": s.Now().UTC()})
  })

  // Minimal login -> issues a JWT (demo)
  r.POST("/auth/login", s.login)

  api := r.Group("/api", AuthRequired())
  {
    api.GET("/devices", s.listDevices)
    api.POST("/devices", s.createDevice)
    api.POST("/devices/:id/toggle", s.toggleDevice)

    api.POST("/devices/:id/activate", s.activateDevice)
    api.POST("/devices/:id/deactivate", s.deactivateDevice)
    api.GET("/transactions", s.listTransactions)
  }

  return s
}

func (s *Server) login(c *gin.Context) {
  var req struct{ Email, Password string }
  if err := c.BindJSON(&req); err != nil || req.Email=="" || req.Password=="" {
    c.JSON(400, gin.H{"error":"invalid payload"}); return
  }
  claims := jwt.MapClaims{
    "sub": req.Email,
    "exp": time.Now().Add(8 * time.Hour).Unix(),
  }
  token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
  ss, _ := token.SignedString([]byte(os.Getenv("JWT_SECRET")))
  c.JSON(200, gin.H{"token": ss})
}

func (s *Server) listDevices(c *gin.Context) {
  ctx := c.Request.Context()
  rows, err := s.DB.Query(ctx, `select id, name, coalesce(location,''), is_locked from devices order by id asc`)
  if err != nil { c.JSON(500, gin.H{"error":err.Error()}); return }
  type device struct{ ID int; Name, Location string; IsLocked bool }
  var out []device
  for rows.Next() {
    var d device
    if err := rows.Scan(&d.ID, &d.Name, &d.Location, &d.IsLocked); err == nil {
      out = append(out, d)
    }
  }
  c.JSON(200, out)
}

func (s *Server) createDevice(c *gin.Context) {
  var req struct{ Name, Location string }
  if err := c.BindJSON(&req); err != nil || req.Name=="" {
    c.JSON(400, gin.H{"error":"name required"}); return
  }
  ctx := c.Request.Context()
  var id int
  if err := s.DB.QueryRow(ctx, `insert into devices(name, location) values($1,$2) returning id`, req.Name, req.Location).Scan(&id); err != nil {
    c.JSON(500, gin.H{"error":err.Error()}); return
  }
  c.JSON(201, gin.H{"id": id})
}

func (s *Server) toggleDevice(c *gin.Context) {
  ctx := c.Request.Context()
  id := c.Param("id")
  var before bool
  if err := s.DB.QueryRow(ctx, `select is_locked from devices where id=$1`, id).Scan(&before); err != nil {
    c.JSON(404, gin.H{"error":"not found"}); return
  }
  after := !before
  if _, err := s.DB.Exec(ctx, `update devices set is_locked=$1 where id=$2`, after, id); err != nil {
    c.JSON(500, gin.H{"error":err.Error()}); return
  }
  _, _ = s.DB.Exec(ctx, `insert into events(device_id, action) values($1,$2)`, id, map[bool]string{true:"LOCKED", false:"UNLOCKED"}[after])
  c.JSON(200, gin.H{"is_locked": after})
}

// -------- Activation / Transactions --------

func (s *Server) activateDevice(c *gin.Context) {
  ctx := c.Request.Context()
  idStr := c.Param("id")
  id, err := strconv.Atoi(idStr)
  if err != nil { c.JSON(400, gin.H{"error":"bad id"}); return }

  // check device existence
  var exists bool
  if err := s.DB.QueryRow(ctx, `select exists(select 1 from devices where id=$1)`, id).Scan(&exists); err != nil || !exists {
    c.JSON(404, gin.H{"error":"device not found"}); return
  }

  s.mu.Lock()
  if _, ok := s.running[id]; ok {
    // already running -> idempotent
    s.mu.Unlock()
    c.JSON(200, gin.H{"status":"already_active"})
    return
  }
  // start generator
  runCtx, cancel := context.WithCancel(context.Background())
  s.running[id] = cancel
  s.mu.Unlock()

  go s.generatorLoop(runCtx, id)

  c.JSON(202, gin.H{"status":"activated"})
}

func (s *Server) deactivateDevice(c *gin.Context) {
  idStr := c.Param("id")
  id, err := strconv.Atoi(idStr)
  if err != nil { c.JSON(400, gin.H{"error":"bad id"}); return }

  s.mu.Lock()
  cancel, ok := s.running[id]
  if ok {
    cancel()
    delete(s.running, id)
    s.mu.Unlock()
    c.JSON(200, gin.H{"status":"deactivated"})
    return
  }
  s.mu.Unlock()
  c.JSON(200, gin.H{"status":"not_active"})
}

func (s *Server) generatorLoop(ctx context.Context, deviceID int) {
  rnd := rand.New(rand.NewSource(time.Now().UnixNano() + int64(deviceID)))
  usernames := []string{"alice","bob","charlie","diana","eve"}
  events := []string{"ACCESS_GRANTED","ACCESS_DENIED","DOOR_FORCED"}

  for {
    // random sleep 0.5s–3s
    d := time.Duration(500+rnd.Intn(2500)) * time.Millisecond
    select {
    case <-ctx.Done():
      return
    case <-time.After(d):
    }

    uname := usernames[rnd.Intn(len(usernames))]
    evt := events[rnd.Intn(len(events))]

    // Insert a transaction row
    _, _ = s.DB.Exec(context.Background(),
      `insert into events(device_id, action) values($1,$2)`, deviceID, evt)

    // (Optional) do something with uname (extend schema if needed). For now we keep action only.
    _ = uname
  }
}

func (s *Server) listTransactions(c *gin.Context) {
  ctx := c.Request.Context()
  limit := 50
  if v := c.Query("limit"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
      limit = n
    }
  }
  rows, err := s.DB.Query(ctx, `
    select e.id, e.device_id, d.name, e.action, e.created_at
    from events e
    join devices d on d.id = e.device_id
    order by e.id desc
    limit $1`, limit)
  if err != nil { c.JSON(500, gin.H{"error":err.Error()}); return }

  type row struct {
    ID        int       `json:"id"`
    DeviceID  int       `json:"device_id"`
    Device    string    `json:"device"`
    Action    string    `json:"action"`
    CreatedAt time.Time `json:"created_at"`
  }
  out := []row{}
  for rows.Next() {
    var r row
    if err := rows.Scan(&r.ID, &r.DeviceID, &r.Device, &r.Action, &r.CreatedAt); err == nil {
      out = append(out, r)
    }
  }
  c.JSON(200, out)
}
