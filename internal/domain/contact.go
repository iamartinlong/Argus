package domain

import "time"

// ContactPerson 证书申请人信息（全局共享，签发云渠道证书时注入）
type ContactPerson struct {
	ID        string
	Name      string
	Email     string
	Phone     string
	CreatedAt time.Time
	UpdatedAt time.Time
}