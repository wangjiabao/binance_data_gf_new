package logic

import (
	"binance_data_gf/internal/model/do"
	"binance_data_gf/internal/model/entity"
	"binance_data_gf/internal/service"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	bybit "github.com/bybit-exchange/bybit.go.api"
	"github.com/gogf/gf/v2/container/gmap"
	"github.com/gogf/gf/v2/container/gtype"
	"github.com/gogf/gf/v2/database/gdb"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/grpool"
	"github.com/gogf/gf/v2/os/gtime"
	"github.com/shopspring/decimal"
	"gopkg.in/gomail.v2"
	"io/ioutil"
	"log"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

type (
	sBinanceTraderHistory struct {
		// 全局存储
		pool *grpool.Pool
	}
)

func init() {
	service.RegisterBinanceTraderHistory(New())
}

func New() *sBinanceTraderHistory {
	return &sBinanceTraderHistory{
		grpool.New(),
	}
}

func IsEqual(f1, f2 float64) bool {
	if f1 > f2 {
		return f1-f2 < 0.000000001
	} else {
		return f2-f1 < 0.000000001
	}
}

// floatEqual 判断两个浮点数是否在精度范围内相等
func floatEqual(a, b, epsilon float64) bool {
	return math.Abs(a-b) <= epsilon
}

func lessThanOrEqualZero(a, b float64, epsilon float64) bool {
	return a-b < epsilon || math.Abs(a-b) < epsilon
}

// 获取步长的小数精度
func getStringFromStepSizePrecision(value, stepSize float64) string {
	stepStr := strconv.FormatFloat(stepSize, 'f', -1, 64) // 转换为字符串
	parts := strings.Split(stepStr, ".")
	if len(parts) == 2 { // 小数部分的长度即为精度
		return strconv.FormatFloat(value, 'f', len(parts[1]), 64) // 保留相同的小数位数
	} else {
		return fmt.Sprintf("%d", int64(value))
	}
}

// 调整数值并返回符合步长精度的字符串
func adjustToStepSize(value, stepSize float64) float64 {
	return math.Ceil(value/stepSize) * stepSize
}

type LhCoinSymbol struct {
	Id                uint    `json:"id"                ` //
	Coin              string  `json:"coin"              ` //
	Symbol            string  `json:"symbol"            ` //
	StartTime         int     `json:"startTime"         ` //
	EndTime           int     `json:"endTime"           ` //
	PricePrecision    int     `json:"pricePrecision"    ` // 小数点精度
	QuantityPrecision int     `json:"quantityPrecision" ` //
	IsOpen            int     `json:"isOpen"            ` //
	Plat              string  `json:"plat"              ` //
	LotSz             float64 `json:"lotSz"             ` //
	CtVal             float64 `json:"ctVal"             ` //
	VolumePlace       int     `json:"volumePlace"       ` //
	SizeMultiplier    float64 `json:"sizeMultiplier"    ` //
	QuantoMultiplier  float64 `json:"quantoMultiplier"  ` //
}

type BybitSymbol struct {
	QtyStep float64
}

type TraderPosition struct {
	Symbol         string  `json:"symbol"         ` //
	PositionSide   string  `json:"positionSide"   ` //
	PositionAmount float64 `json:"positionAmount" ` //
}

var (
	globalTraderNum = uint64(4454993952067821057) // todo 改 3887627985594221568
	orderMap        = gmap.New(true)              // 初始化下单记录
	orderMapTmp     = gmap.New(true)              // 初始化下单记录

	baseMoneyGuiTu      = gtype.NewFloat64()
	baseMoneyUserAllMap = gmap.NewIntAnyMap(true)

	globalUsers        = gmap.New(true)
	globalUsersOrderId = gmap.New(true)

	// 仓位
	binancePositionMap = make(map[string]*TraderPosition, 0)

	symbolsMap      = gmap.NewStrAnyMap(true)
	symbolsBybitMap = gmap.NewStrAnyMap(true)

	locKOrder = gmap.NewStrAnyMap(true)
)

// UpdateCoinInfo 初始化信息
func (s *sBinanceTraderHistory) UpdateCoinInfo(ctx context.Context) bool {
	// 获取代币信息
	var (
		err               error
		binanceSymbolInfo []*BinanceSymbolInfo
		bybitSymbolInfo   []*BybitContract
	)
	binanceSymbolInfo, err = getBinanceFuturesPairs()
	if nil != err {
		log.Println("更新币种，binance", err)
		return false
	}

	for _, v := range binanceSymbolInfo {
		symbolsMap.Set(v.Symbol, &LhCoinSymbol{
			QuantityPrecision: v.QuantityPrecision,
		})
	}

	bybitSymbolInfo, err = getByBitCoinInfo(ctx)
	if nil != err {
		log.Println("更新币种，bybit", err)
		return false
	}

	if 0 >= len(bybitSymbolInfo) {
		log.Println("bybit合约信息空的")
		return false
	}

	for _, v := range bybitSymbolInfo {
		var tmp float64
		tmp, err = strconv.ParseFloat(v.LotSizeFilter.QtyStep, 64)
		if nil != err {
			log.Println("更新币种，bybit", err)
			continue
		}

		if lessThanOrEqualZero(tmp, 0, 1e-7) {
			log.Println("更新币种，bybit", tmp, err)
			continue
		}

		symbolsBybitMap.Set(v.Symbol, &BybitSymbol{
			QtyStep: tmp,
		})
	}

	return true
}

// PullAndSetBaseMoneyNewGuiTuAndUser 拉取binance保证金数据
func (s *sBinanceTraderHistory) PullAndSetBaseMoneyNewGuiTuAndUser(ctx context.Context) {
	var (
		err error
		one string
	)

	one, err = requestBinanceTraderDetail(globalTraderNum)
	if nil != err {
		log.Println("拉取保证金失败：", err, globalTraderNum)
	}
	if 0 < len(one) {
		var tmp float64
		tmp, err = strconv.ParseFloat(one, 64)
		if nil != err {
			log.Println("拉取保证金，转化失败：", err, globalTraderNum)
		}

		if !floatEqual(tmp, baseMoneyGuiTu.Val(), 100) {
			log.Println("带单员保证金变更成功", tmp, baseMoneyGuiTu.Val())
			baseMoneyGuiTu.Set(tmp)
		}
	}
	time.Sleep(300 * time.Millisecond)

	var (
		users []*entity.User
	)
	err = g.Model("user").Ctx(ctx).
		Where("api_status=?", 1).
		Scan(&users)
	if nil != err {
		log.Println("拉取保证金，数据库查询错误：", err)
		return
	}

	tmpUserMap := make(map[uint]*entity.User, 0)
	for _, vUsers := range users {
		tmpUserMap[vUsers.Id] = vUsers
	}

	globalUsers.Iterator(func(k interface{}, v interface{}) bool {
		vGlobalUsers := v.(*entity.User)

		if _, ok := tmpUserMap[vGlobalUsers.Id]; !ok {
			log.Println("变更保证金，用户数据错误，数据库不存在：", vGlobalUsers)
			return true
		}

		if "binance" == tmpUserMap[vGlobalUsers.Id].Plat {
			var (
				detail string
			)
			detail = getBinanceInfo(vGlobalUsers.ApiKey, vGlobalUsers.ApiSecret)
			if 0 < len(detail) {
				var tmp float64
				tmp, err = strconv.ParseFloat(detail, 64)
				if nil != err {
					log.Println("拉取保证金，转化失败：", err, vGlobalUsers)
					return true
				}

				originTmp := tmp
				tmp *= tmpUserMap[vGlobalUsers.Id].Num
				if !baseMoneyUserAllMap.Contains(int(vGlobalUsers.Id)) {
					log.Println("初始化成功保证金", vGlobalUsers, tmp, originTmp, tmpUserMap[vGlobalUsers.Id].Num)
					baseMoneyUserAllMap.Set(int(vGlobalUsers.Id), tmp)
				} else {
					if !floatEqual(tmp, baseMoneyUserAllMap.Get(int(vGlobalUsers.Id)).(float64), 100) {
						log.Println("保证金变更成功", int(vGlobalUsers.Id), tmp, originTmp, tmpUserMap[vGlobalUsers.Id].Num)
						baseMoneyUserAllMap.Set(int(vGlobalUsers.Id), tmp)
					}
				}
			}

		} else if "bybit" == tmpUserMap[vGlobalUsers.Id].Plat {
			var (
				list []*BybitBalanceAccount
			)
			list, err = getBybitAccountBalance(ctx, vGlobalUsers.ApiKey, vGlobalUsers.ApiSecret)
			if 0 < len(list) {
				detail := list[0].TotalMarginBalance
				if 0 < len(detail) {
					var tmp float64
					tmp, err = strconv.ParseFloat(detail, 64)
					if nil != err {
						log.Println("拉取保证金，转化失败，bybit：", err, vGlobalUsers)
						return true
					}

					originTmp := tmp
					tmp *= tmpUserMap[vGlobalUsers.Id].Num
					if !baseMoneyUserAllMap.Contains(int(vGlobalUsers.Id)) {
						log.Println("初始化成功保证金，bybit", vGlobalUsers, tmp, originTmp, tmpUserMap[vGlobalUsers.Id].Num)
						baseMoneyUserAllMap.Set(int(vGlobalUsers.Id), tmp)
					} else {
						if !floatEqual(tmp, baseMoneyUserAllMap.Get(int(vGlobalUsers.Id)).(float64), 100) {
							log.Println("保证金变更成功，bybit", int(vGlobalUsers.Id), tmp, originTmp, tmpUserMap[vGlobalUsers.Id].Num)
							baseMoneyUserAllMap.Set(int(vGlobalUsers.Id), tmp)
						}
					}
				}
			} else {
				log.Println("保证金bybit平台，拉取失败，", vGlobalUsers)
			}
		}

		time.Sleep(300 * time.Millisecond)
		return true
	})
}

// InsertGlobalUsers  新增用户
func (s *sBinanceTraderHistory) InsertGlobalUsers(ctx context.Context) {
	var (
		err   error
		users []*entity.User
	)
	err = g.Model("user").Ctx(ctx).
		Where("api_status=?", 1).
		Scan(&users)
	if nil != err {
		log.Println("新增用户，数据库查询错误：", err)
		return
	}

	tmpUserMap := make(map[uint]*entity.User, 0)
	for _, vUsers := range users {
		tmpUserMap[vUsers.Id] = vUsers
	}

	// 第一遍比较，新增
	for _, vTmpUserMap := range users {
		if globalUsers.Contains(vTmpUserMap.Id) {
			// 变更可否开新仓
			if 2 != vTmpUserMap.OpenStatus && 2 == globalUsers.Get(vTmpUserMap.Id).(*entity.User).OpenStatus {
				log.Println("用户暂停:", vTmpUserMap)
				globalUsers.Set(vTmpUserMap.Id, vTmpUserMap)
			} else if 2 == vTmpUserMap.OpenStatus && 2 != globalUsers.Get(vTmpUserMap.Id).(*entity.User).OpenStatus {
				log.Println("用户开启:", vTmpUserMap)
				globalUsers.Set(vTmpUserMap.Id, vTmpUserMap)
			}

			// 变更num
			if !floatEqual(vTmpUserMap.Num, globalUsers.Get(vTmpUserMap.Id).(*entity.User).Num, 1e-7) {
				log.Println("用户变更num:", vTmpUserMap)
				globalUsers.Set(vTmpUserMap.Id, vTmpUserMap)
			}

			continue
		}

		// 杠杆
		//var (
		//	res bool
		//)
		//err, res = requestBinancePositionSide("true", vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret)
		//if nil != err || !res {
		//	log.Println("更新用户双向持仓模式失败", vTmpUserMap)
		//	continue
		//}

		globalUsersOrderId.Set(vTmpUserMap.Id, uint64(0))

		// 初始化仓位
		if 1 == vTmpUserMap.NeedInit {
			_, err = g.Model("user").Ctx(ctx).Data("need_init", 0).Where("id=?", vTmpUserMap.Id).Update()
			if nil != err {
				log.Println("新增用户，更新初始化状态失败:", vTmpUserMap)
			}

			strUserId := strconv.FormatUint(uint64(vTmpUserMap.Id), 10)

			if lessThanOrEqualZero(vTmpUserMap.Num, 0, 1e-7) {
				log.Println("新增用户，保证金系数错误：", vTmpUserMap)
				continue
			}

			// 新增仓位
			tmpTraderBaseMoney := baseMoneyGuiTu.Val()
			if lessThanOrEqualZero(tmpTraderBaseMoney, 0, 1e-7) {
				log.Println("新增用户，交易员信息无效了，信息", vTmpUserMap)
				continue
			}

			// 获取保证金
			var tmpUserBindTradersAmount float64

			if "binance" == vTmpUserMap.Plat {
				var (
					detail string
				)
				detail = getBinanceInfo(vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret)
				if nil != err {
					log.Println("新增用户，拉取保证金失败：", err, vTmpUserMap)
				}
				if 0 < len(detail) {
					var tmp float64
					tmp, err = strconv.ParseFloat(detail, 64)
					if nil != err {
						log.Println("新增用户，拉取保证金，转化失败：", err, vTmpUserMap)
					}

					originTmp := tmp
					tmp *= vTmpUserMap.Num
					tmpUserBindTradersAmount = tmp
					if !baseMoneyUserAllMap.Contains(int(vTmpUserMap.Id)) {
						log.Println("新增用户，初始化成功保证金", vTmpUserMap, originTmp, tmp, vTmpUserMap.Num)
						baseMoneyUserAllMap.Set(int(vTmpUserMap.Id), tmp)
					} else {
						if !IsEqual(tmp, baseMoneyUserAllMap.Get(int(vTmpUserMap.Id)).(float64)) {
							log.Println("新增用户，变更成功", int(vTmpUserMap.Id), originTmp, tmp, vTmpUserMap.Num)
							baseMoneyUserAllMap.Set(int(vTmpUserMap.Id), tmp)
						}
					}
				}
			} else if "bybit" == vTmpUserMap.Plat {
				var (
					list []*BybitBalanceAccount
				)
				list, err = getBybitAccountBalance(ctx, vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret)
				if nil != err {
					log.Println("新增用户，拉取保证金失败：", err, vTmpUserMap)
				}
				if 0 < len(list) {
					detail := list[0].TotalMarginBalance
					if 0 < len(detail) {
						var tmp float64
						tmp, err = strconv.ParseFloat(detail, 64)
						if nil != err {
							log.Println("新增用户，拉取保证金，转化失败，bybit：", err, vTmpUserMap)
							continue
						}

						originTmp := tmp
						tmp *= vTmpUserMap.Num
						tmpUserBindTradersAmount = tmp
						if !baseMoneyUserAllMap.Contains(int(vTmpUserMap.Id)) {
							log.Println("新增用户，初始化成功保证金", vTmpUserMap, originTmp, tmp, vTmpUserMap.Num)
							baseMoneyUserAllMap.Set(int(vTmpUserMap.Id), tmp)
						} else {
							if !IsEqual(tmp, baseMoneyUserAllMap.Get(int(vTmpUserMap.Id)).(float64)) {
								log.Println("新增用户，变更成功", int(vTmpUserMap.Id), originTmp, tmp, vTmpUserMap.Num)
								baseMoneyUserAllMap.Set(int(vTmpUserMap.Id), tmp)
							}
						}
					}
				}
			} else {
				log.Println("新增用户，交易员信息无效了，平台错误，信息", vTmpUserMap)
				continue
			}

			if lessThanOrEqualZero(tmpUserBindTradersAmount, 0, 1e-7) {
				log.Println("新增用户，保证金不足为0：", tmpUserBindTradersAmount, vTmpUserMap.Id)
				continue
			}

			// 仓位
			for _, vInsertData := range binancePositionMap {
				// 一个新symbol通常3个开仓方向short，long，both，屏蔽一下未真实开仓的
				tmpInsertData := vInsertData
				if IsEqual(tmpInsertData.PositionAmount, 0) {
					continue
				}

				if "binance" == vTmpUserMap.Plat {
					if !symbolsMap.Contains(tmpInsertData.Symbol) {
						log.Println("新增用户，代币信息无效，信息", tmpInsertData, vTmpUserMap)
						continue
					}

					var (
						tmpQty        float64
						quantity      string
						quantityFloat float64
						side          string
						positionSide  string
						orderType     = "MARKET"
					)
					if "LONG" == tmpInsertData.PositionSide {
						positionSide = "LONG"
						side = "BUY"
					} else if "SHORT" == tmpInsertData.PositionSide {
						positionSide = "SHORT"
						side = "SELL"
					} else if "BOTH" == tmpInsertData.PositionSide {
						if math.Signbit(tmpInsertData.PositionAmount) {
							positionSide = "SHORT"
							side = "SELL"
						} else {
							positionSide = "LONG"
							side = "BUY"
						}
					} else {
						log.Println("新增用户，无效信息，信息", vInsertData)
						continue
					}
					tmpPositionAmount := math.Abs(tmpInsertData.PositionAmount)
					// 本次 代单员币的数量 * (用户保证金/代单员保证金)
					tmpQty = tmpPositionAmount * tmpUserBindTradersAmount / tmpTraderBaseMoney // 本次开单数量

					// 精度调整
					if 0 >= symbolsMap.Get(tmpInsertData.Symbol).(*LhCoinSymbol).QuantityPrecision {
						quantity = fmt.Sprintf("%d", int64(tmpQty))
					} else {
						quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpInsertData.Symbol).(*LhCoinSymbol).QuantityPrecision, 64)
					}

					quantityFloat, err = strconv.ParseFloat(quantity, 64)
					if nil != err {
						log.Println(err)
						continue
					}

					if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
						continue
					}

					// 下单，不用计算数量，新仓位
					var (
						binanceOrderRes *binanceOrder
						orderInfoRes    *orderInfo
					)
					// 请求下单
					binanceOrderRes, orderInfoRes, err = requestBinanceOrder(tmpInsertData.Symbol, side, orderType, positionSide, quantity, vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret)
					if nil != err {
						log.Println(err)
					}

					//binanceOrderRes = &binanceOrder{
					//	OrderId:       1,
					//	ExecutedQty:   quantity,
					//	ClientOrderId: "",
					//	Symbol:        "",
					//	AvgPrice:      "",
					//	CumQuote:      "",
					//	Side:          "",
					//	PositionSide:  "",
					//	ClosePosition: false,
					//	Type:          "",
					//	Status:        "",
					//}

					var tmpExecutedQty float64
					tmpExecutedQty = quantityFloat

					// 下单异常
					if 0 >= binanceOrderRes.OrderId {
						if -4164 == orderInfoRes.Code {
							orderMapTmp.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
							log.Println("初始化，暂存：", vTmpUserMap, tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty, orderInfoRes)
							continue
						} else if -2015 == orderInfoRes.Code {
							log.Println("api无效，更新用户api_status：", vTmpUserMap)
							_, err = g.Model("user").Ctx(ctx).Data(g.Map{"api_status": 3}).Where("id=?", vTmpUserMap.Id).Update()
							if nil != err {
								log.Println("api无效，更新用户api_status：", err)
							}
						}

						log.Println(orderInfoRes)
						continue
					}

					// 不存在新增，这里只能是开仓
					if !orderMap.Contains(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId) {
						orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
					} else {

						d1 := decimal.NewFromFloat(orderMap.Get(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Add(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, newRes)
					}

					//  这里只能是，跟单人开仓，用户的预备仓位清空
					orderMapTmp.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))

					time.Sleep(50 * time.Millisecond)
				} else if "bybit" == vTmpUserMap.Plat {
					if !symbolsBybitMap.Contains(tmpInsertData.Symbol) {
						log.Println("新增用户，代币信息无效，信息，bybit", tmpInsertData, vTmpUserMap)
						continue
					}

					var (
						tmpQty            float64
						quantity          string
						quantityFloat     float64
						positionSide      string
						sideOrder         string
						positionSideOrder int
					)
					if "LONG" == tmpInsertData.PositionSide {
						positionSide = "LONG"
						sideOrder = "Buy"
						positionSideOrder = 1
					} else if "SHORT" == tmpInsertData.PositionSide {
						positionSide = "SHORT"
						sideOrder = "Sell"
						positionSideOrder = 2
					} else if "BOTH" == tmpInsertData.PositionSide {
						if math.Signbit(tmpInsertData.PositionAmount) {
							positionSide = "SHORT"
							sideOrder = "Sell"
							positionSideOrder = 2
						} else {
							positionSide = "LONG"
							sideOrder = "Buy"
							positionSideOrder = 1
						}
					} else {
						log.Println("新增用户，无效信息，信息", vInsertData)
						continue
					}

					tmpPositionAmount := math.Abs(tmpInsertData.PositionAmount)
					// 本次 代单员币的数量 * (用户保证金/代单员保证金)
					tmpQty = tmpPositionAmount * tmpUserBindTradersAmount / tmpTraderBaseMoney // 本次开单数量

					// 精度调整
					tmpQtyStep := symbolsBybitMap.Get(tmpInsertData.Symbol).(*BybitSymbol).QtyStep
					tmpFloatQuantity := adjustToStepSize(tmpQty, tmpQtyStep)

					quantity = getStringFromStepSizePrecision(tmpFloatQuantity, tmpQtyStep)
					quantityFloat, err = strconv.ParseFloat(quantity, 64)
					if nil != err {
						log.Println(err)
						continue
					}

					if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
						continue
					}

					if !globalUsersOrderId.Contains(vTmpUserMap.Id) {
						log.Println("新增用户，无效信息，不存在自增订单，信息", vInsertData, vTmpUserMap)
						continue
					}

					tmpOrderId := globalUsersOrderId.Get(vTmpUserMap.Id).(uint64)
					globalUsersOrderId.Set(vTmpUserMap.Id, tmpOrderId+1)
					tmpOrderIdStr := strconv.FormatUint(uint64(vTmpUserMap.Id), 10) + "at" + strconv.FormatUint(uint64(time.Now().Unix()), 10) + "&" + strconv.FormatUint(tmpOrderId, 10)

					var (
						resOrder *BybitPlaceOrderResponse
					)
					resOrder, err = bybitPlaceOrder(ctx, vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret, tmpInsertData.Symbol, quantity, sideOrder, positionSideOrder, tmpOrderIdStr)
					if nil != err {
						log.Println("bybit 初始化仓位下单错误", err, resOrder)
					}

					if nil == resOrder {
						log.Println("bybit 初始化仓位下单错误，没有返回结果", err, resOrder)
						continue
					}

					var tmpExecutedQty float64
					tmpExecutedQty = quantityFloat
					if 0 != resOrder.RetCode {
						if 10010 == resOrder.RetCode {
							log.Println("api无效，更新用户api_status：", vTmpUserMap)
							_, err = g.Model("user").Ctx(ctx).Data(g.Map{"api_status": 3}).Where("id=?", vTmpUserMap.Id).Update()
							if nil != err {
								log.Println("api无效，更新用户api_status：", err)
							}
						} else if 110094 == resOrder.RetCode {
							orderMapTmp.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
							log.Println("初始化，暂存：", vTmpUserMap, tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty, resOrder)
							continue
						}

						fmt.Println("bybit 初始化仓位下单异常：", resOrder)
						continue
					}

					if 0 >= len(resOrder.Result.OrderId) {
						log.Println("bybit 初始化仓位下单错误，订单号错误：", resOrder)
						continue
					}

					// 不存在新增，这里只能是开仓
					if !orderMap.Contains(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId) {
						orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
					} else {

						d1 := decimal.NewFromFloat(orderMap.Get(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Add(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, newRes)
					}

					//  这里只能是，跟单人开仓，用户的预备仓位清空
					orderMapTmp.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))

					log.Println("初始化，现有仓位：", tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, orderMap.Get(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId))
					time.Sleep(50 * time.Millisecond)
				} else {
					log.Println("新增用户，交易员信息无效了，平台错误，信息", vTmpUserMap)
					continue
				}
			}
		}

		globalUsers.Set(vTmpUserMap.Id, vTmpUserMap)

		log.Println("新增用户:", vTmpUserMap)
	}

	// 第二遍比较，删除
	tmpIds := make([]uint, 0)
	globalUsers.Iterator(func(k interface{}, v interface{}) bool {
		if _, ok := tmpUserMap[k.(uint)]; !ok {
			tmpIds = append(tmpIds, k.(uint))
		}
		return true
	})

	// 删除的人
	for _, vTmpIds := range tmpIds {
		log.Println("删除用户:", vTmpIds)
		globalUsers.Remove(vTmpIds)
		globalUsersOrderId.Remove(vTmpIds)

		tmpRemoveUserKey := make([]string, 0)
		// 遍历map
		orderMap.Iterator(func(k interface{}, v interface{}) bool {
			parts := strings.Split(k.(string), "&")
			if 3 != len(parts) {
				return true
			}

			var (
				uid uint64
			)
			uid, err = strconv.ParseUint(parts[2], 10, 64)
			if nil != err {
				log.Println("删除用户,解析id错误:", vTmpIds)
			}

			if uid != uint64(vTmpIds) {
				return true
			}

			tmpRemoveUserKey = append(tmpRemoveUserKey, k.(string))
			return true
		})

		for _, vK := range tmpRemoveUserKey {
			if orderMap.Contains(vK) {
				orderMap.Remove(vK)
			}
		}

		tmpRemoveUserKeyTwo := make([]string, 0)
		// 遍历map
		orderMapTmp.Iterator(func(k interface{}, v interface{}) bool {
			parts := strings.Split(k.(string), "&")
			if 3 != len(parts) {
				return true
			}

			var (
				uid uint64
			)
			uid, err = strconv.ParseUint(parts[2], 10, 64)
			if nil != err {
				log.Println("删除用户,解析id错误，tmp:", vTmpIds)
			}

			if uid != uint64(vTmpIds) {
				return true
			}

			tmpRemoveUserKeyTwo = append(tmpRemoveUserKeyTwo, k.(string))
			return true
		})

		for _, vK := range tmpRemoveUserKeyTwo {
			if orderMapTmp.Contains(vK) {
				orderMapTmp.Remove(vK)
			}
		}
	}
}

