package webclient

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

// FetchBodyAbs は絶対 URL を GET してボディを返す。
func (c *Client) FetchBodyAbs(rawURL string) ([]byte, error) {
	resp, err := c.FetchURL(rawURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// FetchBodyAbsCT は絶対 URL を GET してボディとコンテンツタイプを返す。
func (c *Client) FetchBodyAbsCT(rawURL string) ([]byte, string, error) {
	resp, err := c.FetchURL(rawURL)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return body, resp.Header.Get("Content-Type"), err
}

// PostFormAbs は絶対 URL に form データを POST してボディとコンテンツタイプを返す。
func (c *Client) PostFormAbs(rawURL string, vals url.Values) ([]byte, string, error) {
	body, ct, _, err := c.PostFormAbsFull(rawURL, vals)
	return body, ct, err
}

// PostFormAbsFull は PostFormAbs にリダイレクト後の最終 URL を加えて返す。
func (c *Client) PostFormAbsFull(rawURL string, vals url.Values) ([]byte, string, string, error) {
	req, err := http.NewRequest("POST", rawURL, bytes.NewBufferString(vals.Encode()))
	if err != nil {
		return nil, "", "", fmt.Errorf("POST リクエスト作成: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Referer", rawURL)
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("POST %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}
	return body, resp.Header.Get("Content-Type"), finalURL, err
}

// PostRawAbs は任意の Content-Type のボディをそのまま POST する（multipart 転送等）。
func (c *Client) PostRawAbs(rawURL string, body io.Reader, contentType, referer string) ([]byte, string, error) {
	req, err := http.NewRequest("POST", rawURL, body)
	if err != nil {
		return nil, "", fmt.Errorf("POST リクエスト作成: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("POST %s: %w", rawURL, err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	return respBody, resp.Header.Get("Content-Type"), err
}
