// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

// User is the golang structure of table user for DAO operations like Where/Data.
type User struct {
	g.Meta     `orm:"table:user, do:true"`
	Id         interface{} // 用户id
	Address    interface{} // 用户address
	ApiStatus  interface{} // api的可用状态：不可用2
	ApiKey     interface{} // 用户币安apikey
	ApiSecret  interface{} // 用户币安apisecret
	OpenStatus interface{} //
	CreatedAt  *gtime.Time //
	UpdatedAt  *gtime.Time //
	NeedInit   interface{} //
	Num        interface{} //
	Plat       interface{} //
	Dai        interface{} //
	Ip         interface{} //
}
