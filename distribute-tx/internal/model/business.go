package model

import (
	"gorm.io/gorm"
)

// Account 表示用户账户，通常存储在独立的账户服务数据库中
type Account struct {
	gorm.Model
	UserID      string  `gorm:"column:user_id;type:varchar(64);index"` // 用户ID
	Balance     float64 `gorm:"column:balance;type:decimal(10,2)"`     // 账户余额
	AccountType string  `gorm:"column:account_type;type:varchar(20)"`  // 账户类型：储蓄、支票等
	Status      string  `gorm:"column:status;type:varchar(20)"`        // 账户状态：正常、冻结等
}

// TableName 定义账户表名
func (Account) TableName() string {
	return "accounts"
}

// Order 表示订单信息，通常存储在独立的订单服务数据库中
type Order struct {
	gorm.Model
	OrderNo     string  `gorm:"column:order_no;type:varchar(64);uniqueIndex"` // 订单号
	UserID      string  `gorm:"column:user_id;type:varchar(64);index"`        // 用户ID
	TotalAmount float64 `gorm:"column:total_amount;type:decimal(10,2)"`       // 订单总金额
	Status      string  `gorm:"column:status;type:varchar(20)"`               // 订单状态：待付款、已付款、已发货等
}

// TableName 定义订单表名
func (Order) TableName() string {
	return "orders"
}

// OrderItem 表示订单项，关联到具体订单
type OrderItem struct {
	gorm.Model
	OrderNo   string  `gorm:"column:order_no;type:varchar(64);index"` // 关联的订单号
	ProductID string  `gorm:"column:product_id;type:varchar(64)"`     // 商品ID
	Quantity  int     `gorm:"column:quantity"`                        // 购买数量
	UnitPrice float64 `gorm:"column:unit_price;type:decimal(10,2)"`   // 商品单价
}

// TableName 定义订单项表名
func (OrderItem) TableName() string {
	return "order_items"
}

// Inventory 表示商品库存，通常存储在独立的库存服务数据库中
type Inventory struct {
	gorm.Model
	ProductID   string `gorm:"column:product_id;type:varchar(64);uniqueIndex"` // 商品ID
	ProductName string `gorm:"column:product_name;type:varchar(100)"`          // 商品名称
	Quantity    int    `gorm:"column:quantity"`                                // 可用库存数量
	Reserved    int    `gorm:"column:reserved"`                                // 已预留数量
}

// TableName 定义库存表名
func (Inventory) TableName() string {
	return "inventory"
}

// PaymentRecord 表示支付记录，通常存储在独立的支付服务数据库中
type PaymentRecord struct {
	gorm.Model
	PaymentID string  `gorm:"column:payment_id;type:varchar(64);uniqueIndex"` // 支付ID
	OrderNo   string  `gorm:"column:order_no;type:varchar(64);index"`         // 订单号
	UserID    string  `gorm:"column:user_id;type:varchar(64);index"`          // 用户ID
	Amount    float64 `gorm:"column:amount;type:decimal(10,2)"`               // 支付金额
	Status    string  `gorm:"column:status;type:varchar(20)"`                 // 支付状态：处理中、成功、失败
}

// TableName 定义支付记录表名
func (PaymentRecord) TableName() string {
	return "payment_records"
}
