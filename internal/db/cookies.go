package db

import (
	"encoding/json"
	"net/http"
	"strings"

	appcrypto "github.com/ieyoukan/ryudai-tools/internal/crypto"
)

// SetCookieKey は保存する Cookie の暗号化鍵（AES-256、32 バイト）を設定する。
// Cookie はログイン済みのセッションそのものなので、DB が漏れてもなりすましに使えないよう暗号化して保存する。
func (db *DB) SetCookieKey(key []byte) {
	if len(key) == 32 {
		db.cookieKey = key
	}
}

// encodeCookies は Cookie を JSON にし、鍵があれば暗号化した文字列を返す。
func (db *DB) encodeCookies(cookies []*http.Cookie) (string, error) {
	data, err := json.Marshal(recordsFromCookies(cookies))
	if err != nil {
		return "", err
	}
	if db.cookieKey == nil {
		return string(data), nil
	}
	return appcrypto.Encrypt(db.cookieKey, string(data))
}

// decodeCookies は encodeCookies の逆。暗号化前の旧形式（JSON のまま）も読める。
func (db *DB) decodeCookies(stored string) ([]*http.Cookie, error) {
	plain := stored
	if !strings.HasPrefix(stored, "[") && stored != "null" {
		var err error
		if plain, err = appcrypto.Decrypt(db.cookieKey, stored); err != nil {
			return nil, err
		}
	}
	var recs []cookieRecord
	if err := json.Unmarshal([]byte(plain), &recs); err != nil {
		return nil, err
	}
	return cookiesFromRecords(recs), nil
}
