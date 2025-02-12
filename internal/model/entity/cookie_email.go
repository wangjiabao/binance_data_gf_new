// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package entity

import (
	"github.com/gogf/gf/v2/os/gtime"
)

// CookieEmail is the golang structure for table cookie_email.
type CookieEmail struct {
	Id        uint        `json:"id"        ` //
	Status    uint        `json:"status"    ` //
	Email     string      `json:"email"     ` //
	CreatedAt *gtime.Time `json:"createdAt" ` //
	UpdatedAt *gtime.Time `json:"updatedAt" ` //
}
