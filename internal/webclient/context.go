package webclient

import (
	"context"
	"net/http"
)

// BindContext は以後の通信を ctx に結びつける。ctx が取り消されたり期限を過ぎたりすると、
// 接続・応答待ち・本文の読み取りが止まる（利用者がページを離れたら大学側への取得もやめる）。
// 呼び出し側で別の context を付けたリクエストは、その context を優先する。
func (c *Client) BindContext(ctx context.Context) {
	c.HTTPClient.Transport = &contextTransport{base: c.transport(), ctx: ctx}
}

func (c *Client) transport() http.RoundTripper {
	if c.HTTPClient.Transport != nil {
		return c.HTTPClient.Transport
	}
	return http.DefaultTransport
}

type contextTransport struct {
	base http.RoundTripper
	ctx  context.Context
}

func (t *contextTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Context() == context.Background() {
		req = req.WithContext(t.ctx)
	}
	return t.base.RoundTrip(req)
}