// PullAndOrderNewGuiTu 拉取binance数据，仓位，根据cookie
func (s *sBinanceTraderHistory) PullAndOrderNewGuiTu(ctx context.Context) {
	var (
		traderNum                 = globalTraderNum
		zyTraderCookie            []*entity.ZyTraderCookie
		binancePositionMapCompare map[string]*TraderPosition
		reqResData                []*binancePositionDataList
		cookie                    = "no"
		token                     = "no"
		err                       error
	)

	// 执行
	for {
		time.Sleep(50 * time.Millisecond) // 测试 28 最低值
		//time.Sleep(5 * time.Second) // 测试 28 最低值
		start := time.Now()

		// 重新初始化数据
		if 0 < len(binancePositionMap) {
			binancePositionMapCompare = make(map[string]*TraderPosition, 0)
			for k, vBinancePositionMap := range binancePositionMap {
				binancePositionMapCompare[k] = vBinancePositionMap
			}
		}

		if "no" == cookie || "no" == token {
			// 数据库必须信息
			err = g.Model("zy_trader_cookie").Ctx(ctx).Where("trader_id=? and is_open=?", 1, 1).
				OrderDesc("update_time").Limit(1).Scan(&zyTraderCookie)
			if nil != err {
				time.Sleep(time.Second * 3)
				continue
			}

			if 0 >= len(zyTraderCookie) || 0 >= len(zyTraderCookie[0].Cookie) || 0 >= len(zyTraderCookie[0].Token) {
				time.Sleep(time.Second * 3)
				continue
			}

			// 更新
			cookie = zyTraderCookie[0].Cookie
			token = zyTraderCookie[0].Token
		}

		// 执行
		var (
			retry           = false
			retryTimes      = 0
			retryTimesLimit = 5 // 重试次数
			cookieErr       = false
		)

		for retryTimes < retryTimesLimit { // 最大重试
			reqResData, retry, err = s.requestBinancePositionHistoryNew(traderNum, cookie, token)

			// 需要重试
			if retry {
				retryTimes++
				time.Sleep(time.Second * 5)
				log.Println("重试：", retry)
				continue
			}

			// cookie不好使
			if 0 >= len(reqResData) {
				retryTimes++
				cookieErr = true
				continue
			} else {
				cookieErr = false
				break
			}
		}

		// 记录时间
		timePull := time.Since(start)

		// cookie 错误
		if cookieErr {
			cookie = "no"
			token = "no"

			log.Println("cookie错误，信息", traderNum, reqResData)
			err = g.DB().Transaction(context.TODO(), func(ctx context.Context, tx gdb.TX) error {
				zyTraderCookie[0].IsOpen = 0
				_, err = tx.Ctx(ctx).Update("zy_trader_cookie", zyTraderCookie[0], "id", zyTraderCookie[0].Id)
				if nil != err {
					log.Println("cookie错误，信息", traderNum, reqResData)
					return err
				}

				return nil
			})
			if nil != err {
				log.Println("cookie错误，更新数据库错误，信息", traderNum, err)
			}

			continue
		}

		// 用于数据库更新
		insertData := make([]*TraderPosition, 0)
		updateData := make([]*TraderPosition, 0)
		// 用于下单
		orderInsertData := make([]*TraderPosition, 0)
		orderUpdateData := make([]*TraderPosition, 0)
		for _, vReqResData := range reqResData {
			// 新增
			var (
				currentAmount    float64
				currentAmountAbs float64
			)
			currentAmount, err = strconv.ParseFloat(vReqResData.PositionAmount, 64)
			if nil != err {
				log.Println("解析金额出错，信息", vReqResData, currentAmount, traderNum)
			}
			currentAmountAbs = math.Abs(currentAmount) // 绝对值

			if _, ok := binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide]; !ok {
				if "BOTH" != vReqResData.PositionSide { // 单项持仓
					// 加入数据库
					insertData = append(insertData, &TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
					})

					// 下单
					if IsEqual(currentAmountAbs, 0) {
						continue
					}

					orderInsertData = append(orderInsertData, &TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
					})
				} else {
					// 加入数据库
					insertData = append(insertData, &TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmount, // 正负数保持
					})

					// 模拟为多空仓，下单，todo 组合式的判断应该时牢靠的
					var tmpPositionSide string
					if IsEqual(currentAmount, 0) {
						continue
					} else if math.Signbit(currentAmount) {
						// 模拟空
						tmpPositionSide = "SHORT"
						orderInsertData = append(orderInsertData, &TraderPosition{
							Symbol:         vReqResData.Symbol,
							PositionSide:   tmpPositionSide,
							PositionAmount: currentAmountAbs, // 变成绝对值
						})
					} else {
						// 模拟多
						tmpPositionSide = "LONG"
						orderInsertData = append(orderInsertData, &TraderPosition{
							Symbol:         vReqResData.Symbol,
							PositionSide:   tmpPositionSide,
							PositionAmount: currentAmountAbs, // 变成绝对值
						})
					}
				}
			} else {
				// 数量无变化
				if "BOTH" != vReqResData.PositionSide {
					if IsEqual(currentAmountAbs, binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						continue
					}

					updateData = append(updateData, &TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
					})

					orderUpdateData = append(orderUpdateData, &TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmountAbs,
					})
				} else {
					if IsEqual(currentAmount, binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						continue
					}

					updateData = append(updateData, &TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   vReqResData.PositionSide,
						PositionAmount: currentAmount, // 正负数保持
					})

					// 第一步：构造虚拟的上一次仓位，空或多或无
					// 这里修改一下历史仓位的信息，方便程序在后续的流程中使用，模拟both的positionAmount为正数时，修改仓位对应的多仓方向的数据，为负数时修改空仓位的数据，0时不处理
					if _, ok = binancePositionMap[vReqResData.Symbol+"SHORT"]; !ok {
						log.Println("缺少仓位SHORT，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
						continue
					}
					if _, ok = binancePositionMap[vReqResData.Symbol+"LONG"]; !ok {
						log.Println("缺少仓位LONG，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
						continue
					}

					var lastPositionSide string // 上次仓位
					binancePositionMapCompare[vReqResData.Symbol+"SHORT"] = &TraderPosition{
						Symbol:         binancePositionMapCompare[vReqResData.Symbol+"SHORT"].Symbol,
						PositionSide:   binancePositionMapCompare[vReqResData.Symbol+"SHORT"].PositionSide,
						PositionAmount: 0,
					}
					binancePositionMapCompare[vReqResData.Symbol+"LONG"] = &TraderPosition{
						Symbol:         binancePositionMapCompare[vReqResData.Symbol+"LONG"].Symbol,
						PositionSide:   binancePositionMapCompare[vReqResData.Symbol+"LONG"].PositionSide,
						PositionAmount: 0,
					}

					if IsEqual(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount, 0) { // both仓为0
						// 认为两仓都无

					} else if math.Signbit(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount) {
						lastPositionSide = "SHORT"
						binancePositionMapCompare[vReqResData.Symbol+"SHORT"].PositionAmount = math.Abs(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount)
					} else {
						lastPositionSide = "LONG"
						binancePositionMapCompare[vReqResData.Symbol+"LONG"].PositionAmount = math.Abs(binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide].PositionAmount)
					}

					// 本次仓位
					var tmpPositionSide string
					if IsEqual(currentAmount, 0) { // 本次仓位是0
						if 0 >= len(lastPositionSide) {
							// 本次和上一次仓位都是0，应该不会走到这里
							log.Println("仓位异常逻辑，信息", binancePositionMap[vReqResData.Symbol+vReqResData.PositionSide])
							continue
						}

						// 仍为是一次完全平仓，仓位和上一次保持一致
						tmpPositionSide = lastPositionSide
					} else if math.Signbit(currentAmount) { // 判断有无符号
						// 第二步：本次仓位

						// 上次和本次相反需要平上次
						if "LONG" == lastPositionSide {
							orderUpdateData = append(orderUpdateData, &TraderPosition{
								Symbol:         vReqResData.Symbol,
								PositionSide:   lastPositionSide,
								PositionAmount: float64(0),
							})
						}

						tmpPositionSide = "SHORT"
					} else {
						// 第二步：本次仓位

						// 上次和本次相反需要平上次
						if "SHORT" == lastPositionSide {
							orderUpdateData = append(orderUpdateData, &TraderPosition{
								Symbol:         vReqResData.Symbol,
								PositionSide:   lastPositionSide,
								PositionAmount: float64(0),
							})
						}

						tmpPositionSide = "LONG"
					}

					orderUpdateData = append(orderUpdateData, &TraderPosition{
						Symbol:         vReqResData.Symbol,
						PositionSide:   tmpPositionSide,
						PositionAmount: currentAmountAbs,
					})
				}
			}
		}

		if 0 >= len(insertData) && 0 >= len(updateData) {
			continue
		}

		// 新增数据
		for _, vIBinancePosition := range insertData {
			binancePositionMap[vIBinancePosition.Symbol+vIBinancePosition.PositionSide] = &TraderPosition{
				Symbol:         vIBinancePosition.Symbol,
				PositionSide:   vIBinancePosition.PositionSide,
				PositionAmount: vIBinancePosition.PositionAmount,
			}
		}

		// 更新仓位数据
		for _, vUBinancePosition := range updateData {
			binancePositionMap[vUBinancePosition.Symbol+vUBinancePosition.PositionSide] = &TraderPosition{
				Symbol:         vUBinancePosition.Symbol,
				PositionSide:   vUBinancePosition.PositionSide,
				PositionAmount: vUBinancePosition.PositionAmount,
			}
		}

		// 推送订单，数据库已初始化仓位，新仓库
		if 0 >= len(binancePositionMapCompare) {
			log.Println("初始化仓位成功")
			continue
		}

		log.Printf("程序拉取部分，开始 %v, 拉取时长: %v, 统计更新时长: %v\n", start, timePull, time.Since(start))

		// 闪烁检测
		tmpNow := start.Unix()
		for _, vInsertData := range orderInsertData {
			locKOrder.Set(vInsertData.Symbol+"&"+vInsertData.PositionSide, tmpNow)
		}

		// 同一次执行时，业务上，同一仓位只会存在于insert或update中的一个
		for _, vUpdateData := range orderUpdateData {
			tmpUpdateData := vUpdateData
			if _, ok := binancePositionMapCompare[vUpdateData.Symbol+vUpdateData.PositionSide]; !ok {
				continue
			}
			lastPositionData := binancePositionMapCompare[vUpdateData.Symbol+vUpdateData.PositionSide]

			// 判断是否闪烁
			if lessThanOrEqualZero(tmpUpdateData.PositionAmount, 0, 1e-7) {
				log.Println("判断闪烁，完全平仓：", tmpUpdateData)

				if locKOrder.Contains(tmpUpdateData.Symbol + "&" + tmpUpdateData.PositionSide) {
					lastOrderT := locKOrder.Get(tmpUpdateData.Symbol + "&" + tmpUpdateData.PositionSide).(int64)
					if (tmpNow - 30) < lastOrderT {
						fmt.Println("可能抖动", tmpUpdateData, lastOrderT, tmpNow)
						// 修改
						_, err = g.Model("user").Ctx(ctx).Data("open_status", 4).Where("id>=?", 1).Update()
						if nil != err {
							log.Println("可能抖动，暂停：", err)
						}

						var (
							users []*entity.User
						)
						err = g.Model("user").Ctx(ctx).
							Where("api_status=?", 1).
							Scan(&users)
						if nil != err {
							log.Println("新增用户，数据库查询错误：", err)
						}

						for _, vUsers := range users {
							if globalUsers.Contains(vUsers.Id) {
								globalUsers.Set(vUsers.Id, vUsers)
							}
						}
					}
				}
			} else if lessThanOrEqualZero(lastPositionData.PositionAmount, tmpUpdateData.PositionAmount, 1e-7) {
				// 追加仓位时候记录时间
				locKOrder.Set(tmpUpdateData.Symbol+"&"+tmpUpdateData.PositionSide, tmpNow)
			}
		}

		wg := sync.WaitGroup{}
		// 遍历跟单者
		tmpTraderBaseMoney := baseMoneyGuiTu.Val()
		globalUsers.Iterator(func(k interface{}, v interface{}) bool {
			tmpUser := v.(*entity.User)

			var tmpUserBindTradersAmount float64
			if !baseMoneyUserAllMap.Contains(int(tmpUser.Id)) {
				log.Println("保证金不存在：", tmpUser)
				return true
			}
			tmpUserBindTradersAmount = baseMoneyUserAllMap.Get(int(tmpUser.Id)).(float64)

			if lessThanOrEqualZero(tmpUserBindTradersAmount, 0, 1e-7) {
				log.Println("保证金不足为0：", tmpUserBindTradersAmount, tmpUser)
				return true
			}

			strUserId := strconv.FormatUint(uint64(tmpUser.Id), 10)
			if 0 >= len(tmpUser.ApiSecret) || 0 >= len(tmpUser.ApiKey) {
				log.Println("用户的信息无效了，信息", traderNum, tmpUser)
				return true
			}

			if lessThanOrEqualZero(tmpTraderBaseMoney, 0, 1e-7) {
				log.Println("交易员信息无效了，信息", tmpUser)
				return true
			}

			// 新增仓位
			for _, vInsertData := range orderInsertData {
				if 2 != tmpUser.OpenStatus {
					log.Println("暂停用户:", tmpUser, vInsertData)
					// 暂停开新仓
					continue
				}

				// 一个新symbol通常3个开仓方向short，long，both，屏蔽一下未真实开仓的
				tmpInsertData := vInsertData
				if lessThanOrEqualZero(tmpInsertData.PositionAmount, 0, 1e-7) {
					continue
				}

				if "binance" == tmpUser.Plat {
					if !symbolsMap.Contains(tmpInsertData.Symbol) {
						log.Println("代币信息无效，信息", tmpInsertData, tmpUser)
						continue
					}

					var (
						tmpQty        float64
						quantity      string
						quantityFloat float64
						side          string
						positionSide  string
						orderType     = "MARKET"
					)
					if "LONG" == tmpInsertData.PositionSide {
						positionSide = "LONG"
						side = "BUY"
					} else if "SHORT" == tmpInsertData.PositionSide {
						positionSide = "SHORT"
						side = "SELL"
					} else {
						log.Println("无效信息，信息", vInsertData)
						continue
					}

					// 本次 代单员币的数量 * (用户保证金/代单员保证金)
					tmpQty = tmpInsertData.PositionAmount * tmpUserBindTradersAmount / tmpTraderBaseMoney // 本次开单数量
					// 累加预备仓位
					if orderMapTmp.Contains(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId) {
						tmpOldQty := orderMapTmp.Get(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId).(float64)
						if !lessThanOrEqualZero(tmpOldQty, 0, 1e-7) {
							tmpQty += tmpOldQty
							log.Println("新增，暂存的累加开仓：", tmpQty, tmpOldQty, tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId)
						}
					}

					// 精度调整
					if 0 >= symbolsMap.Get(tmpInsertData.Symbol).(*LhCoinSymbol).QuantityPrecision {
						quantity = fmt.Sprintf("%d", int64(tmpQty))
					} else {
						quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpInsertData.Symbol).(*LhCoinSymbol).QuantityPrecision, 64)
					}

					quantityFloat, err = strconv.ParseFloat(quantity, 64)
					if nil != err {
						log.Println(err)
						continue
					}

					if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
						continue
					}

					wg.Add(1)
					err = s.pool.Add(ctx, func(ctx context.Context) {
						defer wg.Done()

						// 下单，不用计算数量，新仓位
						var (
							binanceOrderRes *binanceOrder
							orderInfoRes    *orderInfo
						)
						// 请求下单
						binanceOrderRes, orderInfoRes, err = requestBinanceOrder(tmpInsertData.Symbol, side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != err {
							log.Println("执行下单错误，新增，错误", err, tmpInsertData.Symbol, side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret, orderInfoRes)
						}

						//binanceOrderRes = &binanceOrder{
						//	OrderId:       1,
						//	ExecutedQty:   quantity,
						//	ClientOrderId: "",
						//	Symbol:        "",
						//	AvgPrice:      "",
						//	CumQuote:      "",
						//	Side:          "",
						//	PositionSide:  "",
						//	ClosePosition: false,
						//	Type:          "",
						//	Status:        "",
						//}
						var tmpExecutedQty float64
						tmpExecutedQty = quantityFloat

						// 下单异常
						if 0 >= binanceOrderRes.OrderId {
							if -4164 == orderInfoRes.Code {
								// 追加仓位，开仓
								if ("LONG" == positionSide && "BUY" == side) || ("SHORT" == positionSide && "SELL" == side) {
									orderMapTmp.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
									log.Println("新增，暂存：", tmpUser, tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty, orderInfoRes)
								}

								return
							} else if -2015 == orderInfoRes.Code {
								log.Println("api无效，更新用户api_status：", tmpUser)
								_, err = g.Model("user").Ctx(ctx).Data(g.Map{"api_status": 3}).Where("id=?", tmpUser.Id).Update()
								if nil != err {
									log.Println("api无效，更新用户api_status：", err)
								}
							}

							log.Println("执行下单错误，新增：", tmpInsertData.Symbol, side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret, orderInfoRes)
							return
						}

						// 不存在新增，这里只能是开仓
						if !orderMap.Contains(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId) {
							orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
						} else {
							d1 := decimal.NewFromFloat(orderMap.Get(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId).(float64))
							d2 := decimal.NewFromFloat(tmpExecutedQty)
							result := d1.Add(d2)

							var (
								newRes float64
								exact  bool
							)
							newRes, exact = result.Float64()
							if !exact {
								fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId).(float64), newRes)
							}

							orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, newRes)
						}

						//  这里只能是，跟单人开仓，用户的预备仓位清空
						orderMapTmp.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))
						log.Println("现有仓位：", tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, orderMap.Get(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId))
						return
					})
					if nil != err {
						log.Println("添加下单任务异常，新增仓位，错误信息：", err, traderNum, vInsertData, tmpUser)
					}
				} else if "bybit" == tmpUser.Plat {
					if !symbolsBybitMap.Contains(tmpInsertData.Symbol) {
						log.Println("代币信息无效，信息，bybit", tmpInsertData, tmpUser)
						continue
					}

					var (
						tmpQty            float64
						quantity          string
						quantityFloat     float64
						positionSide      string
						sideOrder         string
						positionSideOrder int
					)
					if "LONG" == tmpInsertData.PositionSide {
						positionSide = "LONG"

						sideOrder = "Buy"
						positionSideOrder = 1
					} else if "SHORT" == tmpInsertData.PositionSide {
						positionSide = "SHORT"

						sideOrder = "Sell"
						positionSideOrder = 2
					} else if "BOTH" == tmpInsertData.PositionSide {
						if math.Signbit(tmpInsertData.PositionAmount) {
							positionSide = "SHORT"

							sideOrder = "Sell"
							positionSideOrder = 2
						} else {
							positionSide = "LONG"

							sideOrder = "Buy"
							positionSideOrder = 1
						}
					} else {
						log.Println("新增用户，无效信息，信息", vInsertData)
						continue
					}

					tmpPositionAmount := math.Abs(tmpInsertData.PositionAmount)
					// 本次 代单员币的数量 * (用户保证金/代单员保证金)
					tmpQty = tmpPositionAmount * tmpUserBindTradersAmount / tmpTraderBaseMoney // 本次开单数量
					// 累加预备仓位
					if orderMapTmp.Contains(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId) {
						tmpOldQty := orderMapTmp.Get(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId).(float64)
						if !lessThanOrEqualZero(tmpOldQty, 0, 1e-7) {
							tmpQty += tmpOldQty
							log.Println("新增，暂存的累加开仓：", tmpQty, tmpOldQty, tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId)
						}
					}

					// 精度调整
					tmpQtyStep := symbolsBybitMap.Get(tmpInsertData.Symbol).(*BybitSymbol).QtyStep
					tmpFloatQuantity := adjustToStepSize(tmpQty, tmpQtyStep)

					quantity = getStringFromStepSizePrecision(tmpFloatQuantity, tmpQtyStep)
					quantityFloat, err = strconv.ParseFloat(quantity, 64)
					if nil != err {
						log.Println(err)
						continue
					}

					if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
						continue
					}

					if !globalUsersOrderId.Contains(tmpUser.Id) {
						log.Println("新增，无效信息，不存在自增订单，信息", vInsertData, tmpUser)
						continue
					}

					tmpOrderId := globalUsersOrderId.Get(tmpUser.Id).(uint64)
					globalUsersOrderId.Set(tmpUser.Id, tmpOrderId+1)
					tmpOrderIdStr := strconv.FormatUint(uint64(tmpUser.Id), 10) + "at" + strconv.FormatUint(uint64(time.Now().Unix()), 10) + "&" + strconv.FormatUint(tmpOrderId, 10)

					wg.Add(1)
					err = s.pool.Add(ctx, func(ctx context.Context) {
						defer wg.Done()

						var (
							resOrder *BybitPlaceOrderResponse
						)
						resOrder, err = bybitPlaceOrder(ctx, tmpUser.ApiKey, tmpUser.ApiSecret, tmpInsertData.Symbol, quantity, sideOrder, positionSideOrder, tmpOrderIdStr)
						if nil != err {
							log.Println("bybit 仓位下单错误", err, resOrder)
						}

						if nil == resOrder {
							log.Println("bybit 仓位下单错误，没有返回结果", err, resOrder)
							return
						}

						var tmpExecutedQty float64
						tmpExecutedQty = quantityFloat
						if 0 != resOrder.RetCode {
							if 10010 == resOrder.RetCode {
								log.Println("api无效，更新用户api_status：", tmpUser)
								_, err = g.Model("user").Ctx(ctx).Data(g.Map{"api_status": 3}).Where("id=?", tmpUser.Id).Update()
								if nil != err {
									log.Println("api无效，更新用户api_status：", err)
								}
							} else if 110094 == resOrder.RetCode {
								orderMapTmp.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
								log.Println("新增，暂存：", tmpUser, tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty, resOrder)
								return
							}

							fmt.Println("bybit 新增仓位下单异常：", resOrder)
							return
						}

						if 0 >= len(resOrder.Result.OrderId) {
							log.Println("bybit 新增仓位下单错误，订单号错误：", resOrder)
							return
						}

						// 不存在新增，这里只能是开仓
						if !orderMap.Contains(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId) {
							orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
						} else {
							d1 := decimal.NewFromFloat(orderMap.Get(tmpInsertData.Symbol + "&" + positionSide + "&" + strUserId).(float64))
							d2 := decimal.NewFromFloat(tmpExecutedQty)
							result := d1.Add(d2)

							var (
								newRes float64
								exact  bool
							)
							newRes, exact = result.Float64()
							if !exact {
								fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId).(float64), newRes)
							}

							orderMap.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, newRes)
						}

						//  这里只能是，跟单人开仓，用户的预备仓位清空
						orderMapTmp.Set(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))
						log.Println("现有仓位：", tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId, orderMap.Get(tmpInsertData.Symbol+"&"+positionSide+"&"+strUserId))
						return
					})
					if nil != err {
						log.Println("添加下单任务异常，新增仓位，错误信息：", err, traderNum, vInsertData, tmpUser)
					}
				}
			}

			// 修改仓位
			for _, vUpdateData := range orderUpdateData {
				tmpUpdateData := vUpdateData
				if _, ok := binancePositionMapCompare[vUpdateData.Symbol+vUpdateData.PositionSide]; !ok {
					log.Println("添加下单任务异常，修改仓位，错误信息：", err, traderNum, vUpdateData, tmpUser)
					continue
				}
				lastPositionData := binancePositionMapCompare[vUpdateData.Symbol+vUpdateData.PositionSide]

				if "binance" == tmpUser.Plat {
					if !symbolsMap.Contains(tmpUpdateData.Symbol) {
						log.Println("代币信息无效，信息", tmpUpdateData, tmpUser)
						continue
					}
				} else if "bybit" == tmpUser.Plat {
					if !symbolsBybitMap.Contains(tmpUpdateData.Symbol) {
						log.Println("代币信息无效，信息，bybit", tmpUpdateData, tmpUser)
						continue
					}
				} else {
					log.Println("代币信息无效，信息，平台错误，bybit", tmpUpdateData, tmpUser)
					continue
				}

				var (
					tmpQty            float64
					quantity          string
					quantityFloat     float64
					side              string
					positionSide      string
					orderType         = "MARKET"
					sideOrder         string
					positionSideOrder int
				)

				if lessThanOrEqualZero(tmpUpdateData.PositionAmount, 0, 1e-7) {
					log.Println("完全平仓：", tmpUpdateData)
					// 全平仓
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "SELL"

						sideOrder = "Sell"
						positionSideOrder = 1
					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "BUY"

						sideOrder = "Buy"
						positionSideOrder = 2
					} else {
						log.Println("无效信息，信息", tmpUpdateData)
						continue
					}

					// 带单人完全平仓，用户可能没开起来过，所以也要把系统数据清空，用户的预备仓位清空
					if orderMapTmp.Contains(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId) {
						tmpOldQty := orderMapTmp.Get(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId).(float64)
						if !lessThanOrEqualZero(tmpOldQty, 0, 1e-7) {
							log.Println("变更，完全平仓，清空暂存：", tmpOldQty, tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId)
						}
					}
					orderMapTmp.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))

					// 未开启过仓位
					if !orderMap.Contains(tmpUpdateData.Symbol + "&" + tmpUpdateData.PositionSide + "&" + strUserId) {
						continue
					}

					// 认为是0
					if lessThanOrEqualZero(orderMap.Get(tmpUpdateData.Symbol+"&"+tmpUpdateData.PositionSide+"&"+strUserId).(float64), 0, 1e-7) {
						continue
					}

					// 剩余仓位
					tmpQty = orderMap.Get(tmpUpdateData.Symbol + "&" + tmpUpdateData.PositionSide + "&" + strUserId).(float64)
				} else if lessThanOrEqualZero(lastPositionData.PositionAmount, tmpUpdateData.PositionAmount, 1e-7) {
					if 2 != tmpUser.OpenStatus {
						log.Println("变更，暂停用户:", tmpUser, tmpUpdateData, lastPositionData)
						// 暂停开新仓
						continue
					}

					log.Println("追加仓位：", tmpUpdateData, lastPositionData)
					// 本次加仓 代单员币的数量 * (用户保证金/代单员保证金)
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "BUY"

						sideOrder = "Buy"
						positionSideOrder = 1
					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "SELL"

						sideOrder = "Sell"
						positionSideOrder = 2
					} else {
						log.Println("无效信息，信息", tmpUpdateData)
						continue
					}

					// 本次减去上一次
					tmpQty = (tmpUpdateData.PositionAmount - lastPositionData.PositionAmount) * tmpUserBindTradersAmount / tmpTraderBaseMoney // 本次开单数量

					// 累加预备仓位
					if orderMapTmp.Contains(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId) {
						tmpOldQty := orderMapTmp.Get(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId).(float64)
						if !lessThanOrEqualZero(tmpOldQty, 0, 1e-7) {
							tmpQty += tmpOldQty
							log.Println("变更，暂存的累加开仓：", tmpQty, tmpOldQty, tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId)
						}
					}
				} else if lessThanOrEqualZero(tmpUpdateData.PositionAmount, lastPositionData.PositionAmount, 1e-7) {
					log.Println("部分平仓：", tmpUpdateData, lastPositionData)
					// 部分平仓
					if "LONG" == tmpUpdateData.PositionSide {
						positionSide = "LONG"
						side = "SELL"

						sideOrder = "Sell"
						positionSideOrder = 1
					} else if "SHORT" == tmpUpdateData.PositionSide {
						positionSide = "SHORT"
						side = "BUY"

						sideOrder = "Buy"
						positionSideOrder = 2
					} else {
						log.Println("无效信息，信息", tmpUpdateData)
						continue
					}

					// 未开启过仓位
					if !orderMap.Contains(tmpUpdateData.Symbol + "&" + tmpUpdateData.PositionSide + "&" + strUserId) {
						continue
					}

					// 认为是0
					if lessThanOrEqualZero(orderMap.Get(tmpUpdateData.Symbol+"&"+tmpUpdateData.PositionSide+"&"+strUserId).(float64), 0, 1e-7) {
						continue
					}

					// 上次仓位
					if lessThanOrEqualZero(lastPositionData.PositionAmount, 0, 1e-7) {
						log.Println("部分平仓，上次仓位信息无效，信息", lastPositionData, tmpUpdateData)
						continue
					}

					// 按百分比
					tmpQty = orderMap.Get(tmpUpdateData.Symbol+"&"+tmpUpdateData.PositionSide+"&"+strUserId).(float64) * (lastPositionData.PositionAmount - tmpUpdateData.PositionAmount) / lastPositionData.PositionAmount
				} else {
					log.Println("分析仓位无效，信息", lastPositionData, tmpUpdateData)
					continue
				}

				var tmpOrderIdStr string
				if "binance" == tmpUser.Plat {
					// 精度调整
					if 0 >= symbolsMap.Get(tmpUpdateData.Symbol).(*LhCoinSymbol).QuantityPrecision {
						quantity = fmt.Sprintf("%d", int64(tmpQty))
					} else {
						quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(tmpUpdateData.Symbol).(*LhCoinSymbol).QuantityPrecision, 64)
					}

					quantityFloat, err = strconv.ParseFloat(quantity, 64)
					if nil != err {
						log.Println(err)
						continue
					}

					if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
						continue
					}
				} else if "bybit" == tmpUser.Plat {
					// 精度调整
					tmpQtyStep := symbolsBybitMap.Get(tmpUpdateData.Symbol).(*BybitSymbol).QtyStep
					tmpFloatQuantity := adjustToStepSize(tmpQty, tmpQtyStep)

					quantity = getStringFromStepSizePrecision(tmpFloatQuantity, tmpQtyStep)
					quantityFloat, err = strconv.ParseFloat(quantity, 64)
					if nil != err {
						log.Println(err)
						continue
					}

					//log.Println(tmpQtyStep, tmpQty, tmpFloatQuantity, quantity, quantityFloat)
					if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
						continue
					}

					if !globalUsersOrderId.Contains(tmpUser.Id) {
						log.Println("更新，无效信息，不存在自增订单，信息", tmpUpdateData, tmpUser)
						continue
					}

					tmpOrderId := globalUsersOrderId.Get(tmpUser.Id).(uint64)
					globalUsersOrderId.Set(tmpUser.Id, tmpOrderId+1)
					tmpOrderIdStr = strconv.FormatUint(uint64(tmpUser.Id), 10) + "at" + strconv.FormatUint(uint64(time.Now().Unix()), 10) + "&" + strconv.FormatUint(tmpOrderId, 10)

				} else {
					log.Println("无效信息，信息", tmpUpdateData)
					continue
				}

				wg.Add(1)
				err = s.pool.Add(ctx, func(ctx context.Context) {
					defer wg.Done()
					var tmpExecutedQty float64

					if "binance" == tmpUser.Plat {
						// 下单，不用计算数量，新仓位
						var (
							binanceOrderRes *binanceOrder
							orderInfoRes    *orderInfo
						)
						// 请求下单
						binanceOrderRes, orderInfoRes, err = requestBinanceOrder(tmpUpdateData.Symbol, side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
						if nil != err {
							log.Println("执行下单错误，变更，错误：", err, tmpUpdateData.Symbol, side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret)
							return
						}

						//binanceOrderRes = &binanceOrder{
						//	OrderId:       1,
						//	ExecutedQty:   quantity,
						//	ClientOrderId: "",
						//	Symbol:        "",
						//	AvgPrice:      "",
						//	CumQuote:      "",
						//	Side:          "",
						//	PositionSide:  "",
						//	ClosePosition: false,
						//	Type:          "",
						//	Status:        "",
						//}
						tmpExecutedQty = quantityFloat

						// 下单异常
						if 0 >= binanceOrderRes.OrderId {
							if -4164 == orderInfoRes.Code {
								// 追加仓位，开仓
								if ("LONG" == positionSide && "BUY" == side) || ("SHORT" == positionSide && "SELL" == side) {
									orderMapTmp.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
									log.Println("变更，暂存：", tmpUser, tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty, orderInfoRes)
								}
							} else if -2015 == orderInfoRes.Code {
								log.Println("api无效，更新用户api_status：", tmpUser)
								_, err = g.Model("user").Ctx(ctx).Data(g.Map{"api_status": 3}).Where("id=?", tmpUser.Id).Update()
								if nil != err {
									log.Println("api无效，更新用户api_status：", err)
								}
							}

							log.Println("执行下单错误，变更：", tmpUpdateData.Symbol, side, orderType, positionSide, quantity, tmpUser.ApiKey, tmpUser.ApiSecret, orderInfoRes)
							return
						}

					} else if "bybit" == tmpUser.Plat {
						var (
							resOrder *BybitPlaceOrderResponse
						)
						resOrder, err = bybitPlaceOrder(ctx, tmpUser.ApiKey, tmpUser.ApiSecret, tmpUpdateData.Symbol, quantity, sideOrder, positionSideOrder, tmpOrderIdStr)
						if nil != err {
							log.Println("bybit 仓位下单错误", err, resOrder)
						}

						if nil == resOrder {
							log.Println("bybit 仓位下单错误，没有返回结果", err, resOrder)
							return
						}

						tmpExecutedQty = quantityFloat
						if 0 != resOrder.RetCode {
							if 10010 == resOrder.RetCode {
								log.Println("api无效，更新用户api_status：", tmpUser)
								_, err = g.Model("user").Ctx(ctx).Data(g.Map{"api_status": 3}).Where("id=?", tmpUser.Id).Update()
								if nil != err {
									log.Println("api无效，更新用户api_status：", err)
								}
							} else if 110094 == resOrder.RetCode {
								orderMapTmp.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
								log.Println("新增，暂存：", tmpUser, tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty, resOrder)
								return
							}

							fmt.Println("bybit 新增仓位下单异常：", resOrder)
							return
						}

						if 0 >= len(resOrder.Result.OrderId) {
							log.Println("bybit 新增仓位下单错误，订单号错误：", resOrder)
							return
						}
					}

					// 不存在新增，这里只能是开仓
					if !orderMap.Contains(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId) {
						// 追加仓位，开仓
						if "LONG" == positionSide && "BUY" == side {
							orderMap.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
						} else if "SHORT" == positionSide && "SELL" == side {
							orderMap.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
						} else {
							log.Println("未知仓位信息，信息", tmpUpdateData, tmpExecutedQty)
						}

						// 跟单人开仓成功，用户的预备仓位清空
						orderMapTmp.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))
					} else {
						// 追加仓位，开仓
						if "LONG" == positionSide {
							if "BUY" == side {
								d1 := decimal.NewFromFloat(orderMap.Get(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId).(float64))
								d2 := decimal.NewFromFloat(tmpExecutedQty)
								result := d1.Add(d2)

								var (
									newRes float64
									exact  bool
								)
								newRes, exact = result.Float64()
								if !exact {
									fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId).(float64), newRes)
								}

								orderMap.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, newRes)

								// 跟单人开仓成功，用户的预备仓位清空
								orderMapTmp.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))
							} else if "SELL" == side {
								d1 := decimal.NewFromFloat(orderMap.Get(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId).(float64))
								d2 := decimal.NewFromFloat(tmpExecutedQty)
								result := d1.Sub(d2)

								var (
									newRes float64
									exact  bool
								)
								newRes, exact = result.Float64()
								if !exact {
									fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId).(float64), newRes)
								}

								if lessThanOrEqualZero(newRes, 0, 1e-7) {
									newRes = 0
								}

								orderMap.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, newRes)

								//  跟单人完全平仓，用户的预备仓位清空
								if lessThanOrEqualZero(newRes, 0, 1e-7) {
									orderMapTmp.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))
								}
							} else {
								log.Println("未知仓位信息，信息", tmpUpdateData, tmpExecutedQty)
							}

						} else if "SHORT" == positionSide {
							if "SELL" == side {
								d1 := decimal.NewFromFloat(orderMap.Get(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId).(float64))
								d2 := decimal.NewFromFloat(tmpExecutedQty)
								result := d1.Add(d2)

								var (
									newRes float64
									exact  bool
								)
								newRes, exact = result.Float64()
								if !exact {
									fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId).(float64), newRes)
								}

								orderMap.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, newRes)

								// 跟单人开仓成功，用户的预备仓位清空
								orderMapTmp.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))
							} else if "BUY" == side {
								d1 := decimal.NewFromFloat(orderMap.Get(tmpUpdateData.Symbol + "&" + positionSide + "&" + strUserId).(float64))
								d2 := decimal.NewFromFloat(tmpExecutedQty)
								result := d1.Sub(d2)

								var (
									newRes float64
									exact  bool
								)
								newRes, exact = result.Float64()
								if !exact {
									fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId).(float64), newRes)
								}

								if lessThanOrEqualZero(newRes, 0, 1e-7) {
									newRes = 0
								}

								orderMap.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, newRes)

								//  跟单人完全平仓，用户的预备仓位清空
								if lessThanOrEqualZero(newRes, 0, 1e-7) {
									orderMapTmp.Set(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, float64(0))
								}
							} else {
								log.Println("未知仓位信息，信息", tmpUpdateData, tmpExecutedQty)
							}

						} else {
							log.Println("未知仓位信息，信息", tmpUpdateData, tmpExecutedQty)
						}
					}

					log.Println("现有仓位：", tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId, orderMap.Get(tmpUpdateData.Symbol+"&"+positionSide+"&"+strUserId))
					return
				})
				if nil != err {
					log.Println("新，添加下单任务异常，修改仓位，错误信息：", err, traderNum, vUpdateData, tmpUser)
				}
			}

			return true
		})

		// 回收协程
		wg.Wait()

		log.Printf("程序执行完毕，开始 %v, 拉取时长: %v, 总计时长: %v\n", start, timePull, time.Since(start))
	}
}

