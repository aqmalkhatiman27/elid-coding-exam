package httpx

import (
  "log"
  "context"
  "math/rand"
  "net"
  "net/http"
  "os"
  "strconv"
  "strings"
  "sync"
  "time"

  "github.com/gin-gonic/gin"
  "github.com/golang-jwt/jwt/v5"
  "github.com/jackc/pgx/v5/pgxpool"
)

var deviceTypes = map[string]bool{
  "access_controller": true,
  "face_reader":       true,
  "anpr":              true,
}

type Server struct {
  R   *gin.Engine
  DB  *pgxpool.Pool
  Now func() time.Time

  mu      sync.Mutex
  running map[int]contextCancel // deviceID -> cancel
}

type contextCancel interface{ Cancel() }

type cancelWrapper struct{ cancel func() }
func (c cancelWrapper) Cancel(){ c.cancel() }

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

  s := &Server{R:r, DB:db, Now:time.Now, running: make(map[int]contextCancel)}

  r.GET("/health", func(c *gin.Context){
    c.JSON(http.StatusOK, gin.H{"ok": true, "time": s.Now().UTC()})
  })

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

// ---------- auth ----------
func (s *Server) login(c *gin.Context) {
  var req struct{ Email, Password string }
  if err := c.BindJSON(&req); err != nil || req.Email=="" || req.Password=="" {
    c.JSON(400, gin.H{"error":"invalid payload"}); return
  }
  claims := jwt.MapClaims{"sub": req.Email, "exp": time.Now().Add(8*time.Hour).Unix()}
  token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
  ss, _ := token.SignedString([]byte(os.Getenv("JWT_SECRET")))
  c.JSON(200, gin.H{"token": ss})
}

// ---------- devices ----------
func (s *Server) listDevices(c *gin.Context) {
  ctx := c.Request.Context()
  rows, err := s.DB.Query(ctx, `
    select id, name, coalesce(location,''), device_type, ip_address, status, created_at, updated_at
    from devices order by id asc`)
  if err != nil { c.JSON(500, gin.H{"error":err.Error()}); return }
  type device struct{
    ID int `json:"id"`
    Name string `json:"name"`
    Location string `json:"location"`
    DeviceType string `json:"device_type"`
    IP string `json:"ip_address"`
    Status string `json:"status"`
    CreatedAt time.Time `json:"created_at"`
    UpdatedAt time.Time `json:"updated_at"`
  }
  var out []device
  for rows.Next() {
    var d device
    if err := rows.Scan(&d.ID,&d.Name,&d.Location,&d.DeviceType,&d.IP,&d.Status,&d.CreatedAt,&d.UpdatedAt); err==nil {
      out = append(out, d)
    }
  }
  c.JSON(200, out)
}

func (s *Server) createDevice(c *gin.Context) {
  var req struct{
    Name string `json:"name"`
    Location string `json:"location"`
    DeviceType string `json:"device_type"`
    IP string `json:"ip_address"`
  }
  if err := c.BindJSON(&req); err != nil { c.JSON(400, gin.H{"error":"invalid JSON"}); return }
  if strings.TrimSpace(req.Name)=="" { c.JSON(400, gin.H{"error":"name required"}); return }
  if !deviceTypes[req.DeviceType] { c.JSON(400, gin.H{"error":"device_type must be one of access_controller, face_reader, anpr"}); return }
  if !isValidIP(req.IP) { c.JSON(400, gin.H{"error":"invalid ip_address"}); return }

  ctx := c.Request.Context()
  var id int
  err := s.DB.QueryRow(ctx, `
    insert into devices(name, location, device_type, ip_address, status, created_at, updated_at)
    values($1,$2,$3,$4,'inactive', now(), now())
    returning id`,
    req.Name, req.Location, req.DeviceType, req.IP).Scan(&id)
  if err != nil { c.JSON(500, gin.H{"error":err.Error()}); return }
  c.JSON(201, gin.H{"id": id})
}

func isValidIP(ip string) bool {
  if net.ParseIP(ip)!=nil { return true }
  // allow host-like for demo (e.g., 127.0.0.1 or simple hostnames)
  if ip=="localhost" { return true }
  return false
}

func (s *Server) toggleDevice(c *gin.Context) {
  ctx := c.Request.Context()
  id := c.Param("id")
  var before bool
  if err := s.DB.QueryRow(ctx, `select is_locked from devices where id=$1`, id).Scan(&before); err != nil {
    c.JSON(404, gin.H{"error":"not found"}); return
  }
  after := !before
  if _, err := s.DB.Exec(ctx, `update devices set is_locked=$1, updated_at=now() where id=$2`, after, id); err != nil {
    c.JSON(500, gin.H{"error":err.Error()}); return
  }
  // Also log audit into events table (legacy)
  _, _ = s.DB.Exec(ctx, `insert into events(device_id, action) values($1,$2)`, id, map[bool]string{true:"LOCKED", false:"UNLOCKED"}[after])
  c.JSON(200, gin.H{"is_locked": after})
}

