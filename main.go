package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"shortlink/consts"
	"shortlink/lib"
	"shortlink/models"

	"gorm.io/gorm"
)

const (
	adminCookieName = "shortlink_admin"
	adminSessionTTL = 12 * time.Hour
	pageSize        = 10
	maxRequestBody  = 1 << 20
)

type application struct {
	adminUser  string
	adminPass  string
	sessionKey []byte
}

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Printf(".env の読み込みをスキップしました: %v", err)
	}
	if !lib.ConnectDB() {
		log.Fatal("データベースに接続できません")
	}

	app := &application{
		adminUser: os.Getenv("ADMIN_USERNAME"),
		adminPass: os.Getenv("ADMIN_PASSWORD"),
	}
	if app.adminUser == "" || app.adminPass == "" {
		log.Fatal("本番運用には .env の ADMIN_USERNAME と ADMIN_PASSWORD が必要です")
	}
	app.sessionKey = []byte(os.Getenv("ADMIN_SESSION_KEY"))
	if len(app.sessionKey) < 32 {
		log.Fatal("ADMIN_SESSION_KEY は32文字以上で設定してください")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /", app.home)
	mux.HandleFunc("GET /s/{code}", app.redirect)
	mux.HandleFunc("POST /api/create", app.create)
	mux.HandleFunc("GET /admin", app.adminPage)
	mux.HandleFunc("GET /admin/create", app.requireAdmin(app.adminCreatePage))
	mux.HandleFunc("POST /api/admin/login", app.adminLogin)
	mux.HandleFunc("POST /api/admin/logout", app.requireAdmin(app.adminLogout))
	mux.HandleFunc("GET /api/admin/links", app.requireAdmin(app.adminLinks))
	mux.HandleFunc("POST /api/admin/links", app.requireAdmin(app.adminCreateLink))
	mux.HandleFunc("POST /api/admin/links/{code}/ban", app.requireAdmin(app.adminBan))
	mux.HandleFunc("POST /api/admin/links/{code}/warn", app.requireAdmin(app.adminWarn))
	mux.HandleFunc("DELETE /api/admin/links/{code}", app.requireAdmin(app.adminDelete))

	server := &http.Server{
		Addr:              envOr("ADDR", ":3000"),
		Handler:           securityHeaders(mux),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	log.Printf("短縮URLサービスを起動します: %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}

func (app *application) home(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "public/index.html")
}

func (app *application) redirect(w http.ResponseWriter, r *http.Request) {
	var link models.Links
	err := lib.DB.Where("code = ?", r.PathValue("code")).First(&link).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		http.ServeFile(w, r, "public/notfound.html")
		return
	}
	if err != nil {
		http.Error(w, "サーバーエラー", http.StatusInternalServerError)
		return
	}
	if link.IsBan.Valid && link.IsBan.Bool {
		http.ServeFile(w, r, "public/ban.html")
		return
	}
	if link.IsWarn.Valid && link.IsWarn.Bool {
		http.ServeFile(w, r, "public/warn.html")
		return
	}
	http.Redirect(w, r, link.Url, http.StatusFound)
}

func (app *application) create(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var payload models.CreateShortLink
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "不正なJSON形式です")
		return
	}
	target, err := validateTargetURL(payload.Url)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	for attempt := 0; attempt < 5; attempt++ {
		code, genErr := lib.GenerateRandomString(10)
		if genErr != nil {
			writeError(w, http.StatusInternalServerError, "サーバー側でエラーが発生しました")
			return
		}
		link := models.Links{Code: code, Url: target, CreatorIP: clientIP(r)}
		if err := lib.DB.Create(&link).Error; err == nil {
			writeJSON(w, http.StatusCreated, models.CreateResponse{Status: "ok", Code: code})
			return
		} else if !strings.Contains(strings.ToLower(err.Error()), "unique") && !strings.Contains(strings.ToLower(err.Error()), "constraint") {
			log.Printf("短縮URLの保存に失敗しました: %v", err)
			writeError(w, http.StatusInternalServerError, "サーバー側でエラーが発生しました")
			return
		}
	}
	writeError(w, http.StatusServiceUnavailable, "現在混み合っています。しばらくしてからお試しください")
}

