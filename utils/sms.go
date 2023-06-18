package utils

import (
	"fmt"
)

const (

)

//CreateXML 生成发送短信内容请求体
func CreateXML(method string, params []interface{}) string {
	xmlstr :=
		`<soapenv:Envelope xmlns:soapenv="http://schemas.xmlsoap.org/soap/envelope/" xmlns:web="http://webservice.sms.foresealife.com/">`
	xmlstr += " <soapenv:Header/>"
	xmlstr += " <soapenv:Body>"
	xmlstr += "<web:" + method + ">"
	for index, item := range params {
		xmlstr += fmt.Sprintf("<arg%v>%v</arg%v>", index, item, index)
	}
	xmlstr += "</web:" + method + "></soapenv:Body></soapenv:Envelope>"
	return xmlstr
}

