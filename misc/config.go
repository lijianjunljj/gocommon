package misc

import "github.com/lijianjunljj/gocommon/config"

var Config *config.Config

const (

	//RequestTimeout 连接超时时间，默认5秒超时时间
	RequestTimeout = 5
	//RequestIsTracing 是否开启HTTP请求链路追踪
	RequestIsTracing = true

	//CodeSuccess 请求成功响应码
	CodeSuccess = "0000"
	//CodeFail 请求失败响应码
	CodeFail = "1001"
	//CodeTokenExpired 登录失效响应码
	CodeTokenExpired = "1002"
	//CodeException 系统异常响应码
	CodeException = "1003"

	//ResponseCode 请求响应代码字段名
	ResponseCode = "code"
	//ResponseMsg 请求响应代码字段名
	ResponseMsg = "msg"
	//ResponseData 请求响应代码字段名
	ResponseData = "data"
)
