package academic

import (
	"encoding/json"
	"log"
	"time"
)

// PublicTTL は保存した公開情報（シラバス・検索の選択肢）を取り直さずに使う期間。
// 教務システムのシラバスには更新日時が無いため、期間で区切る。
const PublicTTL = 24 * time.Hour

// loadPublic は保存した公開情報を v に読み込む。無いか PublicTTL を過ぎていれば false。
func (s *Service) loadPublic(key string, v any) bool {
	if s.Public == nil {
		return false
	}
	data, _, _, at, ok := s.Public.LoadPublic(key)
	if !ok || time.Since(at) > PublicTTL {
		return false
	}
	return json.Unmarshal(data, v) == nil
}

// savePublic は公開情報を保存する。ログイン不要で誰でも見られる情報だけを渡すこと。
func (s *Service) savePublic(key string, v any) {
	if s.Public == nil {
		return
	}
	data, err := json.Marshal(v)
	if err == nil {
		err = s.Public.SavePublic(key, data, "", "")
	}
	if err != nil {
		log.Printf("公開情報の保存に失敗 (%s): %v", key, err)
	}
}