func (app *application) adminPage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, "public/admin.html")
}

func (app *application) adminCreatePage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, "public/admin-create.html")
}

func (app *application) adminCreateLink(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var payload struct {
		Code string `json:"code"`
		URL  string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "不正なJSON形式です")
		return
	}
	payload.Code = strings.TrimSpace(payload.Code)
	if !validCustomCode(payload.Code) {
		writeError(w, http.StatusBadRequest, "コードは英数字、ハイフン、アンダースコアの1〜64文字で入力してください")
		return
	}
	target, err := validateTargetURL(payload.URL)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	link := models.Links{Code: payload.Code, Url: target, CreatorIP: "admin:" + app.adminUser}
	if err := lib.DB.Create(&link).Error; err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") || strings.Contains(strings.ToLower(err.Error()), "constraint") {
			writeError(w, http.StatusConflict, "このコードはすでに使用されています")
			return
		}
		log.Printf("管理者による短縮URLの保存に失敗しました: %v", err)
		writeError(w, http.StatusInternalServerError, "短縮URLを保存できませんでした")
		return
	}
	writeJSON(w, http.StatusCreated, models.CreateResponse{Status: "ok", Code: payload.Code})
}

func validCustomCode(code string) bool {
	if len(code) == 0 || len(code) > 64 {
		return false
	}
	for _, char := range code {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') ||
			(char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func validateTargetURL(value string) (string, error) {
	parsed, err := url.ParseRequestURI(strings.TrimSpace(value))
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" || parsed.User != nil {
		return "", errors.New("http または https のURLを入力してください")
	}
	if consts.IsInclude(parsed.String()) || isPrivateHost(parsed.Hostname()) {
		return "", errors.New("禁止されたURLです")
	}
	return parsed.String(), nil
}

func (app *application) adminLogin(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var credentials struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if json.NewDecoder(r.Body).Decode(&credentials) != nil || !secureEqual(credentials.Username, app.adminUser) || !secureEqual(credentials.Password, app.adminPass) {
		writeError(w, http.StatusUnauthorized, "ユーザー名またはパスワードが違います")
		return
	}
	value := app.signSession(time.Now().Add(adminSessionTTL).Unix())
	secure := envOr("APP_ENV", "production") == "production"
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: value, Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, Expires: time.Now().Add(adminSessionTTL), MaxAge: int(adminSessionTTL.Seconds())})
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (app *application) adminLogout(w http.ResponseWriter, r *http.Request) {
	secure := envOr("APP_ENV", "production") == "production"
	http.SetCookie(w, &http.Cookie{Name: adminCookieName, Value: "", Path: "/", HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode, MaxAge: -1})
	w.WriteHeader(http.StatusNoContent)
}

func (app *application) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(adminCookieName)
		if err != nil || !app.validSession(cookie.Value) {
			writeError(w, http.StatusUnauthorized, "ログインしてください")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get("Origin") != "" && !sameOrigin(r) {
			writeError(w, http.StatusForbidden, "不正なリクエストです")
			return
		}
		next(w, r)
	}
}