// CookieErrEmail email
func (s *sBinanceTraderHistory) CookieErrEmail(ctx context.Context) {
	var (
		err         error
		cookieEmail []*entity.CookieEmail
		cookies     []*entity.ZyTraderCookie
	)
	err = g.Model("zy_trader_cookie").Ctx(ctx).Scan(&cookies)
	if nil != err {
		log.Println("cookies，数据库查询错误：", err)
		return
	}

	if 0 >= len(cookies) {
		log.Println("cookies，没有cookie")
		return
	}

	if 1 == cookies[0].IsOpen {
		return
	}

	err = g.Model("cookie_email").Ctx(ctx).Scan(&cookieEmail)
	if nil != err {
		log.Println("cookie_email，数据库查询错误：", err)
		return
	}

	if 0 >= len(cookieEmail) {
		return
	}

	dialer := gomail.NewDialer("smtp.163.com", 465, "18510841547@163.com", "FNXmjvfpDSEC2ckD")
	dialer.TLSConfig = &tls.Config{InsecureSkipVerify: true} // 跳过安全验证，如果不设置，会导致连接失败
	for _, v := range cookieEmail {
		// 发送邮件
		mail := gomail.NewMessage()
		mail.SetHeader("From", "18510841547@163.com")
		mail.SetHeader("To", v.Email)                 // 替换为收件人邮箱
		mail.SetHeader("Subject", "cookie失效")         // 替换为邮件主题
		mail.SetBody("text/html", "<b>cookie失效了</b>") // 替换为邮件正文

		if err = dialer.DialAndSend(mail); err != nil {
			fmt.Println("cookie，邮件发送失败：", err, v.Email)
			return
		}
	}

	return
}

