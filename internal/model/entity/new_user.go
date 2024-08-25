// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"github.com/gogf/gf/v2/os/gtime"
)

// NewUser is the golang structure for table new_user.
type NewUser struct {
	Id                  uint        `json:"id"                  orm:"id"                     ` // 用id
	Address             string      `json:"address"             orm:"address"                ` // 用户address
	ApiStatus           uint        `json:"apiStatus"           orm:"api_status"             ` // api的可用状态：不可用2
	ApiKey              string      `json:"apiKey"              orm:"api_key"                ` // 用户币安apikey
	ApiSecret           string      `json:"apiSecret"           orm:"api_secret"             ` // 用户币安apisecret
	BindTraderStatus    uint        `json:"bindTraderStatus"    orm:"bind_trader_status"     ` // 绑定trader状态：0未绑定，1绑定
	BindTraderStatusTfi uint        `json:"bindTraderStatusTfi" orm:"bind_trader_status_tfi" ` //
	UseNewSystem        int         `json:"useNewSystem"        orm:"use_new_system"         ` //
	IsDai               int         `json:"isDai"               orm:"is_dai"                 ` //
	CreatedAt           *gtime.Time `json:"createdAt"           orm:"created_at"             ` //
	UpdatedAt           *gtime.Time `json:"updatedAt"           orm:"updated_at"             ` //
	BinanceId           int64       `json:"binanceId"           orm:"binance_id"             ` //
	NeedInit            int         `json:"needInit"            orm:"need_init"              ` //
	Num                 float64     `json:"num"                 orm:"num"                    ` //
}
