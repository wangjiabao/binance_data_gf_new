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
		// UpdateCoinInfo 初始化信息
		UpdateCoinInfo(ctx context.Context) bool
		// PullAndSetBaseMoneyNewGuiTuAndUser 拉取binance保证金数据
		PullAndSetBaseMoneyNewGuiTuAndUser(ctx context.Context)
		// InsertGlobalUsers  新增用户
		InsertGlobalUsers(ctx context.Context)
		// PullAndOrderNewGuiTu 拉取binance数据，仓位，根据cookie
		PullAndOrderNewGuiTu(ctx context.Context)
		// CookieErrEmail email
		CookieErrEmail(ctx context.Context)
		// GetSystemUserNum get user num
		GetSystemUserNum(ctx context.Context) map[string]float64
		// CreateUser set user num
		CreateUser(ctx context.Context, address, apiKey, apiSecret, plat string, needInit uint64, num float64) error
		// SetPositionSide set position side
		SetPositionSide(apiKey, apiSecret string) (uint64, string)
		// SetSystemUserNum set user num
		SetSystemUserNum(ctx context.Context, apiKey string, num float64) error
		// SetApiStatus set user api status
		SetApiStatus(ctx context.Context, apiKey string, status uint64, init uint64) uint64
		// SetUseNewSystem set user num
		SetUseNewSystem(ctx context.Context, apiKey string, useNewSystem uint64) error
		// GetSystemUserPositions get user positions
		GetSystemUserPositions(ctx context.Context, apiKey string) map[string]float64
		// GetBinanceUserPositions get binance user positions
		GetBinanceUserPositions(ctx context.Context, apiKey string) map[string]string
		// CloseBinanceUserPositions close binance user positions
		CloseBinanceUserPositions(ctx context.Context) uint64
		// SetSystemUserPosition set user positions
		SetSystemUserPosition(ctx context.Context, system uint64, systemOrder uint64, apiKey string, symbol string, side string, positionSide string, num float64) uint64
		// SetCookie set cookie
		SetCookie(ctx context.Context, cookie, token string) int64
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
