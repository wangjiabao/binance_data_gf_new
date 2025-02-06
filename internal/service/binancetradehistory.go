// ================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// You can delete these comments if you wish manually maintain this interface file.
// ================================================================================

package service

import (
	"context"
)

type (
	IBinanceTraderHistory interface {
		// UpdateProxyIp ip更新
		UpdateProxyIp(ctx context.Context) (err error)
		// PullAndOrder 拉取binance数据
		PullAndOrder(ctx context.Context, traderNum uint64) (err error)
		// PullAndOrderNew 拉取binance数据，仓位，根据cookie
		PullAndOrderNew(ctx context.Context, traderNum uint64, ipProxyUse int) (err error)
		// GetGlobalInfo 获取全局测试数据
		GetGlobalInfo(ctx context.Context)
		// UpdateCoinInfo 初始化信息
		UpdateCoinInfo(ctx context.Context) bool
		// UpdateKeyPosition 更新keyPosition信息
		UpdateKeyPosition(ctx context.Context) bool
		// InitGlobalInfo 初始化信息
		InitGlobalInfo(ctx context.Context) bool
		// PullAndSetBaseMoneyNewGuiTuAndUser 拉取binance保证金数据
		PullAndSetBaseMoneyNewGuiTuAndUser(ctx context.Context)
		// InsertGlobalUsers  新增用户
		InsertGlobalUsers(ctx context.Context)
		// PullAndOrderNewGuiTu 拉取binance数据，仓位，根据cookie
		PullAndOrderNewGuiTu(ctx context.Context)
		// GetSystemUserNum get user num
		GetSystemUserNum(ctx context.Context) map[string]float64
		// CreateUser set user num
		CreateUser(ctx context.Context, address, apiKey, apiSecret, plat string, needInit uint64) error
		// SetSystemUserNum set user num
		SetSystemUserNum(ctx context.Context, apiKey string, num float64) error
		// SetApiStatus set user api status
		SetApiStatus(ctx context.Context, apiKey string, num uint64) uint64
		// SetUseNewSystem set user num
		SetUseNewSystem(ctx context.Context, apiKey string, useNewSystem uint64) error
		// GetSystemUserPositions get user positions
		GetSystemUserPositions(ctx context.Context, apiKey string) map[string]float64
		// SetSystemUserPosition set user positions
		SetSystemUserPosition(ctx context.Context, system uint64, allCloseGate uint64, apiKey string, symbol string, side string, positionSide string, num float64) uint64
		// PullAndClose 拉取binance数据
		PullAndClose(ctx context.Context)
		// ListenThenOrder 监听拉取的binance数据
		ListenThenOrder(ctx context.Context)
	}
)

var (
	localBinanceTraderHistory IBinanceTraderHistory
)

func BinanceTraderHistory() IBinanceTraderHistory {
	if localBinanceTraderHistory == nil {
		panic("implement not found for interface IBinanceTraderHistory, forgot register?")
	}
	return localBinanceTraderHistory
}

func RegisterBinanceTraderHistory(i IBinanceTraderHistory) {
	localBinanceTraderHistory = i
}
