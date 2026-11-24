package config

import (
	"bufio"
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port            string
	WebClassBaseURL string
	// KyomuBaseURL は教務情報システム（TIGLON）のポータル URL。
	KyomuBaseURL string
	// PublicBaseURL は外部から見たこのアプリの URL。
	// OAuth の issuer と MCP の resource に使う。空ならリクエストの Host から組み立てる。
	PublicBaseURL string
	// LegacyHosts は旧ドメイン。ここへのアクセスは PublicBaseURL へ 308 で転送する。
	LegacyHosts []string
	DBPath      string
	// CredentialKey は保存する Cookie と OAuth の秘密情報の AES-256-GCM 暗号化に使う 32 バイト鍵（パスワードは保存しない）。
	// crypto.LoadOrGenerateKey で設定される。
	CredentialKey []byte
	// 時限開始時刻マップ: period番号 → "HH:MM"
	// デフォルトは琉球大学の標準時程
	PeriodTimes map[int]string

	// オプション。自分のアカウントのセッションを再ログインで更新する。
	WebClassUsername string
	WebClassPassword string
}

func Load() *Config {
	cfg := &Config{
		Port:            "8080",
		WebClassBaseURL: "https://webclass.cc.u-ryukyu.ac.jp/webclass",
		KyomuBaseURL:    "https://tiglon.jim.u-ryukyu.ac.jp/portal",
		DBPath:          "./data/daihack.db",
		PeriodTimes: map[int]string{
			1: "08:30",
			2: "10:20",
			3: "12:50",
			4: "14:40",
			5: "16:20",
			6: "18:00",
			7: "19:40",
			8: "21:10",
		},
	}

	for _, p := range envFilePaths() {
		if err := loadEnvFile(cfg, p); err == nil {
			log.Printf("config loaded from %s", p)
			break
		}
	}

	// 実際の環境変数は .env ファイルより優先
	applyEnvVars(cfg)
	return cfg
}

func envFilePaths() []string {
	paths := []string{"/etc/daihack.env"}
	if home, err := os.UserHomeDir(); err == nil {
		paths = append(paths, home+"/.config/daihack/.env")
	}
	return append(paths, ".env")
}

func applyEnvVars(cfg *Config) {
	if v := os.Getenv("PORT"); v != "" {
		cfg.Port = v
	}
	if v := os.Getenv("WEBCLASS_BASE_URL"); v != "" {
		cfg.WebClassBaseURL = v
	}
	if v := os.Getenv("KYOMU_BASE_URL"); v != "" {
		cfg.KyomuBaseURL = v
	}
	if v := os.Getenv("PUBLIC_BASE_URL"); v != "" {
		cfg.PublicBaseURL = strings.TrimRight(v, "/")
	}
	if v := os.Getenv("LEGACY_HOSTS"); v != "" {
		cfg.LegacyHosts = splitHosts(v)
	}
	if v := os.Getenv("DB_PATH"); v != "" {
		cfg.DBPath = v
	}
	if v := os.Getenv("WEBCLASS_USERNAME"); v != "" {
		cfg.WebClassUsername = v
	}
	if v := os.Getenv("WEBCLASS_PASSWORD"); v != "" {
		cfg.WebClassPassword = v
	}
	for i := 1; i <= 8; i++ {
		key := "PERIOD_" + strconv.Itoa(i)
		if v := os.Getenv(key); v != "" {
			cfg.PeriodTimes[i] = v
		}
	}
}

// loadEnvFile は KEY=VALUE 形式の .env ファイルを読み込む。
func loadEnvFile(cfg *Config, path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.ToUpper(strings.TrimSpace(key))
		val = strings.Trim(strings.TrimSpace(val), `"'`)

		switch key {
		case "PORT":
			cfg.Port = val
		case "WEBCLASS_BASE_URL":
			cfg.WebClassBaseURL = val
		case "KYOMU_BASE_URL":
			cfg.KyomuBaseURL = val
		case "PUBLIC_BASE_URL":
			cfg.PublicBaseURL = strings.TrimRight(val, "/")
		case "LEGACY_HOSTS":
			cfg.LegacyHosts = splitHosts(val)
		case "DB_PATH":
			cfg.DBPath = val
		case "WEBCLASS_USERNAME":
			cfg.WebClassUsername = val
		case "WEBCLASS_PASSWORD":
			cfg.WebClassPassword = val
		default:
			// PERIOD_1 〜 PERIOD_8
			if strings.HasPrefix(key, "PERIOD_") {
				if n, err := strconv.Atoi(strings.TrimPrefix(key, "PERIOD_")); err == nil {
					cfg.PeriodTimes[n] = val
				}
			}
		}
	}
	return scanner.Err()
}

// splitHosts はカンマ区切りのホスト名を小文字にして返す。
func splitHosts(v string) []string {
	var hosts []string
	for _, h := range strings.Split(v, ",") {
		if h = strings.ToLower(strings.TrimSpace(h)); h != "" {
			hosts = append(hosts, h)
		}
	}
	return hosts
}