// GetSystemUserNum get user num
func (s *sBinanceTraderHistory) GetSystemUserNum(ctx context.Context) map[string]float64 {
	var (
		err   error
		users []*entity.User
		res   map[string]float64
	)
	res = make(map[string]float64, 0)

	err = g.Model("user").Ctx(ctx).Scan(&users)
	if nil != err {
		log.Println("获取用户num，数据库查询错误：", err)
		return res
	}

	for _, v := range users {
		res[v.ApiKey] = v.Num
	}

	return res
}

// CreateUser set user num
func (s *sBinanceTraderHistory) CreateUser(ctx context.Context, address, apiKey, apiSecret, plat string, needInit uint64, num float64) error {
	var (
		users []*entity.User
		err   error
	)
	apiStatusOk := make([]uint64, 0)
	apiStatusOk = append(apiStatusOk, 1)

	err = g.Model("user").WhereIn("api_status", apiStatusOk).Ctx(ctx).Scan(&users)
	if nil != err {
		log.Println("CreateUser，数据库查询错误：", err)
		return err
	}

	tmpMap := make(map[string][]*entity.User, 0)
	for _, v := range users {
		if _, ok := tmpMap[v.Plat]; !ok {
			tmpMap[v.Plat] = make([]*entity.User, 0)
		}

		tmpMap[v.Plat] = append(tmpMap[v.Plat], v)
	}

	if _, ok := tmpMap[plat]; ok {
		if 50 <= len(tmpMap[plat]) {
			return errors.New("超人数")
		}
	}

	_, err = g.Model("user").Ctx(ctx).Insert(&do.User{
		Address:    address,
		ApiStatus:  1,
		ApiKey:     apiKey,
		ApiSecret:  apiSecret,
		OpenStatus: 2,
		CreatedAt:  gtime.Now(),
		UpdatedAt:  gtime.Now(),
		NeedInit:   needInit,
		Num:        num,
		Plat:       plat,
		Dai:        0,
		Ip:         1,
	})

	if nil != err {
		log.Println("新增用户失败：", err)
		return err
	}
	return nil
}

