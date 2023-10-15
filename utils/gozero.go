package utils

import (
	"github.com/zeromicro/go-zero/zrpc"
	"github.com/zeromicro/go-zero/core/discov"
	"fmt"
)
func ZrpcConnDirect(dsn string,options ...zrpc.ClientOption) zrpc.Client  {
	conn := zrpc.MustNewClient(zrpc.RpcClientConf{
		Target: dsn,
		NonBlock: true,
	},options...)
	return conn
}
func ZrpcAuthConnDirect(dsn,app,token string,options ...zrpc.ClientOption) zrpc.Client  {
	conn := zrpc.MustNewClient(zrpc.RpcClientConf{
		Target: dsn,
		NonBlock: true,
		App: app,
		Token: token,
	},options...)
	return conn
}
func ZrpcConn(serviceName string,option ...interface{}) zrpc.Client  {
	var dsn,user,pass string
	if len(option) > 0  {
		dsn = option[0].(string)
		user = option[1].(string)
		pass = option[2].(string)
	}else{
		dsn = "127.0.0.1:2379"
	}
	fmt.Println("dsn,user,pass:",dsn,user,pass)
	conn := zrpc.MustNewClient(zrpc.RpcClientConf{
		Etcd: discov.EtcdConf{ // 通过 etcd 服务发现时，只需要给 Etcd 配置即可
			Hosts: []string{dsn},
			Key:   serviceName,
			User: user,
			Pass: pass,
		},
		NonBlock: true,
	})
	return conn
}
