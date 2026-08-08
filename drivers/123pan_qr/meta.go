package _123pan_qr

import (
	"github.com/OpenListTeam/OpenList/v4/internal/driver"
	"github.com/OpenListTeam/OpenList/v4/internal/op"
)

type AdditionQR struct {
	// Authorization: 完整的 Authorization header，如 "Bearer eyJhbGciOiJIUzI1NiIs..."
	// 与 Token 二选一填写即可
	Authorization string `json:"authorization" type:"text" help:"从扫码登录获取的完整 Authorization header，例如 Bearer eyJ..."`
	// Token: JWT token（不含 Bearer 前缀），与 Authorization 二选一
	Token         string `json:"token" required:"true" type:"text" help:"JWT token，不含 Bearer 前缀，与 authorization 二选一"`
	driver.RootID
}

var config = driver.Config{
	Name:        "123Pan QR",
	DefaultRoot: "0",
	LocalSort:   true,
	PreferProxy: true,
}

func init() {
	op.RegisterDriver(func() driver.Driver {
		return &Pan123QR{}
	})
}
