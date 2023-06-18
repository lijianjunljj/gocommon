package utils

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"
	"github.com/lijianjunljj/gocommon/misc"
	"github.com/gin-gonic/gin"
)

//Request 请求结构体
type Request struct {
	URL     string
	Method  string
	Data    interface{}
	Headers map[string]string
}

const (
	//RequestTimeout 连接超时时间，默认5秒超时时间
	RequestTimeout = misc.RequestTimeout
	//RequestIsTracing 是否开启HTTP请求链路追踪
	RequestIsTracing = misc.RequestIsTracing
	//ResponseCode 请求响应代码字段名
	ResponseCode = misc.ResponseCode
	//ResponseMsg 请求响应代码字段名
	ResponseMsg = misc.ResponseMsg
	//ResponseData 请求响应代码字段名
	ResponseData = misc.ResponseData
)

//POST POST请求
func POST(url string, data interface{}, ctxs ...*gin.Context) (map[string]interface{}, error) {
	var req = Request{
		URL:    url,
		Method: "POST",
		Data:   data,
	}
	return HTTPRequest(req, false, ctxs)

}

//POSTByResponse POST请求返回全部响应信息
func POSTByResponse(url string, data interface{}, ctxs ...*gin.Context) (map[string]interface{}, error) {
	var req = Request{
		URL:    url,
		Method: "POST",
		Data:   data,
	}
	return HTTPRequest(req, true, ctxs)

}

//GET GET请求
func GET(url string, ctxs ...*gin.Context) (map[string]interface{}, error) {
	var req = Request{
		URL:    url,
		Method: "GET",
		Data:   nil,
	}
	return HTTPRequest(req, false, ctxs)
}

//HTTPRequest 请求函数封装
func HTTPRequest(request Request, isAllResponse bool, ctxs []*gin.Context) (map[string]interface{}, error) {
	var resData = make(map[string]interface{})
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	client := &http.Client{
		Timeout:   time.Second * RequestTimeout,
		Transport: tr,
	}
	data := ""
	var err error
	switch v := request.Data.(type) {
	case *map[string]interface{}:
		data, err = JSONEncode(request.Data)
		fmt.Println("request JSONEncode err:", err)
		if err != nil {
			return resData, err
		}
	case string:
		data = v
		//fmt.Println("request data type:", v)
	default:
		//fmt.Println("request data type:", v)
	}

	req, err := http.NewRequest(request.Method, request.URL, strings.NewReader(data))
	fmt.Println("request NewRequest err:", err, req)
	if err != nil {
		return resData, err
	}
	if request.Method != "GET" {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	for index, item := range request.Headers {
		req.Header.Set(index, item)
	}
	if len(ctxs) > 0 {
		req.Header.Set("Authorization", ctxs[0].Request.Header.Get("Authorization"))
	}
	resp, err := client.Do(req)
	fmt.Println("request err:", err)
	if err != nil {
		return resData, err
	}
	content, err := ioutil.ReadAll(resp.Body)
	defer resp.Body.Close()

	if err != nil {
		return resData, err
	}
	//fmt.Println("response :", string(content))
	if strings.Contains(request.Headers["Content-Type"], "xml") || strings.Contains(request.Headers["Content-Type"], "x-www-form-urlencoded") {

		resData["data"] = string(content)
		return resData, nil
	}

	json.Unmarshal(content, &resData)
	//fmt.Println("request ReadAll err:", err, resData)
	empty := make(map[string]interface{})
	if len(resData) == 0 {
		return empty, errors.New("请求出错")
	}
	if isAllResponse {
		return resData, nil
	}
	if resData[ResponseCode] != misc.CodeSuccess {
		return empty, errors.New(resData[ResponseMsg].(string))
	}
	if resData[ResponseData] == nil {
		return empty, nil
	}
	return resData[ResponseData].(map[string]interface{}), nil
}