func (app *application) adminLinks(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	if page > 1000000 {
		page = 1000000
	}
	query := strings.TrimSpace(r.URL.Query().Get("q"))
	base := lib.DB.Model(&models.Links{})
	if query != "" {
		pattern := "%" + strings.ReplaceAll(strings.ReplaceAll(query, "%", "\\%"), "_", "\\_") + "%"
		base = base.Where("code LIKE ? OR url LIKE ? OR creator_ip LIKE ? OR ban_reason LIKE ? OR warn_reason LIKE ?", pattern, pattern, pattern, pattern, pattern)
	}
	var total int64
	if err := base.Count(&total).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "一覧を取得できませんでした")
		return
	}
	var links []models.Links
	if err := base.Order("created_at DESC").Limit(pageSize).Offset((page - 1) * pageSize).Find(&links).Error; err != nil {
		writeError(w, http.StatusInternalServerError, "一覧を取得できませんでした")
		return
	}
	items := make([]map[string]any, 0, len(links))
	for _, link := range links {
		items = append(items, map[string]any{
			"code": link.Code, "url": link.Url, "creatorIp": link.CreatorIP,
			"createdAt": link.CreatedAt, "banned": link.IsBan.Valid && link.IsBan.Bool,
			"banReason": nullString(link.BanReason), "warned": link.IsWarn.Valid && link.IsWarn.Bool,
			"warnReason": nullString(link.WarnReason),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "page": page, "total": total, "pages": (total + pageSize - 1) / pageSize})
}

func (app *application) adminBan(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Reason string `json:"reason"`
	}
	if !decodeReason(w, r, &payload.Reason) {
		return
	}
	result := lib.DB.Model(&models.Links{}).Where("code = ?", r.PathValue("code")).Updates(map[string]any{"is_ban": true, "ban_reason": payload.Reason})
	if result.Error != nil {
		writeError(w, 500, "Banに失敗しました")
		return
	}
	if result.RowsAffected == 0 {
		writeError(w, 404, "URLが見つかりません")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (app *application) adminWarn(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Reason string `json:"reason"`
	}
	if !decodeReason(w, r, &payload.Reason) {
		return
	}
	result := lib.DB.Model(&models.Links{}).Where("code = ?", r.PathValue("code")).Updates(map[string]any{"is_warn": true, "warn_reason": payload.Reason})
	if result.Error != nil {
		writeError(w, 500, "警告に失敗しました")
		return
	}
	if result.RowsAffected == 0 {
		writeError(w, 404, "URLが見つかりません")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (app *application) adminDelete(w http.ResponseWriter, r *http.Request) {
	result := lib.DB.Where("code = ?", r.PathValue("code")).Delete(&models.Links{})
	if result.Error != nil {
		writeError(w, 500, "削除に失敗しました")
		return
	}
	if result.RowsAffected == 0 {
		writeError(w, 404, "URLが見つかりません")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeReason(w http.ResponseWriter, r *http.Request, reason *string) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var payload struct {
		Reason string `json:"reason"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, "不正なリクエストです")
		return false
	}
	*reason = strings.TrimSpace(payload.Reason)
	if *reason == "" || len(*reason) > 500 {
		writeError(w, http.StatusBadRequest, "理由を1〜500文字で入力してください")
		return false
	}
	return true
}

func (app *application) signSession(exp int64) string {
	payload := strconv.FormatInt(exp, 10)
	mac := hmac.New(sha256.New, app.sessionKey)
	mac.Write([]byte(payload))
	return payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (app *application) validSession(value string) bool {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return false
	}
	exp, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || time.Now().Unix() > exp {
		return false
	}
	want := app.signSession(exp)
	return secureEqual(value, want)
}

func secureEqual(a, b string) bool {
	return len(a) == len(b) && subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, models.ErrorResponse{Status: "error", Error: message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(value); err != nil {
		log.Printf("JSONレスポンスの書き込みに失敗しました: %v", err)
	}
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		return host
	}
	return r.RemoteAddr
}

func isPrivateHost(host string) bool {
	if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast())
}

func nullString(value sql.NullString) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func sameOrigin(r *http.Request) bool {
	origin, err := url.Parse(r.Header.Get("Origin"))
	if err != nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	return strings.EqualFold(origin.Host, r.Host) && origin.Scheme == scheme
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "no-referrer")
		next.ServeHTTP(w, r)
	})
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		value = strings.Trim(value, "\"'")
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
	return nil
}
