// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package do

import (
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gtime"
)

// CookieEmail is the golang structure of table cookie_email for DAO operations like Where/Data.
type CookieEmail struct {
	g.Meta    `orm:"table:cookie_email, do:true"`
	Id        interface{} //
	Status    interface{} //
	Email     interface{} //
	CreatedAt *gtime.Time //
	UpdatedAt *gtime.Time //
}