// ---------- activation & transactions ----------
type stopper interface{ Stop() }

func (s *Server) activateDevice(c *gin.Context) {
  ctx := c.Request.Context()
  idStr := c.Param("id")
  id, err := strconv.Atoi(idStr)
  if err != nil { c.JSON(400, gin.H{"error":"bad id"}); return }

  // validate device exists
  var exists bool
  if err := s.DB.QueryRow(ctx, `select exists(select 1 from devices where id=$1)`, id).Scan(&exists); err != nil || !exists {
    c.JSON(404, gin.H{"error":"device not found"}); return
  }

  // set status=active
  if _, err := s.DB.Exec(ctx, `update devices set status='active', updated_at=now() where id=$1`, id); err != nil {
    c.JSON(500, gin.H{"error":err.Error()}); return
  }

  s.mu.Lock()
  if _, ok := s.running[id]; ok {
    s.mu.Unlock()
    c.JSON(200, gin.H{"status":"already_active"})
    return
  }
  // start generator with cancel
  runCtx, cancel := context.WithCancel(context.Background())
  s.running[id] = cancelWrapper{cancel: cancel}
  s.mu.Unlock()

  log.Printf("generator start for device %d", id);
  go s.generatorLoop(runCtx, id)

  c.JSON(202, gin.H{"status":"activated"})
}

func (s *Server) deactivateDevice(c *gin.Context) {
  ctx := c.Request.Context()
  idStr := c.Param("id")
  id, err := strconv.Atoi(idStr)
  if err != nil { c.JSON(400, gin.H{"error":"bad id"}); return }

  s.mu.Lock()
  canc, ok := s.running[id]
  if ok {
    canc.Cancel()
    delete(s.running, id)
  }
  s.mu.Unlock()

  // set status=inactive
  if _, err := s.DB.Exec(ctx, `update devices set status='inactive', updated_at=now() where id=$1`, id); err != nil {
    c.JSON(500, gin.H{"error":err.Error()}); return
  }

  c.JSON(200, gin.H{"status": map[bool]string{true:"deactivated", false:"not_active"}[ok]})
}

func (s *Server) generatorLoop(ctx context.Context, deviceID int) {
  rnd := rand.New(rand.NewSource(time.Now().UnixNano() + int64(deviceID)))
  usernames := []string{"alice","bob","charlie","diana","eve"}
  events := []string{"access_granted","access_denied","face_match","plate_read","door_forced"}

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
    ts := time.Now().UTC()
    entropy := rnd.Intn(1000000)

    // Insert into transactions with JSONB payload
    // Use jsonb_build_object to avoid client-side JSON marshaling types.
    // Insert into transactions with JSONB payload
    // Use jsonb_build_object to avoid client-side JSON marshaling types.
    _, err := s.DB.Exec(context.Background(),
    `insert into transactions(device_id, username, event_type, "timestamp", payload)
    values ($1, $2, $3, $4, jsonb_build_object('entropy', to_jsonb($5), 'note','simulated'))`,
      deviceID, uname, evt, ts, entropy)
    if err != nil {
      log.Printf("insert error for device %d: %v", deviceID, err)
    } else {
      log.Printf("insert ok for device %d (%s)", deviceID, evt)
    }
  }
}

func (s *Server) listTransactions(c *gin.Context) {
  ctx := c.Request.Context()
  limit := 50
  if v := c.Query("limit"); v != "" {
    if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 { limit = n }
  }
  rows, err := s.DB.Query(ctx, `
    select t.transaction_id, t.device_id, d.name, t.username, t.event_type, t.timestamp, t.payload, t.created_at
    from transactions t
    join devices d on d.id = t.device_id
    order by t.created_at desc
    limit $1`, limit)
  if err != nil { c.JSON(500, gin.H{"error":err.Error()}); return }

  type row struct{
    TransactionID string    `json:"transaction_id"`
    DeviceID      int       `json:"device_id"`
    Device        string    `json:"device"`
    Username      string    `json:"username"`
    EventType     string    `json:"event_type"`
    Timestamp     time.Time `json:"timestamp"`
    Payload       any       `json:"payload"`
    CreatedAt     time.Time `json:"created_at"`
  }
  out := []row{}
  for rows.Next() {
    var r row
    if err := rows.Scan(&r.TransactionID, &r.DeviceID, &r.Device, &r.Username, &r.EventType, &r.Timestamp, &r.Payload, &r.CreatedAt); err==nil {
      out = append(out, r)
    }
  }
  c.JSON(200, out)
}
