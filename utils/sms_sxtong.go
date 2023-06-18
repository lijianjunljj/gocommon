package utils

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)
var SmsSxtongConf struct {
	//SmsServerURL 短信服务地址
	SmsServerURL  string
	//SmsUserID 短信用户ID
	SmsUserID string
	//SmsPassword 短信用户密码
	SmsPassword  string
	//SmsMethod 短信发送调用方法
	SmsMethod  string
	//SmsTemplate 短信内容模板
	SmsTemplate  string
}

//FilterResult 处理短信返回结果
func FilterResult(result string) int {
	start := strings.Index(result, "<StatusCode>")
	end := strings.Index(result, "</StatusCode>")
	content := result[start+len("<StatusCode>") : end]
	b, err := strconv.Atoi(content)
	if err != nil {
		return -1
	}
	return b
}

//SendSMS 发送短信
func SendSMS(mobile string, content string, ctxs ...*gin.Context) (bool, error) {
	errorMsgs := map[int]string{
		-1:  "用户或者密码错误",
		-2:  "用户账号被冻结",
		-3:  "超过最大短信内容长度",
		-4:  "超过允许发送号码数量",
		-5:  "内容非法",
		-6:  "手机号码无效",
		-7:  "认证失败，修改密码无效",
		-8:  "账号余额不足",
		-9:  "服务端异常",
		-10: "用户不存在",
		-11: "申请试用账号失败",
		-12: "定时发送时间错误",
		-13: "定时短信时间不能超过3个月",
		-14: "定时短信时MsgIdentify不能为空",
		-15: "该定时短信不能被取消",
	}
	paramsQuery := fmt.Sprintf("UserName=%v&Password=%v&MobileNumber=%v&MsgContent=%v", SmsSxtongConf.SmsUserID, SmsSxtongConf.SmsPassword, mobile, content)
	headers := map[string]string{
		"Content-Type": "application/x-www-form-urlencoded; charset=utf-8",
	}
	res, err := HTTPRequest(Request{
		URL:     SmsSxtongConf.SmsServerURL,
		Method:  "POST",
		Data:    paramsQuery,
		Headers: headers,
	}, true, ctxs)
	if err != nil {
		return false, err
	}
	result, err := strconv.Atoi(res["data"].(string))
	if result != 0 {
		return false, errors.New(errorMsgs[result])
	}
	return true, nil
}
