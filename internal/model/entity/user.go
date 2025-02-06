// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"github.com/gogf/gf/v2/os/gtime"
)

// User is the golang structure for table user.
type User struct {
	Id         uint        `json:"id"         ` // 用户id
	Address    string      `json:"address"    ` // 用户address
	ApiStatus  uint        `json:"apiStatus"  ` // api的可用状态：不可用2
	ApiKey     string      `json:"apiKey"     ` // 用户币安apikey
	ApiSecret  string      `json:"apiSecret"  ` // 用户币安apisecret
	OpenStatus int         `json:"openStatus" ` //
	CreatedAt  *gtime.Time `json:"createdAt"  ` //
	UpdatedAt  *gtime.Time `json:"updatedAt"  ` //
	NeedInit   int         `json:"needInit"   ` //
	Num        float64     `json:"num"        ` //
	Plat       string      `json:"plat"       ` //
	Dai        int         `json:"dai"        ` //
	Ip         string      `json:"ip"         ` //
}