// SetPositionSide set position side
func (s *sBinanceTraderHistory) SetPositionSide(ctx context.Context, plat, apiKey, apiSecret string) (uint64, string) {
	var (
		res    bool
		resStr string
		err    error
	)

	if "binance" == plat {
		err, resStr, res = requestBinancePositionSide("true", apiKey, apiSecret)
		if nil != err || !res {
			return 0, resStr
		}

		return 1, resStr
	} else if "bybit" == plat {
		var (
			bybitRes    *BybitSetPositionSideResponse
			resStrBybit string
		)
		bybitRes, resStrBybit, err = bybitSetPositionSide(ctx, apiKey, apiSecret, "BTCUSDT")
		if nil != err || nil == bybitRes {
			return 0, resStrBybit
		}

		if 0 != bybitRes.RetCode {
			if 110025 == bybitRes.RetCode {
				return 1, resStrBybit
			}

			return 0, resStrBybit
		}

		return 1, resStrBybit
	}

	return 0, ""
}

// SetSystemUserNum set user num
func (s *sBinanceTraderHistory) SetSystemUserNum(ctx context.Context, apiKey string, num float64) error {
	var (
		err error
	)
	_, err = g.Model("user").Ctx(ctx).Data("num", num).Where("api_key=?", apiKey).Update()
	if nil != err {
		log.Println("更新用户num：", err)
		return err
	}

	return nil
}

// GetApiStatus get user api status
func (s *sBinanceTraderHistory) GetApiStatus(ctx context.Context, apiKey string) int64 {
	var (
		err   error
		users []*entity.User
	)

	err = g.Model("user").Where("api_key=?", apiKey).Ctx(ctx).Scan(&users)
	if nil != err {
		log.Println("查看用户仓位，数据库查询错误：", err)
		return -1
	}

	if 0 >= len(users) || 0 >= users[0].Id {
		return -1
	}

	return int64(users[0].ApiStatus)
}

// SetApiStatus set user api status
func (s *sBinanceTraderHistory) SetApiStatus(ctx context.Context, apiKey string, status uint64, init uint64) uint64 {
	var (
		err   error
		users []*entity.User
	)

	err = g.Model("user").Where("api_key=?", apiKey).Ctx(ctx).Scan(&users)
	if nil != err {
		log.Println("查看用户仓位，数据库查询错误：", err)
		return 0
	}

	if 0 >= len(users) || 0 >= users[0].Id {
		return 0
	}

	//canClose := true
	//orderMap.Iterator(func(k interface{}, v interface{}) bool {
	//	parts := strings.Split(k.(string), "&")
	//	if 3 != len(parts) {
	//		return true
	//	}
	//
	//	var (
	//		uid uint64
	//	)
	//	uid, err = strconv.ParseUint(parts[2], 10, 64)
	//	if nil != err {
	//		log.Println("查看用户仓位，解析id错误:", k)
	//	}
	//
	//	if uid != uint64(users[0].Id) {
	//		return true
	//	}
	//
	//	amount := v.(float64)
	//
	//	if !floatEqual(amount, 0, 1e-7) {
	//		canClose = false
	//	}
	//
	//	return true
	//})
	//
	//if !canClose {
	//	return 0
	//}

	_, err = g.Model("user").Ctx(ctx).Data(g.Map{"api_status": status, "need_init": init}).Where("api_key=?", apiKey).Update()
	if nil != err {
		log.Println("更新用户api_status：", err)
		return 0
	}

	return 1
}

// SetUseNewSystem set user num
func (s *sBinanceTraderHistory) SetUseNewSystem(ctx context.Context, apiKey string, useNewSystem uint64) error {
	var (
		err error
	)
	_, err = g.Model("user").Ctx(ctx).Data("open_status", useNewSystem).Where("api_key=?", apiKey).Update()
	if nil != err {
		log.Println("更新用户num：", err)
		return err
	}

	return nil
}

// GetSystemUserPositions get user positions
func (s *sBinanceTraderHistory) GetSystemUserPositions(ctx context.Context, apiKey string) map[string]float64 {
	var (
		err   error
		users []*entity.User
		res   map[string]float64
	)
	res = make(map[string]float64, 0)

	err = g.Model("user").Where("api_key=?", apiKey).Ctx(ctx).Scan(&users)
	if nil != err {
		log.Println("查看用户仓位，数据库查询错误：", err)
		return res
	}

	if 0 >= len(users) || 0 >= users[0].Id {
		return res
	}

	// 遍历map
	orderMap.Iterator(func(k interface{}, v interface{}) bool {
		parts := strings.Split(k.(string), "&")
		if 3 != len(parts) {
			return true
		}

		var (
			uid uint64
		)
		uid, err = strconv.ParseUint(parts[2], 10, 64)
		if nil != err {
			log.Println("查看用户仓位，解析id错误:", k)
		}

		if uid != uint64(users[0].Id) {
			return true
		}

		part1 := parts[1]
		amount := v.(float64)
		res[parts[0]+"&"+part1] = math.Abs(amount)
		return true
	})

	return res
}

