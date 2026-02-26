package utils

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-playground/validator/v10"

	"github.com/gin-gonic/gin"
)

const (
	//CodeSuccess 请求成功响应码
	CodeSuccess = "0000"
	//CodeFail 请求失败响应码
	CodeFail = "1001"
	//CodeTokenExpired 登录失效响应码
	CodeTokenExpired = "1002"
	//CodeException 系统异常响应码
	CodeException = "1003"
)

// Response 响应结构体
type Response struct {
	Code    string      `json:"code"`
	Message string      `json:"msg"`
	Data    interface{} `json:"data"`
}

// Success 响应成功
func Success(ctx *gin.Context, data interface{}) {
	response := Response{
		Code:    CodeSuccess,
		Message: "成功",
		Data:    data,
	}
	jsonData, _ := json.Marshal(response)
	fmt.Println("response:", string(jsonData))
	ctx.JSON(200, response)
}

// Fail 响应错误
func Fail(ctx *gin.Context, err error, codes ...string) {
	code := CodeFail
	msg := err.Error()
	fmt.Println(err, msg)
	if len(codes) > 0 {
		code = codes[0]
	}
	if _, isValidationErrors := err.(validator.ValidationErrors); isValidationErrors {
		msg = err.Error() // "参数校验不通过"
	}
	if _, isJSONError := err.(*json.UnmarshalTypeError); isJSONError {
		msg = err.Error() // "字段类型不匹配"
	}
	if strings.Contains(msg, "rpc error") {
		errs := strings.Split(msg, "=")
		msg = errs[len(errs)-1]
	}
	ctx.JSON(200, Response{
		Code:    code,
		Message: msg,
		Data:    err.Error(),
	})
}
