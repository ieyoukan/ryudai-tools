package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// Encrypt は AES-256-GCM で平文を暗号化し base64 文字列を返す。
func Encrypt(key []byte, plaintext string) (string, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// Decrypt は AES-256-GCM で暗号化された base64 文字列を復号する。
func Decrypt(key []byte, encoded string) (string, error) {
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("gcm: %w", err)
	}
	if len(data) < gcm.NonceSize() {
		return "", fmt.Errorf("暗号文が短すぎます")
	}
	nonce, ct := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("復号失敗: %w", err)
	}
	return string(pt), nil
}

// LoadOrGenerateKey は以下の順で 32 バイトの暗号鍵を返す。
//  1. 環境変数 CREDENTIAL_KEY（64 文字の hex）
//  2. dataDir/.cred_key ファイル
//  3. 自動生成して dataDir/.cred_key に保存
func LoadOrGenerateKey(dataDir string) ([]byte, error) {
	if hexKey := os.Getenv("CREDENTIAL_KEY"); hexKey != "" {
		key, err := hex.DecodeString(strings.TrimSpace(hexKey))
		if err != nil || len(key) != 32 {
			return nil, fmt.Errorf("CREDENTIAL_KEY は 64 文字の hex 文字列（32 バイト）にしてください")
		}
		return key, nil
	}

	keyPath := filepath.Join(dataDir, ".cred_key")

	if data, err := os.ReadFile(keyPath); err == nil {
		key, err := hex.DecodeString(strings.TrimSpace(string(data)))
		if err == nil && len(key) == 32 {
			return key, nil
		}
	}

	key := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("鍵生成: %w", err)
	}

	if err := os.MkdirAll(dataDir, 0700); err == nil {
		if err := os.WriteFile(keyPath, []byte(hex.EncodeToString(key)), 0600); err != nil {
			log.Printf("警告: 暗号鍵の保存に失敗しました（再起動後に認証情報が使えなくなります）: %v", err)
		} else {
			log.Printf("認証情報暗号鍵を生成・保存しました: %s", keyPath)
		}
	}

	return key, nil
}