// GetPlatUserPositions get Plat user positions
func (s *sBinanceTraderHistory) GetPlatUserPositions(ctx context.Context, apiKey string) map[string]string {
	var (
		err       error
		users     []*entity.User
		res       map[string]string
		positions []*BinancePosition
	)
	res = make(map[string]string, 0)

	err = g.Model("user").Where("api_key=?", apiKey).Ctx(ctx).Scan(&users)
	if nil != err {
		log.Println("查看用户仓位，数据库查询错误：", err)
		return res
	}

	if 0 >= len(users) || 0 >= users[0].Id {
		return res
	}
	if "binance" == users[0].Plat {
		positions = getBinancePositionInfo(users[0].ApiKey, users[0].ApiSecret)
		for _, v := range positions {
			// 新增
			var (
				currentAmount float64
			)
			currentAmount, err = strconv.ParseFloat(v.PositionAmt, 64)
			if nil != err {
				log.Println("获取用户仓位接口，解析出错")
				continue
			}

			if floatEqual(currentAmount, 0, 1e-7) {
				continue
			}

			res[v.Symbol+v.PositionSide] = v.PositionAmt
		}
	} else if "bybit" == users[0].Plat {
		var (
			bybitPisitionsRes *BybitPositionInfoResponse
		)

		bybitPisitionsRes, err = bybitGetPositionInfo(ctx, users[0].ApiKey, users[0].ApiSecret)
		if nil != err || nil == bybitPisitionsRes {
			return res
		}

		for _, v := range bybitPisitionsRes.Result.List {
			if "" == v.Side || "None" == v.Side {
				continue
			}

			// 新增
			var (
				currentAmount float64
			)
			currentAmount, err = strconv.ParseFloat(v.Size, 64)
			if nil != err {
				log.Println("获取用户仓位接口，解析出错")
				continue
			}

			if floatEqual(currentAmount, 0, 1e-7) {
				continue
			}

			positionSide := "BOTH"
			if 1 == v.PositionIdx {
				positionSide = "LONG"
			} else if 2 == v.PositionIdx {
				positionSide = "SHORT"
			}

			res[v.Symbol+positionSide] = v.Size
		}

	}

	return res
}

// CloseBinanceUserPositions close binance user positions
func (s *sBinanceTraderHistory) CloseBinanceUserPositions(ctx context.Context) uint64 {
	var (
		err   error
		users []*entity.User
	)

	err = g.Model("user").Where("api_status=?", 1).Ctx(ctx).Scan(&users)
	if nil != err {
		log.Println("查看用户仓位，数据库查询错误：", err)
		return 0
	}

	for _, vUser := range users {

		if "binance" == vUser.Plat {
			var (
				positions []*BinancePosition
			)

			positions = getBinancePositionInfo(vUser.ApiKey, vUser.ApiSecret)
			for _, v := range positions {
				// 新增
				var (
					currentAmount float64
				)
				currentAmount, err = strconv.ParseFloat(v.PositionAmt, 64)
				if nil != err {
					log.Println("close positions 获取用户仓位接口，解析出错", v, vUser)
					continue
				}

				currentAmount = math.Abs(currentAmount)
				if floatEqual(currentAmount, 0, 1e-7) {
					continue
				}

				var (
					symbolRel     = v.Symbol
					tmpQty        float64
					quantity      string
					quantityFloat float64
					orderType     = "MARKET"
					side          string
				)
				if "LONG" == v.PositionSide {
					side = "SELL"
				} else if "SHORT" == v.PositionSide {
					side = "BUY"
				} else {
					log.Println("close positions 仓位错误", v, vUser)
					continue
				}

				tmpQty = currentAmount // 本次开单数量
				if !symbolsMap.Contains(symbolRel) {
					log.Println("close positions，代币信息无效，信息", v, vUser)
					continue
				}

				// 精度调整
				if 0 >= symbolsMap.Get(symbolRel).(*LhCoinSymbol).QuantityPrecision {
					quantity = fmt.Sprintf("%d", int64(tmpQty))
				} else {
					quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(symbolRel).(*LhCoinSymbol).QuantityPrecision, 64)
				}

				quantityFloat, err = strconv.ParseFloat(quantity, 64)
				if nil != err {
					log.Println("close positions，数量解析", v, vUser, err)
					continue
				}

				if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
					continue
				}

				var (
					binanceOrderRes *binanceOrder
					orderInfoRes    *orderInfo
				)

				// 请求下单
				binanceOrderRes, orderInfoRes, err = requestBinanceOrder(symbolRel, side, orderType, v.PositionSide, quantity, vUser.ApiKey, vUser.ApiSecret)
				if nil != err {
					log.Println("close positions，执行下单错误，手动：", err, symbolRel, side, orderType, v.PositionSide, quantity, vUser.ApiKey, vUser.ApiSecret)
				}

				// 下单异常
				if 0 >= binanceOrderRes.OrderId {
					log.Println("自定义下单，binance下单错误：", orderInfoRes)
					continue
				}
				log.Println("close, 执行成功：", vUser, v, binanceOrderRes)
			}

		} else if "bybit" == vUser.Plat {
			var (
				bybitPisitionsRes *BybitPositionInfoResponse
			)

			bybitPisitionsRes, err = bybitGetPositionInfo(ctx, vUser.ApiKey, vUser.ApiSecret)
			if nil != err || nil == bybitPisitionsRes {
				continue
			}

			for _, v := range bybitPisitionsRes.Result.List {
				if "" == v.Side || "None" == v.Side {
					continue
				}

				// 新增
				var (
					currentAmount float64
				)
				currentAmount, err = strconv.ParseFloat(v.Size, 64)
				if nil != err {
					log.Println("获取用户仓位接口，解析出错")
					continue
				}

				if floatEqual(currentAmount, 0, 1e-7) {
					continue
				}

				var (
					symbolRel = v.Symbol
					quantity  = v.Size
					side      string
				)
				if 1 == v.PositionIdx {
					side = "Sell"
				} else if 2 == v.PositionIdx {
					side = "Buy"
				} else {
					log.Println("close positions 仓位错误，bybit", v, vUser)
					continue
				}

				if !globalUsersOrderId.Contains(users[0].Id) {
					log.Println("close position，无效信息，不存在自增订单，信息", users[0])
					continue
				}

				tmpOrderId := globalUsersOrderId.Get(users[0].Id).(uint64)
				globalUsersOrderId.Set(users[0].Id, tmpOrderId+1)
				tmpOrderIdStr := strconv.FormatUint(uint64(users[0].Id), 10) + "at" + strconv.FormatUint(uint64(time.Now().Unix()), 10) + "&" + strconv.FormatUint(tmpOrderId, 10)

				var (
					resOrder *BybitPlaceOrderResponse
				)
				resOrder, err = bybitPlaceOrder(ctx, vUser.ApiKey, vUser.ApiSecret, symbolRel, quantity, side, v.PositionIdx, tmpOrderIdStr)
				if nil != err {
					log.Println("bybit 仓位下单错误", err, resOrder)
				}

				if nil == resOrder {
					log.Println("bybit 仓位下单错误，没有返回结果", err, resOrder)
					continue
				}

				if 0 != resOrder.RetCode {
					fmt.Println("bybit close position 下单异常：", resOrder)
				}

				if 0 >= len(resOrder.Result.OrderId) {
					log.Println("bybit close position 下单异常，订单号错误：", resOrder)
					continue
				}
			}
		}

		time.Sleep(500 * time.Millisecond)
	}

	return 1
}

