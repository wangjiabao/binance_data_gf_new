package cmd

import (
	"binance_data_gf/internal/service"
	"context"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gcmd"
	"github.com/gogf/gf/v2/os/gtimer"
	"log"
	"strconv"
	"time"
)

var (
	Main = &gcmd.Command{
		Name: "main",
	}

	// TraderGui 监听系统中指定的交易员-龟兔赛跑
	TraderGui = &gcmd.Command{
		Name:  "traderGui",
		Brief: "listen trader",
		Func: func(ctx context.Context, parser *gcmd.Parser) (err error) {
			serviceBinanceTrader := service.BinanceTraderHistory()

			// 初始化根据数据库现有人
			if !serviceBinanceTrader.UpdateCoinInfo(ctx) {
				log.Println("初始化币种失败，fail")
				return nil
			}
			log.Println("初始化币种成功，ok")

			// 拉龟兔的保证金
			serviceBinanceTrader.PullAndSetBaseMoneyNewGuiTuAndUser(ctx)

			// 10秒/次，拉取保证金
			handle := func(ctx context.Context) {
				serviceBinanceTrader.PullAndSetBaseMoneyNewGuiTuAndUser(ctx)
			}
			gtimer.AddSingleton(ctx, time.Second*10, handle)

			// 30秒/次，加新人
			handle2 := func(ctx context.Context) {
				serviceBinanceTrader.InsertGlobalUsers(ctx)
			}
			gtimer.AddSingleton(ctx, time.Second*30, handle2)

			// 300秒/次，币种信息
			handle3 := func(ctx context.Context) {
				serviceBinanceTrader.UpdateCoinInfo(ctx)
			}
			gtimer.AddSingleton(ctx, time.Second*300, handle3)

			// 60秒/次，cookie
			handle4 := func(ctx context.Context) {
				serviceBinanceTrader.CookieErrEmail(ctx)
			}
			gtimer.AddSingleton(ctx, time.Second*60, handle4)

			// 任务1 同步订单，死循环
			go serviceBinanceTrader.PullAndOrderNewGuiTu(ctx)

			// 开启http管理服务
			s := g.Server()
			s.Group("/api", func(group *ghttp.RouterGroup) {
				// 探测ip
				group.POST("/set_position_side", func(r *ghttp.Request) {
					var (
						setCode   uint64
						setString string
					)

					setCode, setString = serviceBinanceTrader.SetPositionSide(ctx, r.PostFormValue("plat"), r.PostFormValue("api_key"), r.PostFormValue("api_secret"))
					r.Response.WriteJson(g.Map{
						"code": setCode,
						"msg":  setString,
					})

					return
				})

				// 查询num
				group.GET("/nums", func(r *ghttp.Request) {
					res := serviceBinanceTrader.GetSystemUserNum(ctx)

					responseData := make([]*g.MapStrAny, 0)
					for k, v := range res {
						responseData = append(responseData, &g.MapStrAny{k: v})
					}

					r.Response.WriteJson(responseData)
					return
				})

				// 更新num
				group.POST("/update/num", func(r *ghttp.Request) {
					var (
						parseErr error
						setErr   error
						num      float64
					)
					num, parseErr = strconv.ParseFloat(r.PostFormValue("num"), 64)
					if nil != parseErr || 0 >= num {
						r.Response.WriteJson(g.Map{
							"code": -1,
						})

						return
					}

					setErr = serviceBinanceTrader.SetSystemUserNum(ctx, r.PostFormValue("apiKey"), num)
					if nil != setErr {
						r.Response.WriteJson(g.Map{
							"code": -2,
						})

						return
					}

					r.Response.WriteJson(g.Map{
						"code": 1,
					})

					return
				})

				// 加人
				group.POST("/create/user", func(r *ghttp.Request) {
					var (
						parseErr error
						setErr   error
						needInit uint64
						num      float64
					)
					needInit, parseErr = strconv.ParseUint(r.PostFormValue("need_init"), 10, 64)
					if nil != parseErr {
						r.Response.WriteJson(g.Map{
							"code": -1,
						})

						return
					}

					num, parseErr = strconv.ParseFloat(r.PostFormValue("num"), 64)
					if nil != parseErr || 0 >= num {
						r.Response.WriteJson(g.Map{
							"code": -1,
						})

						return
					}

					setErr = serviceBinanceTrader.CreateUser(
						ctx,
						r.PostFormValue("address"),
						r.PostFormValue("api_key"),
						r.PostFormValue("api_secret"),
						r.PostFormValue("plat"),
						needInit,
						num,
					)
					if nil != setErr {
						r.Response.WriteJson(g.Map{
							"code": -2,
						})

						return
					}

					r.Response.WriteJson(g.Map{
						"code": 1,
					})

					return
				})

				// 更新api status
				group.POST("/update/api_status", func(r *ghttp.Request) {
					var (
						parseErr error
						setCode  uint64
						status   uint64
						reInit   uint64
					)
					status, parseErr = strconv.ParseUint(r.PostFormValue("status"), 10, 64)
					if nil != parseErr || 0 >= status {
						r.Response.WriteJson(g.Map{
							"code": -1,
						})

						return
					}

					reInit, parseErr = strconv.ParseUint(r.PostFormValue("init"), 10, 64)
					if nil != parseErr || 0 >= reInit {
						r.Response.WriteJson(g.Map{
							"code": -1,
						})

						return
					}

					setCode = serviceBinanceTrader.SetApiStatus(ctx, r.PostFormValue("apiKey"), status, reInit)
					r.Response.WriteJson(g.Map{
						"code": setCode,
					})

					return
				})

				// 查询api status
				group.GET("/api_status", func(r *ghttp.Request) {
					res := serviceBinanceTrader.GetApiStatus(ctx, r.Get("apiKey").String())
					r.Response.WriteJson(&g.MapStrAny{r.Get("apiKey").String(): res})
					return
				})

				// 更新开新单
				group.POST("/update/useNewSystem", func(r *ghttp.Request) {
					var (
						parseErr error
						setErr   error
						status   uint64
					)
					status, parseErr = strconv.ParseUint(r.PostFormValue("status"), 10, 64)
					if nil != parseErr || 0 > status {
						r.Response.WriteJson(g.Map{
							"code": -1,
						})

						return
					}

					setErr = serviceBinanceTrader.SetUseNewSystem(ctx, r.PostFormValue("apiKey"), status)
					if nil != setErr {
						r.Response.WriteJson(g.Map{
							"code": -2,
						})

						return
					}

					r.Response.WriteJson(g.Map{
						"code": 1,
					})

					return
				})

				// 查询用户系统仓位
				group.GET("/user/positions", func(r *ghttp.Request) {
					res := serviceBinanceTrader.GetSystemUserPositions(ctx, r.Get("apiKey").String())

					responseData := make([]*g.MapStrAny, 0)
					for k, v := range res {
						responseData = append(responseData, &g.MapStrAny{k: v})
					}

					r.Response.WriteJson(responseData)
					return
				})

				// 查询用户binance仓位
				group.GET("/user/binance/positions", func(r *ghttp.Request) {

					res := serviceBinanceTrader.GetPlatUserPositions(ctx, r.Get("apiKey").String())

					responseData := make([]*g.MapStrAny, 0)
					for k, v := range res {
						responseData = append(responseData, &g.MapStrAny{k: v})
					}

					r.Response.WriteJson(responseData)
					return
				})

				// 用户全平仓位
				group.POST("/user/close/positions", func(r *ghttp.Request) {
					r.Response.WriteJson(g.Map{
						"code": serviceBinanceTrader.CloseBinanceUserPositions(ctx),
					})

					return
				})

				// 用户设置仓位
				group.POST("/user/update/position", func(r *ghttp.Request) {
					var (
						parseErr    error
						num         float64
						system      uint64
						systemOrder uint64 = 1
					)
					num, parseErr = strconv.ParseFloat(r.PostFormValue("num"), 64)
					if nil != parseErr || 0 >= num {
						r.Response.WriteJson(g.Map{
							"code": -1,
						})

						return
					}

					system, parseErr = strconv.ParseUint(r.PostFormValue("system"), 10, 64)
					if nil != parseErr || 0 > system {
						r.Response.WriteJson(g.Map{
							"code": -1,
						})

						return
					}

					if 0 < len(r.PostFormValue("systemOrder")) {
						systemOrder, parseErr = strconv.ParseUint(r.PostFormValue("systemOrder"), 10, 64)
						if nil != parseErr || 0 > systemOrder {
							r.Response.WriteJson(g.Map{
								"code": -1,
							})

							return
						}
					}

					r.Response.WriteJson(g.Map{
						"code": serviceBinanceTrader.SetSystemUserPosition(
							ctx,
							system,
							systemOrder,
							r.PostFormValue("apiKey"),
							r.PostFormValue("symbol"),
							r.PostFormValue("side"),
							r.PostFormValue("positionSide"),
							num,
						),
					})

					return
				})

				// cookie设置
				group.POST("/cookie", func(r *ghttp.Request) {
					r.Response.WriteJson(g.Map{
						"code": serviceBinanceTrader.SetCookie(ctx, r.PostFormValue("cookie"), r.PostFormValue("token")),
					})

					return
				})
			})

			s.SetPort(80)
			s.Run()

			// 阻塞
			select {}
		},
	}
)
