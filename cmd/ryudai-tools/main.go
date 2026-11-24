package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/ieyoukan/ryudai-tools/internal/academic"
	"github.com/ieyoukan/ryudai-tools/internal/config"
	appcrypto "github.com/ieyoukan/ryudai-tools/internal/crypto"
	"github.com/ieyoukan/ryudai-tools/internal/db"
	"github.com/ieyoukan/ryudai-tools/internal/handler"
	"github.com/ieyoukan/ryudai-tools/internal/mcp"
	"github.com/ieyoukan/ryudai-tools/internal/middleware"
	"github.com/ieyoukan/ryudai-tools/internal/oauth"
	"github.com/ieyoukan/ryudai-tools/internal/session"
	"github.com/ieyoukan/ryudai-tools/internal/ui"
	"github.com/ieyoukan/ryudai-tools/internal/webapp"
)

// version はリリースビルド時に -ldflags "-X main.version=..." で埋め込まれる。
var version = "dev"

func main() {
	log.Printf("Ryudai Tools version %s", version)
	cfg := config.Load()

	// --- 認証情報暗号化鍵のロード ---
	dataDir := filepath.Dir(cfg.DBPath)
	credKey, err := appcrypto.LoadOrGenerateKey(dataDir)
	if err != nil {
		log.Fatalf("認証情報暗号化鍵のロード失敗: %v", err)
	}
	cfg.CredentialKey = credKey

	// --- DB & セッションストア ---
	var store session.Store
	var database *db.DB
	if cfg.DBPath != "" {
		database, err = db.Open(cfg.DBPath)
		if err != nil {
			log.Printf("DB オープン失敗（インメモリにフォールバック）: %v", err)
			store = session.NewMemStore()
		} else {
			database.SetCookieKey(cfg.CredentialKey)
			store = database.NewSessionStore()
			log.Printf("DB: %s", cfg.DBPath)
		}
	} else {
		store = session.NewMemStore()
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// --- 教務システム（シラバス・履修） ---
	// 教務システムはログイン時の Cookie で使う。切れたら再ログインできるのは .env の本人だけ。
	var tokens middleware.TokenResolver
	if database != nil {
		tokens = database
	}
	academicSvc := academic.New(cfg.KyomuBaseURL, func(username string) (string, bool) {
		if cfg.WebClassUsername == "" || cfg.WebClassPassword == "" || !strings.EqualFold(username, cfg.WebClassUsername) {
			return "", false
		}
		return cfg.WebClassPassword, true
	})
	academicSvc.PeriodStarts = cfg.PeriodTimes
	if database != nil {
		academicSvc.Cookies = database
		academicSvc.Plans = database
		academicSvc.Categories = database
	}

	// --- HTTP ハンドラー ---
	apiAuth := &handler.AuthHandler{Config: cfg, SessionStore: store, Academic: academicSvc}
	apiFiles := &handler.FilesHandler{Config: cfg}
	apiAcademic := &handler.AcademicHandler{
		Svc:         academicSvc,
		User:        func(r *http.Request) string { return middleware.RequestUser(r, store, tokens) },
		SessionUser: func(r *http.Request) string { return middleware.SessionUser(r, store) },
	}
	if database != nil {
		apiAcademic.Plans = database
	}
	uiHandler := ui.NewHandler(cfg, store, database, academicSvc)
	apiFiles.Client = uiHandler.WebClassClient

	// --- OAuth（ChatGPT / claude.ai のアカウント連携） ---
	var oauthSrv *oauth.Server
	if database != nil {
		oauthSrv = &oauth.Server{
			Store:         database,
			PublicBaseURL: cfg.PublicBaseURL,
			CurrentUser:   uiHandler.CurrentUser,
			Key:           cfg.CredentialKey,
		}
		go func() {
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					if err := database.PurgeExpiredOAuth(); err != nil {
						log.Printf("期限切れ OAuth データの削除に失敗: %v", err)
					}
				}
			}
		}()
	}

	// MCP は Cookie を受け付けず Bearer トークンのみ（他サイトからの CSRF を避ける）。
	// OAuth のアクセストークン（ChatGPT 等）と、設定画面で発行する API トークン（Codex 等）の両方を受け付ける。
	mcpAuth := func(r *http.Request) (string, error) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			return "", nil
		}
		token := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if oauthSrv != nil && oauth.IsAccessToken(token) {
			return oauthSrv.VerifyAccessToken(r, token)
		}
		if tokens != nil {
			if user, err := tokens.UserForAPIToken(token); err == nil {
				return user, nil
			}
		}
		return "", oauth.ErrInvalidToken
	}
	var mcpChallenge func(*http.Request) string
	if oauthSrv != nil {
		mcpChallenge = oauthSrv.Challenge
	}
	mcpServer := mcp.NewAcademicServer(academicSvc, version, mcpAuth, mcpChallenge)

	r := chi.NewRouter()
	r.Use(chimw.Logger)
	r.Use(chimw.Recoverer)
	r.Use(chimw.RealIP)
	r.Use(middleware.RedirectLegacyHosts(cfg.PublicBaseURL, cfg.LegacyHosts))
	r.Use(middleware.RefreshSession(store))

	// 画面（web/ の React をビルドしてバイナリに埋め込んだもの）。ページの URL にはどれも index.html を返し、
	// 中身は画面が /api/page/...（ページのデータ）と /api/me/...（教務システム）から読む。
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/courses", http.StatusSeeOther)
	})
	page := webapp.Index()
	for _, p := range []string{
		"/login", "/terms", "/privacy", "/downloads",
		"/courses", "/courses/{courseID}", "/courses/{courseID}/contents/{contentID}", "/messages", "/settings",
		"/registration", "/registration/candidates", "/grades", "/plan", "/syllabus", "/syllabus/{year}/{code}",
		"/lectures/{year}/{code}",
	} {
		r.Get(p, page.ServeHTTP)
	}
	r.Mount("/api/page", uiHandler.PageAPI())
	files := webapp.Files()
	r.Handle("/assets/*", files)
	r.Handle("/brand/*", files)
	for _, f := range []string{"/favicon.ico", "/favicon.svg", "/apple-touch-icon.png"} {
		r.Get(f, files.ServeHTTP)
	}
	// 以前の URL
	r.Get("/register", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/login", http.StatusMovedPermanently)
	})
	r.Get("/app/*", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, strings.TrimPrefix(r.URL.Path, "/app"), http.StatusMovedPermanently)
	})
	r.Post("/logout", uiHandler.HandleLogout)

	// ファイルのダウンロードなど、画面ではない URL
	// 旧配布ファイルのリンクからは現在の配布案内へ移動する。
	r.Get("/downloads/{name}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/downloads", http.StatusMovedPermanently)
	})
	r.Get("/courses/{courseID}/files.json", uiHandler.CourseFilesJSON)
	r.Get("/courses/{courseID}/files.zip", uiHandler.DownloadCourseZip)
	r.Get("/courses/{courseID}/summary", uiHandler.CourseSummary)
	r.Get("/download", uiHandler.Download)

	// MCP（Streamable HTTP）と OAuth
	r.Handle("/mcp", mcpServer)
	if oauthSrv != nil {
		oauthSrv.Routes(r)
	}

	// API ルート
	r.Group(func(r chi.Router) {
		r.Use(cors.Handler(cors.Options{
			AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Content-Type", "X-Session-ID", "Authorization"},
			ExposedHeaders:   []string{"Content-Disposition"},
			AllowCredentials: true,
			MaxAge:           300,
		}))

		r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
			// version はデプロイ後の確認（mise run smoke）で、動いている版が手元と同じかを見るのに使う
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			json.NewEncoder(w).Encode(map[string]string{"status": "ok", "version": version})
		})

		r.Post("/api/auth/login", apiAuth.Login)
		r.Post("/api/auth/logout", apiAuth.Logout)
		r.Get("/api/auth/session", apiAuth.Session)

		// 教務システム: シラバスは公開、/api/me は Bearer トークンまたはセッションで認証
		r.Get("/api/academic/calendar", apiAcademic.Calendar)
		r.Get("/api/academic/timetable", apiAcademic.Timetable)
		r.Get("/api/academic/rules", apiAcademic.StudyRules)
		r.Get("/api/academic/required", apiAcademic.RequiredCourses)
		r.Get("/api/me/required", apiAcademic.MyRequiredCourses)
		r.Get("/api/syllabus/search", apiAcademic.SearchSyllabus)
		r.Get("/api/syllabus/options", apiAcademic.SyllabusOptions)
		r.Get("/api/syllabus/{year}/{code}", apiAcademic.Syllabus)
		r.Get("/api/me/registration", apiAcademic.MyRegistration)
		r.Get("/api/me/schedule", apiAcademic.MySchedule)
		r.Get("/api/me/lectures/{year}/{code}", apiAcademic.MyLecture)
		r.Get("/api/me/candidates", apiAcademic.MyCandidates)
		r.Post("/api/me/plan", apiAcademic.MyPlan)
		r.Get("/api/me/graduation", apiAcademic.MyGraduation)
		r.Get("/api/me/plan/suggestions", apiAcademic.PlanSuggestions)
		r.Post("/api/me/plan/items", apiAcademic.AddPlanItem)
		r.Delete("/api/me/plan/items", apiAcademic.DeletePlan)
		r.Delete("/api/me/plan/items/{id}", apiAcademic.DeletePlanItem)
		r.Put("/api/me/lecture-categories", apiAcademic.SaveLectureCategories)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireSession(store))
			r.Get("/api/courses", apiFiles.ListCourses)
			r.Get("/api/courses/{courseID}/contents", apiFiles.ListCourseContents)
			r.Get("/api/courses/{courseID}/contents/{contentID}/files", apiFiles.ListContentFiles)
			r.Get("/api/files/{courseID}/{fileID}", apiFiles.Download)
		})
	})

	// 存在しないページ: 画面の「見つかりません」を出す（API は JSON の 404）
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		webapp.NotFound().ServeHTTP(w, req)
	})

	log.Printf("Ryudai Tools starting on :%s", cfg.Port)
	log.Printf("WebClass: %s", cfg.WebClassBaseURL)
	log.Printf("教務システム: %s", cfg.KyomuBaseURL)

	srv := &http.Server{Addr: ":" + cfg.Port, Handler: r}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}