// SetSystemUserPosition set user positions
func (s *sBinanceTraderHistory) SetSystemUserPosition(ctx context.Context, system uint64, systemOrder uint64, apiKey string, symbol string, side string, positionSide string, num float64) uint64 {
	var (
		err   error
		users []*entity.User
	)

	err = g.Model("user").Where("api_key=?", apiKey).Ctx(ctx).Scan(&users)
	if nil != err {
		log.Println("修改仓位，数据库查询错误：", err)
		return 0
	}

	if 0 >= len(users) || 0 >= users[0].Id {
		log.Println("修改仓位，数据库查询错误：", err)
		return 0
	}

	vTmpUserMap := users[0]
	strUserId := strconv.FormatUint(uint64(vTmpUserMap.Id), 10)
	symbolMapKey := symbol + "USDT"

	if "binance" == vTmpUserMap.Plat {
		var (
			symbolRel     = symbolMapKey
			tmpQty        float64
			quantity      string
			quantityFloat float64
			orderType     = "MARKET"
		)
		if "LONG" == positionSide {

			positionSide = "LONG"
			if "BUY" == side {
				side = "BUY"
			} else if "SELL" == side {
				side = "SELL"
			} else {
				log.Println("自定义下单，无效信息，信息", apiKey, symbol, side, positionSide, num)
				return 0
			}
		} else if "SHORT" == positionSide {
			positionSide = "SHORT"
			if "BUY" == side {
				side = "BUY"
			} else if "SELL" == side {
				side = "SELL"
			} else {
				log.Println("自定义下单，无效信息，信息", apiKey, symbol, side, positionSide, num)
				return 0
			}
		} else {
			log.Println("自定义下单，无效信息，信息", apiKey, symbol, side, positionSide, num)
			return 0
		}

		tmpQty = num // 本次开单数量
		if !symbolsMap.Contains(symbolMapKey) {
			log.Println("自定义下单，代币信息无效，信息", apiKey, symbol, side, positionSide, num)
			return 0
		}

		// 精度调整
		if 0 >= symbolsMap.Get(symbolMapKey).(*LhCoinSymbol).QuantityPrecision {
			quantity = fmt.Sprintf("%d", int64(tmpQty))
		} else {
			quantity = strconv.FormatFloat(tmpQty, 'f', symbolsMap.Get(symbolMapKey).(*LhCoinSymbol).QuantityPrecision, 64)
		}

		quantityFloat, err = strconv.ParseFloat(quantity, 64)
		if nil != err {
			log.Println(err)
			return 0
		}

		if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
			return 0
		}

		var (
			binanceOrderRes *binanceOrder
			orderInfoRes    *orderInfo
		)

		if 1 == systemOrder {
			// 请求下单
			binanceOrderRes, orderInfoRes, err = requestBinanceOrder(symbolRel, side, orderType, positionSide, quantity, vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret)
			if nil != err {
				log.Println("执行下单错误，手动：", err, symbolRel, side, orderType, positionSide, quantity, vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret)
			}

			//binanceOrderRes = &binanceOrder{
			//	OrderId:       1,
			//	ExecutedQty:   quantity,
			//	ClientOrderId: "",
			//	Symbol:        "",
			//	AvgPrice:      "",
			//	CumQuote:      "",
			//	Side:          side,
			//	PositionSide:  positionSide,
			//	ClosePosition: false,
			//	Type:          "",
			//	Status:        "",
			//}

			// 下单异常
			if 0 >= binanceOrderRes.OrderId {
				log.Println("自定义下单，binance下单错误：", orderInfoRes)
				return 0
			}
		}

		var tmpExecutedQty float64
		tmpExecutedQty = quantityFloat

		if 1 == system {
			// 不存在新增，这里只能是开仓
			if !orderMap.Contains(symbolRel + "&" + positionSide + "&" + strUserId) {
				orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
			} else {
				// 追加仓位，开仓
				if "LONG" == positionSide {
					if "BUY" == side {
						d1 := decimal.NewFromFloat(orderMap.Get(symbolRel + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Add(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(symbolRel+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, newRes)

						// 跟单人开仓成功，用户的预备仓位清空
						orderMapTmp.Set(symbolRel+"&"+positionSide+"&"+strUserId, float64(0))
					} else if "SELL" == side {
						d1 := decimal.NewFromFloat(orderMap.Get(symbolRel + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Sub(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(symbolRel+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						if lessThanOrEqualZero(newRes, 0, 1e-7) {
							newRes = 0
						}

						orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, newRes)

						//  跟单人完全平仓，用户的预备仓位清空
						if lessThanOrEqualZero(newRes, 0, 1e-7) {
							orderMapTmp.Set(symbolRel+"&"+positionSide+"&"+strUserId, float64(0))
						}
					} else {
						log.Println("手动，binance下单，数据存储:", system, apiKey, symbol, side, positionSide, num, binanceOrderRes, orderInfoRes, tmpExecutedQty)
					}

				} else if "SHORT" == positionSide {
					if "SELL" == side {
						d1 := decimal.NewFromFloat(orderMap.Get(symbolRel + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Add(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(symbolRel+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, newRes)

						// 跟单人开仓成功，用户的预备仓位清空
						orderMapTmp.Set(symbolRel+"&"+positionSide+"&"+strUserId, float64(0))
					} else if "BUY" == side {
						d1 := decimal.NewFromFloat(orderMap.Get(symbolRel + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Sub(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(symbolRel+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						if lessThanOrEqualZero(newRes, 0, 1e-7) {
							newRes = 0
						}

						orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, newRes)

						//  跟单人完全平仓，用户的预备仓位清空
						if lessThanOrEqualZero(newRes, 0, 1e-7) {
							orderMapTmp.Set(symbolRel+"&"+positionSide+"&"+strUserId, float64(0))
						}
					} else {
						log.Println("手动，binance下单，数据存储:", system, apiKey, symbol, side, positionSide, num, binanceOrderRes, orderInfoRes, tmpExecutedQty)
					}

				} else {
					log.Println("手动，binance下单，数据存储:", system, apiKey, symbol, side, positionSide, num, binanceOrderRes, orderInfoRes, tmpExecutedQty)
				}
			}
		}
	} else if "bybit" == vTmpUserMap.Plat {
		var (
			symbolRel         = symbolMapKey
			tmpQty            float64
			quantity          string
			quantityFloat     float64
			positionSideOrder int
			sideOrder         string
		)
		if "LONG" == positionSide {
			positionSideOrder = 1
			if "BUY" == side {
				sideOrder = "Buy"
			} else if "SELL" == side {
				sideOrder = "Sell"
			} else {
				log.Println("自定义下单，无效信息，信息", apiKey, symbol, side, positionSide, num)
				return 0
			}
		} else if "SHORT" == positionSide {
			positionSideOrder = 2
			if "BUY" == side {
				sideOrder = "Buy"
			} else if "SELL" == side {
				sideOrder = "Sell"
			} else {
				log.Println("自定义下单，无效信息，信息", apiKey, symbol, side, positionSide, num)
				return 0
			}
		} else {
			log.Println("自定义下单，无效信息，信息", apiKey, symbol, side, positionSide, num)
			return 0
		}

		tmpQty = num // 本次开单数量
		if !symbolsBybitMap.Contains(symbolMapKey) {
			log.Println("自定义下单，代币信息无效，信息, bybit", apiKey, symbol, side, positionSide, num)
			return 0
		}

		// 精度调整
		tmpQtyStep := symbolsBybitMap.Get(symbolMapKey).(*BybitSymbol).QtyStep
		tmpFloatQuantity := adjustToStepSize(tmpQty, tmpQtyStep)

		quantity = getStringFromStepSizePrecision(tmpFloatQuantity, tmpQtyStep)
		quantityFloat, err = strconv.ParseFloat(quantity, 64)
		if nil != err {
			log.Println(err)
			return 0
		}

		if lessThanOrEqualZero(quantityFloat, 0, 1e-7) {
			return 0
		}

		var (
			resOrder *BybitPlaceOrderResponse
		)

		if 1 == systemOrder {
			if !globalUsersOrderId.Contains(vTmpUserMap.Id) {
				log.Println("自定义下单，无效信息，不存在自增订单，信息", vTmpUserMap)
				return 0
			}

			tmpOrderId := globalUsersOrderId.Get(vTmpUserMap.Id).(uint64)
			globalUsersOrderId.Set(vTmpUserMap.Id, tmpOrderId+1)
			tmpOrderIdStr := strconv.FormatUint(uint64(vTmpUserMap.Id), 10) + "at" + strconv.FormatUint(uint64(time.Now().Unix()), 10) + "&" + strconv.FormatUint(tmpOrderId, 10)

			resOrder, err = bybitPlaceOrder(ctx, vTmpUserMap.ApiKey, vTmpUserMap.ApiSecret, symbolRel, quantity, sideOrder, positionSideOrder, tmpOrderIdStr)
			if nil != err {
				log.Println("bybit 自定义下单，错误", err, resOrder)
			}

			if nil == resOrder {
				log.Println("bybit 仓位下单错误，没有返回结果", err, resOrder)
				return 0
			}

			if 0 != resOrder.RetCode {
				fmt.Println("bybit 自定义下单，错误，下单异常：", resOrder)
				return 0
			}

			if 0 >= len(resOrder.Result.OrderId) {
				log.Println("bybit 自定义下单，错误，订单号错误：", resOrder)
				return 0
			}
		}

		var tmpExecutedQty float64
		tmpExecutedQty = quantityFloat

		if 1 == system {
			// 不存在新增，这里只能是开仓
			if !orderMap.Contains(symbolRel + "&" + positionSide + "&" + strUserId) {
				orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, tmpExecutedQty)
			} else {
				// 追加仓位，开仓
				if "LONG" == positionSide {
					if "BUY" == side {
						d1 := decimal.NewFromFloat(orderMap.Get(symbolRel + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Add(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(symbolRel+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, newRes)

						// 跟单人开仓成功，用户的预备仓位清空
						orderMapTmp.Set(symbolRel+"&"+positionSide+"&"+strUserId, float64(0))
					} else if "SELL" == side {
						d1 := decimal.NewFromFloat(orderMap.Get(symbolRel + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Sub(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(symbolRel+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						if lessThanOrEqualZero(newRes, 0, 1e-7) {
							newRes = 0
						}

						orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, newRes)

						//  跟单人完全平仓，用户的预备仓位清空
						if lessThanOrEqualZero(newRes, 0, 1e-7) {
							orderMapTmp.Set(symbolRel+"&"+positionSide+"&"+strUserId, float64(0))
						}
					} else {
						log.Println("手动，bybit下单，数据存储:", system, apiKey, symbol, side, positionSide, num, resOrder, tmpExecutedQty)
					}

				} else if "SHORT" == positionSide {
					if "SELL" == side {
						d1 := decimal.NewFromFloat(orderMap.Get(symbolRel + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Add(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(symbolRel+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, newRes)

						// 跟单人开仓成功，用户的预备仓位清空
						orderMapTmp.Set(symbolRel+"&"+positionSide+"&"+strUserId, float64(0))
					} else if "BUY" == side {
						d1 := decimal.NewFromFloat(orderMap.Get(symbolRel + "&" + positionSide + "&" + strUserId).(float64))
						d2 := decimal.NewFromFloat(tmpExecutedQty)
						result := d1.Sub(d2)

						var (
							newRes float64
							exact  bool
						)
						newRes, exact = result.Float64()
						if !exact {
							fmt.Println("转换过程中可能发生了精度损失", d1, d2, tmpExecutedQty, orderMap.Get(symbolRel+"&"+positionSide+"&"+strUserId).(float64), newRes)
						}

						if lessThanOrEqualZero(newRes, 0, 1e-7) {
							newRes = 0
						}

						orderMap.Set(symbolRel+"&"+positionSide+"&"+strUserId, newRes)

						//  跟单人完全平仓，用户的预备仓位清空
						if lessThanOrEqualZero(newRes, 0, 1e-7) {
							orderMapTmp.Set(symbolRel+"&"+positionSide+"&"+strUserId, float64(0))
						}
					} else {
						log.Println("手动，bybit下单，数据存储:", system, apiKey, symbol, side, positionSide, num, resOrder, tmpExecutedQty)
					}

				} else {
					log.Println("手动，bybit下单，数据存储:", system, apiKey, symbol, side, positionSide, num, resOrder, tmpExecutedQty)
				}
			}
		}

	} else {
		log.Println("初始化，错误用户信息，开仓", vTmpUserMap)
		return 0
	}

	return 1
}

// SetCookie set cookie
func (s *sBinanceTraderHistory) SetCookie(ctx context.Context, cookie, token string) int64 {
	var (
		err error
	)

	_, err = g.Model("zy_trader_cookie").Ctx(ctx).
		Data(g.Map{"cookie": cookie, "token": token, "is_open": 1}).
		Where("id=?", 1).Update()
	if nil != err {
		log.Println("更新cookie：", err)
		return 0
	}

	return 1
}

type binancePositionResp struct {
	Data []*binancePositionDataList
}

type binancePositionDataList struct {
	Symbol         string
	PositionSide   string
	PositionAmount string
}

// 请求binance的持有仓位历史接口，新
func (s *sBinanceTraderHistory) requestBinancePositionHistoryNew(portfolioId uint64, cookie string, token string) ([]*binancePositionDataList, bool, error) {
	var (
		resp   *http.Response
		res    []*binancePositionDataList
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-data/positions?portfolioId=" + strconv.FormatUint(portfolioId, 10)
	)

	// 创建不验证 SSL 证书的 HTTP 客户端
	httpClient := &http.Client{
		Timeout: time.Second * 2,
	}

	// 构造请求
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		fmt.Println("Error creating request:", err)
		return nil, true, err
	}

	// 添加头信息
	req.Header.Set("Clienttype", "web")
	req.Header.Set("Cookie", cookie)
	req.Header.Set("Csrftoken", token)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 Edg/126.0.0.0")

	// 发送请求
	resp, err = httpClient.Do(req)
	if err != nil {
		fmt.Println("Error making request:", err)
		return nil, true, err
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				fmt.Println(44444, err)
			}
		}
	}()

	// 结果
	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(4444, err)
		return nil, true, err
	}

	//fmt.Println(string(b))
	var l *binancePositionResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		return nil, true, err
	}

	if nil == l.Data {
		return res, true, nil
	}

	res = make([]*binancePositionDataList, 0)
	for _, v := range l.Data {
		res = append(res, v)
	}

	return res, false, nil
}

type binanceOrder struct {
	OrderId       int64
	ExecutedQty   string
	ClientOrderId string
	Symbol        string
	AvgPrice      string
	CumQuote      string
	Side          string
	PositionSide  string
	ClosePosition bool
	Type          string
	Status        string
}

type orderInfo struct {
	Code int64
	Msg  string
}

func requestBinanceOrder(symbol string, side string, orderType string, positionSide string, quantity string, apiKey string, secretKey string) (*binanceOrder, *orderInfo, error) {
	var (
		client       *http.Client
		req          *http.Request
		resp         *http.Response
		res          *binanceOrder
		resOrderInfo *orderInfo
		data         string
		b            []byte
		err          error
		apiUrl       = "https://fapi.binance.com/fapi/v1/order"
	)

	//fmt.Println(symbol, side, orderType, positionSide, quantity, apiKey, secretKey)
	// 时间
	now := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	// 拼请求数据
	data = "symbol=" + symbol + "&side=" + side + "&type=" + orderType + "&positionSide=" + positionSide + "&newOrderRespType=" + "RESULT" + "&quantity=" + quantity + "&timestamp=" + now

	// 加密
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(data))
	signature := hex.EncodeToString(h.Sum(nil))
	// 构造请求

	req, err = http.NewRequest("POST", apiUrl, strings.NewReader(data+"&signature="+signature))
	if err != nil {
		return nil, nil, err
	}
	// 添加头信息
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", apiKey)

	// 请求执行
	client = &http.Client{Timeout: 3 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		return nil, nil, err
	}

	// 结果
	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	var o binanceOrder
	err = json.Unmarshal(b, &o)
	if err != nil {
		fmt.Println(string(b), err)
		return nil, nil, err
	}

	res = &binanceOrder{
		OrderId:       o.OrderId,
		ExecutedQty:   o.ExecutedQty,
		ClientOrderId: o.ClientOrderId,
		Symbol:        o.Symbol,
		AvgPrice:      o.AvgPrice,
		CumQuote:      o.CumQuote,
		Side:          o.Side,
		PositionSide:  o.PositionSide,
		ClosePosition: o.ClosePosition,
		Type:          o.Type,
	}

	if 0 >= res.OrderId {
		//fmt.Println(string(b))
		err = json.Unmarshal(b, &resOrderInfo)
		if err != nil {
			fmt.Println(string(b), err)
			return nil, nil, err
		}
	}

	return res, resOrderInfo, nil
}

type BinanceTraderDetailResp struct {
	Data *BinanceTraderDetailData
}

type BinanceTraderDetailData struct {
	MarginBalance string
}

// 拉取交易员交易历史
func requestBinanceTraderDetail(portfolioId uint64) (string, error) {
	var (
		resp   *http.Response
		res    string
		b      []byte
		err    error
		apiUrl = "https://www.binance.com/bapi/futures/v1/friendly/future/copy-trade/lead-portfolio/detail?portfolioId=" + strconv.FormatUint(portfolioId, 10)
	)

	// 构造请求
	resp, err = http.Get(apiUrl)
	if err != nil {
		return res, err
	}

	// 结果
	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(err)
		return res, err
	}

	var l *BinanceTraderDetailResp
	err = json.Unmarshal(b, &l)
	if err != nil {
		fmt.Println(err)
		return res, err
	}

	if nil == l.Data {
		return res, nil
	}

	return l.Data.MarginBalance, nil
}

// BinanceExchangeInfoResp 结构体表示 Binance 交易对信息的 API 响应
type BinanceExchangeInfoResp struct {
	Symbols []*BinanceSymbolInfo `json:"symbols"`
}

// BinanceSymbolInfo 结构体表示单个交易对的信息
type BinanceSymbolInfo struct {
	Symbol            string `json:"symbol"`
	Pair              string `json:"pair"`
	ContractType      string `json:"contractType"`
	Status            string `json:"status"`
	BaseAsset         string `json:"baseAsset"`
	QuoteAsset        string `json:"quoteAsset"`
	MarginAsset       string `json:"marginAsset"`
	PricePrecision    int    `json:"pricePrecision"`
	QuantityPrecision int    `json:"quantityPrecision"`
}

// 获取 Binance U 本位合约交易对信息
func getBinanceFuturesPairs() ([]*BinanceSymbolInfo, error) {
	apiUrl := "https://fapi.binance.com/fapi/v1/exchangeInfo"

	// 发送 HTTP GET 请求
	resp, err := http.Get(apiUrl)
	if err != nil {
		return nil, err
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	// 读取响应体
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析 JSON 响应
	var exchangeInfo *BinanceExchangeInfoResp
	err = json.Unmarshal(body, &exchangeInfo)
	if err != nil {
		return nil, err
	}

	return exchangeInfo.Symbols, nil
}

// 获取币安服务器时间
func getBinanceServerTime() int64 {
	urlTmp := "https://api.binance.com/api/v3/time"
	resp, err := http.Get(urlTmp)
	if err != nil {
		log.Println("Error getting server time:", err)
		return 0
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	var serverTimeResponse struct {
		ServerTime int64 `json:"serverTime"`
	}
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response body:", err)
		return 0
	}
	if err := json.Unmarshal(body, &serverTimeResponse); err != nil {
		log.Println("Error unmarshaling server time:", err)
		return 0
	}

	return serverTimeResponse.ServerTime
}

// 生成签名
func generateSignature(apiS string, params url.Values) string {
	// 将请求参数编码成 URL 格式的字符串
	queryString := params.Encode()

	// 生成签名
	mac := hmac.New(sha256.New, []byte(apiS))
	mac.Write([]byte(queryString)) // 用 API Secret 生成签名
	return hex.EncodeToString(mac.Sum(nil))
}

// Asset 代表单个资产的保证金信息
type Asset struct {
	TotalMarginBalance string `json:"totalMarginBalance"` // 资产余额
}

// GetBinanceInfo 获取账户信息
func getBinanceInfo(apiK, apiS string) string {
	// 请求的API地址
	endpoint := "/fapi/v2/account"
	baseURL := "https://fapi.binance.com"

	// 获取当前时间戳（使用服务器时间避免时差问题）
	serverTime := getBinanceServerTime()
	if serverTime == 0 {
		return ""
	}
	timestamp := strconv.FormatInt(serverTime, 10)

	// 设置请求参数
	params := url.Values{}
	params.Set("timestamp", timestamp)
	params.Set("recvWindow", "5000") // 设置接收窗口

	// 生成签名
	signature := generateSignature(apiS, params)

	// 将签名添加到请求参数中
	params.Set("signature", signature)

	// 构建完整的请求URL
	requestURL := baseURL + endpoint + "?" + params.Encode()

	// 创建请求
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		log.Println("Error creating request:", err)
		return ""
	}

	// 添加请求头
	req.Header.Add("X-MBX-APIKEY", apiK)

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error sending request:", err)
		return ""
	}

	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	// 读取响应
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response:", err)
		return ""
	}

	// 解析响应
	var o *Asset
	err = json.Unmarshal(body, &o)
	if err != nil {
		log.Println("Error unmarshalling response:", err)
		return ""
	}

	// 返回资产余额
	return o.TotalMarginBalance
}

func requestBinancePositionSide(positionSide string, apiKey string, secretKey string) (error, string, bool) {
	var (
		client       *http.Client
		req          *http.Request
		resp         *http.Response
		resOrderInfo *orderInfo
		data         string
		b            []byte
		err          error
		apiUrl       = "https://fapi.binance.com/fapi/v1/positionSide/dual"
	)

	//log.Println(symbol, side, orderType, positionSide, quantity, apiKey, secretKey)
	// 时间
	now := strconv.FormatInt(time.Now().UTC().UnixMilli(), 10)
	// 拼请求数据
	data = "dualSidePosition=" + positionSide + "&timestamp=" + now

	// 加密
	h := hmac.New(sha256.New, []byte(secretKey))
	h.Write([]byte(data))
	signature := hex.EncodeToString(h.Sum(nil))
	// 构造请求

	req, err = http.NewRequest("POST", apiUrl, strings.NewReader(data+"&signature="+signature))
	if err != nil {
		return err, "", false
	}
	// 添加头信息
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-MBX-APIKEY", apiKey)

	// 请求执行
	client = &http.Client{Timeout: 3 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		return err, "", false
	}

	// 结果
	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	b, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		//log.Println(string(b), err)
		return err, string(b), false
	}

	err = json.Unmarshal(b, &resOrderInfo)
	if err != nil {
		//log.Println(string(b), err)
		return err, string(b), false
	}

	//log.Println(string(b))
	if 200 == resOrderInfo.Code || -4059 == resOrderInfo.Code {
		return nil, string(b), true
	}

	//log.Println(string(b), err)
	return nil, string(b), false
}

// BinanceResponse 包含多个仓位和账户信息
type BinanceResponse struct {
	Positions []*BinancePosition `json:"positions"` // 仓位信息
}

// BinancePosition 代表单个头寸（持仓）信息
type BinancePosition struct {
	Symbol                 string `json:"symbol"`                 // 交易对
	InitialMargin          string `json:"initialMargin"`          // 当前所需起始保证金(基于最新标记价格)
	MaintMargin            string `json:"maintMargin"`            // 维持保证金
	UnrealizedProfit       string `json:"unrealizedProfit"`       // 持仓未实现盈亏
	PositionInitialMargin  string `json:"positionInitialMargin"`  // 持仓所需起始保证金(基于最新标记价格)
	OpenOrderInitialMargin string `json:"openOrderInitialMargin"` // 当前挂单所需起始保证金(基于最新标记价格)
	Leverage               string `json:"leverage"`               // 杠杆倍率
	Isolated               bool   `json:"isolated"`               // 是否是逐仓模式
	EntryPrice             string `json:"entryPrice"`             // 持仓成本价
	MaxNotional            string `json:"maxNotional"`            // 当前杠杆下用户可用的最大名义价值
	BidNotional            string `json:"bidNotional"`            // 买单净值，忽略
	AskNotional            string `json:"askNotional"`            // 卖单净值，忽略
	PositionSide           string `json:"positionSide"`           // 持仓方向 (BOTH, LONG, SHORT)
	PositionAmt            string `json:"positionAmt"`            // 持仓数量
	UpdateTime             int64  `json:"updateTime"`             // 更新时间
}

// getBinancePositionInfo 获取账户信息
func getBinancePositionInfo(apiK, apiS string) []*BinancePosition {
	// 请求的API地址
	endpoint := "/fapi/v2/account"
	baseURL := "https://fapi.binance.com"

	// 获取当前时间戳（使用服务器时间避免时差问题）
	serverTime := getBinanceServerTime()
	if serverTime == 0 {
		return nil
	}
	timestamp := strconv.FormatInt(serverTime, 10)

	// 设置请求参数
	params := url.Values{}
	params.Set("timestamp", timestamp)
	params.Set("recvWindow", "5000") // 设置接收窗口

	// 生成签名
	signature := generateSignature(apiS, params)

	// 将签名添加到请求参数中
	params.Set("signature", signature)

	// 构建完整的请求URL
	requestURL := baseURL + endpoint + "?" + params.Encode()

	// 创建请求
	req, err := http.NewRequest("GET", requestURL, nil)
	if err != nil {
		log.Println("Error creating request:", err)
		return nil
	}

	// 添加请求头
	req.Header.Add("X-MBX-APIKEY", apiK)

	// 发送请求
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Println("Error sending request:", err)
		return nil
	}
	defer func() {
		if resp != nil && resp.Body != nil {
			err := resp.Body.Close()
			if err != nil {
				log.Println("关闭响应体错误：", err)
			}
		}
	}()

	// 读取响应
	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading response:", err)
		return nil
	}

	// 解析响应
	var o *BinanceResponse
	err = json.Unmarshal(body, &o)
	if err != nil {
		log.Println("Error unmarshalling response:", err)
		return nil
	}

	// 返回资产余额
	return o.Positions
}

// BybitLotSizeFilter 解析 LotSizeFilter 相关信息
type BybitLotSizeFilter struct {
	MinOrderQty string `json:"minOrderQty"` // 最小下单量
	MaxOrderQty string `json:"maxOrderQty"` // 最大下单量
	QtyStep     string `json:"qtyStep"`     // 下单步长
}

// BybitContract 解析 USDT 永续合约信息
type BybitContract struct {
	Symbol        string             `json:"symbol"`        // 交易对名称
	ContractType  string             `json:"contractType"`  // 合约类型
	Status        string             `json:"status"`        // 状态（Trading、PreLaunch）
	BaseCoin      string             `json:"baseCoin"`      // 交易基础货币
	QuoteCoin     string             `json:"quoteCoin"`     // 计价货币
	LaunchTime    string             `json:"launchTime"`    // 上线时间
	LotSizeFilter BybitLotSizeFilter `json:"lotSizeFilter"` // 下单规则
}

func getByBitCoinInfo(ctx context.Context) ([]*BybitContract, error) {
	client := bybit.NewBybitHttpClient("", "", bybit.WithBaseURL(bybit.MAINNET))
	res := make([]*BybitContract, 0)

	// 查询 USDT 永续合约
	params := map[string]interface{}{
		"category": "linear", // 只查询 USDT 永续合约
		"limit":    1000,
	}

	// 调用 API 获取合约信息
	var (
		info *bybit.ServerResponse
		err  error
	)
	info, err = client.NewUtaBybitServiceWithParams(params).GetInstrumentInfo(ctx)
	if err != nil {
		log.Println("API 请求失败:", err)
		return res, err
	}

	// 检查返回码
	if info.RetCode != 0 {
		log.Println("API 返回错误", info.RetCode, info.RetMsg)
		return res, nil
	}

	// 解析 info.Result 为 map
	resultMap, ok := info.Result.(map[string]interface{})
	if !ok {
		log.Println("解析 API 结果失败: ", info.Result)
		return res, nil
	}

	// 解析合约列表
	list, ok2 := resultMap["list"].([]interface{})
	if !ok2 {
		log.Println("解析合约列表失败:", resultMap)
		return res, nil
	}

	for _, item := range list {
		var (
			contractJSON []byte
		)
		contractJSON, err = json.Marshal(item)
		if err != nil {
			log.Printf("合约 JSON 序列化失败: %v", err)
			continue
		}

		var contract *BybitContract
		if err = json.Unmarshal(contractJSON, &contract); err != nil {
			log.Printf("合约 JSON 解析失败: %v", err)
			continue
		}

		res = append(res, contract)
	}

	return res, nil
}

// BybitBalanceCoin 表示每个币种的资产信息
type BybitBalanceCoin struct {
	Coin                string `json:"coin"`                // 币种名称
	Equity              string `json:"equity"`              // 总权益
	UsdValue            string `json:"usdValue"`            // 折合 USD 价值
	WalletBalance       string `json:"walletBalance"`       // 钱包余额
	UnrealisedPnl       string `json:"unrealisedPnl"`       // 未实现盈亏
	Locked              string `json:"locked"`              // 锁仓资金
	AvailableToBorrow   string `json:"availableToBorrow"`   // 可借金额
	AvailableToWithdraw string `json:"availableToWithdraw"` // 可提余额
	MarginCollateral    bool   `json:"marginCollateral"`    // 是否作为保证金
	CollateralSwitch    bool   `json:"collateralSwitch"`    // 保证金开关
}

// BybitBalanceAccount 表示账户整体的资产信息
type BybitBalanceAccount struct {
	AccountType            string              `json:"accountType"`
	TotalEquity            string              `json:"totalEquity"`
	TotalMarginBalance     string              `json:"totalMarginBalance"`
	TotalAvailableBalance  string              `json:"totalAvailableBalance"`
	TotalWalletBalance     string              `json:"totalWalletBalance"`
	TotalPerpUPL           string              `json:"totalPerpUPL"`
	TotalInitialMargin     string              `json:"totalInitialMargin"`
	TotalMaintenanceMargin string              `json:"totalMaintenanceMargin"`
	AccountIMRate          string              `json:"accountIMRate"`
	AccountMMRate          string              `json:"accountMMRate"`
	AccountLTV             string              `json:"accountLTV"`
	Coin                   []*BybitBalanceCoin `json:"coin"`
}
type BybitBalanceResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		List []*BybitBalanceAccount `json:"list"`
	} `json:"result"`
}

func getBybitAccountBalance(ctx context.Context, apiK, apiS string) ([]*BybitBalanceAccount, error) {
	client := bybit.NewBybitHttpClient(apiK, apiS, bybit.WithBaseURL(bybit.MAINNET))

	// 构造请求参数（如果 API 需要）
	params := map[string]interface{}{
		"accountType": "UNIFIED", // 只查询 USDT 永续合约
	}

	resp, err := client.NewUtaBybitServiceWithParams(params).GetAccountWallet(ctx)
	if err != nil {
		log.Println("请求账户余额失败:", err)
		return nil, err
	}

	if resp.RetCode != 0 {
		log.Println("账户余额接口返回错误:", resp.RetCode, resp.RetMsg)
		return nil, fmt.Errorf("retCode=%d, retMsg=%s", resp.RetCode, resp.RetMsg)
	}

	var (
		raw         []byte
		balanceResp *BybitBalanceResponse
	)
	raw, err = json.Marshal(resp)
	if err != nil {
		log.Println("序列化原始返回数据失败:", err)
		return nil, err
	}

	if err = json.Unmarshal(raw, &balanceResp); err != nil {
		log.Println("解析账户余额数据失败:", err)
		return nil, err
	}

	return balanceResp.Result.List, nil
}

type BybitPlaceOrderResponse struct {
	RetCode int    `json:"retCode"`
	RetMsg  string `json:"retMsg"`
	Result  struct {
		OrderId     string `json:"orderId"`
		OrderLinkId string `json:"orderLinkId"`
	} `json:"result"`
	RetExtInfo interface{} `json:"retExtInfo"`
	Time       int64       `json:"time"`
}

func bybitPlaceOrder(ctx context.Context, apiK, apiS, symbol, qty, side string, position int, orderId string) (*BybitPlaceOrderResponse, error) {
	// 初始化客户端（选择 TESTNET 或 MAINNET）
	client := bybit.NewBybitHttpClient(
		apiK,
		apiS,
		bybit.WithBaseURL(bybit.MAINNET), // 或者 MAINNET
	)

	// 构造下单参数
	params := map[string]interface{}{
		"category":    "linear",
		"symbol":      symbol,   // 交易对，比如 BTCUSDT
		"side":        side,     // 下单方向：Buy 或 Sell
		"positionIdx": position, // 仓位索引：0 表示单向持仓模式，1 表示多仓，2 表示空仓
		"orderType":   "Market", // 订单类型：Limit 或 Market
		"qty":         qty,      // 下单数量
		"orderLinkId": orderId,
	}

	//log.Println("测试下单信息：", params)
	// 发起下单请求
	resp, err := client.NewUtaBybitServiceWithParams(params).PlaceOrder(ctx)
	if err != nil {
		log.Printf("下单失败: %v", err)
		return nil, err
	}
	var (
		contractJSON  []byte
		orderResponse *BybitPlaceOrderResponse
	)

	//log.Println(resp)
	contractJSON, err = json.Marshal(resp)
	if err != nil {
		log.Printf("合约 JSON 序列化失败: %v", err)
		return nil, err
	}

	if err = json.Unmarshal(contractJSON, &orderResponse); err != nil {
		log.Printf("合约 JSON 解析失败: %v", err)
		return nil, err
	}

	return orderResponse, nil
}

type BybitSetPositionSideResponse struct {
	RetCode    int         `json:"retCode"`
	RetMsg     string      `json:"retMsg"`
	Result     struct{}    `json:"result"`
	RetExtInfo interface{} `json:"retExtInfo"`
	Time       int64       `json:"time"`
}

func bybitSetPositionSide(ctx context.Context, apiK, apiS, symbol string) (*BybitSetPositionSideResponse, string, error) {
	// 初始化客户端（选择 TESTNET 或 MAINNET）
	client := bybit.NewBybitHttpClient(
		apiK,
		apiS,
		bybit.WithBaseURL(bybit.MAINNET), // 或者 MAINNET
	)

	// 构造下单参数
	params := map[string]interface{}{
		"category": "linear",
		"symbol":   symbol,
		"mode":     3,
	}

	// 发起下单请求
	resp, err := client.NewUtaBybitServiceWithParams(params).SwitchPositionMode(ctx)
	if err != nil {
		log.Printf("设置持仓模式失败: %v", err)
		return nil, "", err
	}
	var (
		contractJSON  []byte
		orderResponse *BybitSetPositionSideResponse
	)

	contractJSON, err = json.Marshal(resp)
	if err != nil {
		log.Printf("合约 JSON 序列化失败: %v", err)
		return nil, string(contractJSON), err
	}

	if err = json.Unmarshal(contractJSON, &orderResponse); err != nil {
		log.Printf("合约 JSON 解析失败: %v", err)
		return nil, string(contractJSON), err
	}

	return orderResponse, string(contractJSON), nil
}

type BybitPositionInfoResponse struct {
	RetCode    int             `json:"retCode"`
	RetMsg     string          `json:"retMsg"`
	Result     *PositionResult `json:"result"`
	RetExtInfo interface{}     `json:"retExtInfo"`
	Time       int64           `json:"time"`
}

type PositionResult struct {
	Category string      `json:"category"`
	List     []*Position `json:"list"`
}

type Position struct {
	Symbol           string `json:"symbol"`
	Side             string `json:"side"`           // Buy / Sell
	Size             string `json:"size"`           // 持仓数量
	EntryPrice       string `json:"entryPrice"`     // 开仓均价
	Leverage         string `json:"leverage"`       // 杠杆倍数
	PositionValue    string `json:"positionValue"`  // 持仓价值
	PositionStatus   string `json:"positionStatus"` // Normal 等状态
	TakeProfit       string `json:"takeProfit"`
	StopLoss         string `json:"stopLoss"`
	TrailingStop     string `json:"trailingStop"`
	UnrealisedPnl    string `json:"unrealisedPnl"`  // 浮动盈亏
	CumRealisedPnl   string `json:"cumRealisedPnl"` // 已实现盈亏
	RiskId           int    `json:"riskId"`
	PositionIdx      int    `json:"positionIdx"` // 0=双向/1=买/2=卖
	TradeMode        int    `json:"tradeMode"`   // 0=逐仓/1=全仓
	AutoAddMargin    int    `json:"autoAddMargin"`
	AdlRankIndicator int    `json:"adlRankIndicator"`
	UpdatedTime      string `json:"updatedTime"`
}

func bybitGetPositionInfo(ctx context.Context, apiK, apiS string) (*BybitPositionInfoResponse, error) {
	client := bybit.NewBybitHttpClient(
		apiK,
		apiS,
		bybit.WithBaseURL(bybit.MAINNET),
	)

	params := map[string]interface{}{
		"category":   "linear", // 线性合约
		"limit":      200,
		"settleCoin": "USDT",
	}

	resp, err := client.NewUtaBybitServiceWithParams(params).GetPositionList(ctx)
	if err != nil {
		log.Printf("查询持仓失败: %v", err)
		return nil, err
	}

	var (
		jsonBytes   []byte
		positionRes *BybitPositionInfoResponse
	)
	//log.Printf("测试仓位信息：", resp)
	jsonBytes, err = json.Marshal(resp)
	if err != nil {
		log.Printf("序列化 JSON 失败: %v", err)
		return nil, err
	}

	if err = json.Unmarshal(jsonBytes, &positionRes); err != nil {
		log.Printf("反序列化 JSON 失败: %v", err)
		return nil, err
	}

	return positionRes, nil
}
